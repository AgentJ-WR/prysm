package forkchoice

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/epoch/precompute"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

var (
	beaconFinalizedEpoch = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "beacon_finalized_epoch",
		Help: "Last finalized epoch of the processed state",
	})
	beaconFinalizedRoot = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "beacon_finalized_root",
		Help: "Last finalized root of the processed state",
	})
	beaconCurrentJustifiedEpoch = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "beacon_current_justified_epoch",
		Help: "Current justified epoch of the processed state",
	})
	beaconCurrentJustifiedRoot = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "beacon_current_justified_root",
		Help: "Current justified root of the processed state",
	})
	beaconPrevJustifiedEpoch = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "beacon_previous_justified_epoch",
		Help: "Previous justified epoch of the processed state",
	})
	beaconPrevJustifiedRoot = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "beacon_previous_justified_root",
		Help: "Previous justified root of the processed state",
	})
	sigFailsToVerify = promauto.NewCounter(prometheus.CounterOpts{
		Name: "att_signature_failed_to_verify_with_cache",
		Help: "Number of attestation signatures that failed to verify with cache on, but succeeded without cache",
	})
	validatorsCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "validator_count",
		Help: "The total number of validators",
	}, []string{"state"})
	validatorsBalance = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "validators_total_balance",
		Help: "The total balance of validators, in GWei",
	}, []string{"state"})
	validatorsEffectiveBalance = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "validators_total_effective_balance",
		Help: "The total effective balance of validators, in GWei",
	}, []string{"state"})
	currentEth1DataDepositCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "current_eth1_data_deposit_count",
		Help: "The current eth1 deposit count in the last processed state eth1data field.",
	})
	totalEligibleBalances = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "total_eligible_balances",
		Help: "The total amount of ether, in gwei, that has been used in voting attestation target of previous epoch",
	})
	totalVotedTargetBalances = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "total_voted_target_balances",
		Help: "The total amount of ether, in gwei, that is eligible for voting of previous epoch",
	})
)

func reportEpochMetrics(state *stateTrie.BeaconState) {
	currentEpoch := helpers.CurrentEpoch(state)

	// Validator instances
	pendingInstances := 0
	activeInstances := 0
	slashingInstances := 0
	slashedInstances := 0
	exitingInstances := 0
	exitedInstances := 0
	// Validator balances
	pendingBalance := uint64(0)
	activeBalance := uint64(0)
	activeEffectiveBalance := uint64(0)
	exitingBalance := uint64(0)
	exitingEffectiveBalance := uint64(0)
	slashingBalance := uint64(0)
	slashingEffectiveBalance := uint64(0)

	validators := state.Validators()
	for i, validator := range validators {
		valBalance, err := state.BalanceAtIndex(uint64(i))
		if err != nil {
			log.WithError(err).Error("could not get balance for validator")
			return
		}
		if validator.Slashed {
			if currentEpoch < validator.ExitEpoch {
				slashingInstances++
				slashingBalance += valBalance
				slashingEffectiveBalance += validator.EffectiveBalance
			} else {
				slashedInstances++
			}
			continue
		}
		if validator.ExitEpoch != params.BeaconConfig().FarFutureEpoch {
			if currentEpoch < validator.ExitEpoch {
				exitingInstances++
				exitingBalance += valBalance
				exitingEffectiveBalance += validator.EffectiveBalance
			} else {
				exitedInstances++
			}
			continue
		}
		if currentEpoch < validator.ActivationEpoch {
			pendingInstances++
			pendingBalance += valBalance
			continue
		}
		activeInstances++
		activeBalance += valBalance
		activeEffectiveBalance += validator.EffectiveBalance
	}
	validatorsCount.WithLabelValues("Pending").Set(float64(pendingInstances))
	validatorsCount.WithLabelValues("Active").Set(float64(activeInstances))
	validatorsCount.WithLabelValues("Exiting").Set(float64(exitingInstances))
	validatorsCount.WithLabelValues("Exited").Set(float64(exitedInstances))
	validatorsCount.WithLabelValues("Slashing").Set(float64(slashingInstances))
	validatorsCount.WithLabelValues("Slashed").Set(float64(slashedInstances))
	validatorsBalance.WithLabelValues("Pending").Set(float64(pendingBalance))
	validatorsBalance.WithLabelValues("Active").Set(float64(activeBalance))
	validatorsBalance.WithLabelValues("Exiting").Set(float64(exitingBalance))
	validatorsBalance.WithLabelValues("Slashing").Set(float64(slashingBalance))
	validatorsEffectiveBalance.WithLabelValues("Active").Set(float64(activeEffectiveBalance))
	validatorsEffectiveBalance.WithLabelValues("Exiting").Set(float64(exitingEffectiveBalance))
	validatorsEffectiveBalance.WithLabelValues("Slashing").Set(float64(slashingEffectiveBalance))

	// Last justified slot
	if c := state.CurrentJustifiedCheckpoint(); c != nil {
		beaconCurrentJustifiedEpoch.Set(float64(c.Epoch))
		beaconCurrentJustifiedRoot.Set(float64(bytesutil.ToLowInt64(c.Root)))
	}
	// Last previous justified slot
	if c := state.PreviousJustifiedCheckpoint(); c != nil {
		beaconPrevJustifiedEpoch.Set(float64(c.Epoch))
		beaconPrevJustifiedRoot.Set(float64(bytesutil.ToLowInt64(c.Root)))
	}
	// Last finalized slot
	if c := state.FinalizedCheckpoint(); c != nil {
		beaconFinalizedEpoch.Set(float64(c.Epoch))
		beaconFinalizedRoot.Set(float64(bytesutil.ToLowInt64(c.Root)))
	}
	if e := state.Eth1Data(); e != nil {
		currentEth1DataDepositCount.Set(float64(e.DepositCount))
	}

	if precompute.Balances != nil {
		totalEligibleBalances.Set(float64(precompute.Balances.PrevEpoch))
		totalVotedTargetBalances.Set(float64(precompute.Balances.PrevEpochTargetAttesters))
	}
}
