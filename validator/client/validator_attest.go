package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/go-ssz"
	slashpb "github.com/prysmaticlabs/prysm/proto/slashing"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/roughtime"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

// SubmitAttestation completes the validator client's attester responsibility at a given slot.
// It fetches the latest beacon block head along with the latest canonical beacon state
// information in order to sign the block and include information about the validator's
// participation in voting on the block.
func (v *validator) SubmitAttestation(ctx context.Context, slot uint64, pubKey [48]byte) {
	ctx, span := trace.StartSpan(ctx, "validator.SubmitAttestation")
	defer span.End()
	span.AddAttributes(trace.StringAttribute("validator", fmt.Sprintf("%#x", pubKey)))

	log := log.WithField("pubKey", fmt.Sprintf("%#x", bytesutil.Trunc(pubKey[:]))).WithField("slot", slot)
	duty, err := v.duty(pubKey)
	if err != nil {
		log.WithError(err).Error("Could not fetch validator assignment")
		return
	}

	// As specified in the spec, an attester should wait until one-third of the way through the slot,
	// then create and broadcast the attestation.
	// https://github.com/ethereum/eth2.0-specs/blob/v0.9.3/specs/validator/0_beacon-chain-validator.md#attesting
	v.waitToOneThird(ctx, slot)

	req := &ethpb.AttestationDataRequest{
		Slot:           slot,
		CommitteeIndex: duty.CommitteeIndex,
	}
	data, err := v.validatorClient.GetAttestationData(ctx, req)
	if err != nil {
		log.WithError(err).Error("Could not request attestation to sign at slot")
		return
	}

	if featureconfig.Get().ProtectAttester {
		history, err := v.db.AttestationHistory(ctx, pubKey[:])
		if err != nil {
			log.Errorf("Could not get attestation history from DB: %v", err)
			return
		}
		if isNewAttSlashable(history, data.Source.Epoch, data.Target.Epoch) {
			log.WithFields(logrus.Fields{
				"sourceEpoch": data.Source.Epoch,
				"targetEpoch": data.Target.Epoch,
			}).Error("Attempted to make a slashable attestation, rejected")
			return
		}
	}

	sig, err := v.signAtt(ctx, pubKey, data)
	if err != nil {
		log.WithError(err).Error("Could not sign attestation")
		return
	}

	var indexInCommittee uint64
	var found bool
	for i, vID := range duty.Committee {
		if vID == duty.ValidatorIndex {
			indexInCommittee = uint64(i)
			found = true
			break
		}
	}
	if !found {
		log.Errorf("Validator ID %d not found in committee of %v", duty.ValidatorIndex, duty.Committee)
		return
	}

	aggregationBitfield := bitfield.NewBitlist(uint64(len(duty.Committee)))
	aggregationBitfield.SetBitAt(indexInCommittee, true)
	attestation := &ethpb.Attestation{
		Data:            data,
		AggregationBits: aggregationBitfield,
		Signature:       sig,
	}

	attResp, err := v.validatorClient.ProposeAttestation(ctx, attestation)
	if err != nil {
		log.WithError(err).Error("Could not submit attestation to beacon node")
		return
	}

	if featureconfig.Get().ProtectAttester {
		history, err := v.db.AttestationHistory(ctx, pubKey[:])
		if err != nil {
			log.Errorf("Could not get attestation history from DB: %v", err)
			return
		}
		history = markAttestationForTargetEpoch(history, data.Source.Epoch, data.Target.Epoch)
		if err := v.db.SaveAttestationHistory(ctx, pubKey[:], history); err != nil {
			log.Errorf("Could not save attestation history to DB: %v", err)
			return
		}
	}

	if err := v.saveAttesterIndexToData(data, duty.ValidatorIndex); err != nil {
		log.WithError(err).Error("Could not save validator index for logging")
		return
	}

	span.AddAttributes(
		trace.Int64Attribute("slot", int64(slot)),
		trace.StringAttribute("attestationHash", fmt.Sprintf("%#x", attResp.AttestationDataRoot)),
		trace.Int64Attribute("committeeIndex", int64(data.CommitteeIndex)),
		trace.StringAttribute("blockRoot", fmt.Sprintf("%#x", data.BeaconBlockRoot)),
		trace.Int64Attribute("justifiedEpoch", int64(data.Source.Epoch)),
		trace.Int64Attribute("targetEpoch", int64(data.Target.Epoch)),
		trace.StringAttribute("bitfield", fmt.Sprintf("%#x", aggregationBitfield)),
	)
}

// waitToOneThird waits until one-third of the way through the slot
// such that any blocks from this slot have time to reach the beacon node
// before creating the attestation.
func (v *validator) waitToOneThird(ctx context.Context, slot uint64) {
	_, span := trace.StartSpan(ctx, "validator.waitToOneThird")
	defer span.End()

	oneThird := params.BeaconConfig().SecondsPerSlot / 3
	delay := time.Duration(oneThird) * time.Second
	if oneThird == 0 {
		delay = 500 * time.Millisecond
	}
	startTime := slotutil.SlotStartTime(v.genesisTime, slot)
	timeToBroadcast := startTime.Add(delay)
	time.Sleep(roughtime.Until(timeToBroadcast))
}

// Given the validator public key, this gets the validator assignment.
func (v *validator) duty(pubKey [48]byte) (*ethpb.DutiesResponse_Duty, error) {
	if v.duties == nil {
		return nil, errors.New("no duties for validators")
	}

	for _, duty := range v.duties.Duties {
		if bytes.Equal(pubKey[:], duty.PublicKey) {
			return duty, nil
		}
	}

	return nil, fmt.Errorf("pubkey %#x not in duties", bytesutil.Trunc(pubKey[:]))
}

// Given validator's public key, this returns the signature of an attestation data.
func (v *validator) signAtt(ctx context.Context, pubKey [48]byte, data *ethpb.AttestationData) ([]byte, error) {
	domain, err := v.validatorClient.DomainData(ctx, &ethpb.DomainRequest{
		Epoch:  data.Target.Epoch,
		Domain: params.BeaconConfig().DomainBeaconAttester,
	})
	if err != nil {
		return nil, err
	}

	root, err := ssz.HashTreeRoot(data)
	if err != nil {
		return nil, err
	}

	sig, err := v.keyManager.Sign(pubKey, root, domain.SignatureDomain)
	if err != nil {
		return nil, err
	}

	return sig.Marshal(), nil
}

// For logging, this saves the last submitted attester index to its attestation data. The purpose of this
// is to enhance attesting logs to be readable when multiple validator keys ran in a single client.
func (v *validator) saveAttesterIndexToData(data *ethpb.AttestationData, index uint64) error {
	v.attLogsLock.Lock()
	defer v.attLogsLock.Unlock()

	h, err := hashutil.HashProto(data)
	if err != nil {
		return err
	}

	if v.attLogs[h] == nil {
		v.attLogs[h] = &attSubmitted{data, []uint64{}, []uint64{}}
	}
	v.attLogs[h] = &attSubmitted{data, append(v.attLogs[h].attesterIndices, index), []uint64{}}

	return nil
}

// isNewAttSlashable uses the attestation history to determine if an attestation of sourceEpoch
// and targetEpoch would be slashable. It can detect double, surrounding, and surrounded votes.
func isNewAttSlashable(history *slashpb.AttestationHistory, sourceEpoch uint64, targetEpoch uint64) bool {
	farFuture := params.BeaconConfig().FarFutureEpoch
	wsPeriod := params.BeaconConfig().WeakSubjectivityPeriod

	// Previously pruned, we should return false.
	if int(targetEpoch) <= int(history.LatestEpochWritten)-int(wsPeriod) {
		return false
	}

	// Check if there has already been a vote for this target epoch.
	if safeTargetToSource(history, targetEpoch) != farFuture {
		return true
	}

	// Check if the new attestation would be surrounding another attestation.
	for i := sourceEpoch; i <= targetEpoch; i++ {
		// Unattested for epochs are marked as FAR_FUTURE_EPOCH.
		if safeTargetToSource(history, i) == farFuture {
			continue
		}
		if history.TargetToSource[i%wsPeriod] > sourceEpoch {
			return true
		}
	}

	// Check if the new attestation is being surrounded.
	for i := targetEpoch; i <= history.LatestEpochWritten; i++ {
		if safeTargetToSource(history, i) < sourceEpoch {
			return true
		}
	}

	return false
}

// markAttestationForTargetEpoch returns the modified attestation history with the passed-in epochs marked
// as attested for. This is done to prevent the validator client from signing any slashable attestations.
func markAttestationForTargetEpoch(history *slashpb.AttestationHistory, sourceEpoch uint64, targetEpoch uint64) *slashpb.AttestationHistory {
	wsPeriod := params.BeaconConfig().WeakSubjectivityPeriod

	if targetEpoch > history.LatestEpochWritten {
		// If the target epoch to mark is ahead of latest written epoch, override the old targets and mark the requested epoch.
		// Limit the overwriting to one weak subjectivity period as further is not needed.
		maxToWrite := history.LatestEpochWritten + wsPeriod
		for i := history.LatestEpochWritten + 1; i < targetEpoch && i <= maxToWrite; i++ {
			history.TargetToSource[i%wsPeriod] = params.BeaconConfig().FarFutureEpoch
		}
		history.LatestEpochWritten = targetEpoch
	}
	history.TargetToSource[targetEpoch%wsPeriod] = sourceEpoch
	return history
}

// safeTargetToSource makes sure the epoch accessed is within bounds, and if it's not it at
// returns the "default" FAR_FUTURE_EPOCH value.
func safeTargetToSource(history *slashpb.AttestationHistory, targetEpoch uint64) uint64 {
	wsPeriod := params.BeaconConfig().WeakSubjectivityPeriod
	if targetEpoch > history.LatestEpochWritten || int(targetEpoch) < int(history.LatestEpochWritten)-int(wsPeriod) {
		return params.BeaconConfig().FarFutureEpoch
	}
	return history.TargetToSource[targetEpoch%wsPeriod]
}
