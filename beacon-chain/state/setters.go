package state

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	pbp2p "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
)

type fieldIndex int

// Below we define a set of useful enum values for the field
// indices of the beacon state. For example, genesisTime is the
// 0th field of the beacon state. This is helpful when we are
// updating the Merkle branches up the trie representation
// of the beacon state.
const (
	genesisTime fieldIndex = iota
	slot
	fork
	latestBlockHeader
	blockRoots
	stateRoots
	historicalRoots
	eth1Data
	eth1DataVotes
	eth1DepositIndex
	validators
	balances
	randaoMixes
	slashings
	previousEpochAttestations
	currentEpochAttestations
	justificationBits
	previousJustifiedCheckpoint
	currentJustifiedCheckpoint
	finalizedCheckpoint
)

// SetGenesisTime for the beacon state.
func (b *BeaconState) SetGenesisTime(val uint64) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.GenesisTime = val
	b.markFieldAsDirty(genesisTime)
	return nil
}

// SetSlot for the beacon state.
func (b *BeaconState) SetSlot(val uint64) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Slot = val
	b.markFieldAsDirty(slot)
	return nil
}

// SetFork version for the beacon chain.
func (b *BeaconState) SetFork(val *pbp2p.Fork) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Fork = proto.Clone(val).(*pbp2p.Fork)
	b.markFieldAsDirty(fork)
	return nil
}

// SetLatestBlockHeader in the beacon state.
func (b *BeaconState) SetLatestBlockHeader(val *ethpb.BeaconBlockHeader) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.LatestBlockHeader = proto.Clone(val).(*ethpb.BeaconBlockHeader)
	b.markFieldAsDirty(latestBlockHeader)
	return nil
}

// SetBlockRoots for the beacon state. This PR updates the entire
// list to a new value by overwriting the previous one.
func (b *BeaconState) SetBlockRoots(val [][]byte) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.BlockRoots = val
	b.markFieldAsDirty(blockRoots)
	return nil
}

// UpdateBlockRootAtIndex for the beacon state. This PR updates the randao mixes
// at a specific index to a new value.
func (b *BeaconState) UpdateBlockRootAtIndex(idx uint64, blockRoot [32]byte) error {
	if len(b.state.BlockRoots) <= int(idx) {
		return fmt.Errorf("invalid index provided %d", idx)
	}

	// Copy on write since this is a shared array.
	r := b.BlockRoots()

	// Must secure lock after copy or hit a deadlock.
	b.lock.Lock()
	defer b.lock.Unlock()

	r[idx] = blockRoot[:]
	b.state.BlockRoots = r

	b.markFieldAsDirty(blockRoots)
	return nil
}

// SetStateRoots for the beacon state. This PR updates the entire
// to a new value by overwriting the previous one.
func (b *BeaconState) SetStateRoots(val [][]byte) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.StateRoots = val
	b.markFieldAsDirty(stateRoots)
	return nil
}

// UpdateStateRootAtIndex for the beacon state. This PR updates the randao mixes
// at a specific index to a new value.
func (b *BeaconState) UpdateStateRootAtIndex(idx uint64, stateRoot [32]byte) error {
	if len(b.state.StateRoots) <= int(idx) {
		return errors.Errorf("invalid index provided %d", idx)
	}

	// Copy on write since this is a shared array.
	r := b.StateRoots()

	// Must secure lock after copy or hit a deadlock.
	b.lock.Lock()
	defer b.lock.Unlock()

	r[idx] = stateRoot[:]
	b.state.StateRoots = r

	b.markFieldAsDirty(stateRoots)
	return nil
}

// SetHistoricalRoots for the beacon state. This PR updates the entire
// list to a new value by overwriting the previous one.
func (b *BeaconState) SetHistoricalRoots(val [][]byte) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.HistoricalRoots = val
	b.markFieldAsDirty(historicalRoots)
	return nil
}

// SetEth1Data for the beacon state.
func (b *BeaconState) SetEth1Data(val *ethpb.Eth1Data) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Eth1Data = val
	b.markFieldAsDirty(eth1Data)
	return nil
}

// SetEth1DataVotes for the beacon state. This PR updates the entire
// list to a new value by overwriting the previous one.
func (b *BeaconState) SetEth1DataVotes(val []*ethpb.Eth1Data) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Eth1DataVotes = val
	b.markFieldAsDirty(eth1DataVotes)
	return nil
}

// AppendEth1DataVotes for the beacon state. This PR appends the new value
// to the the end of list.
func (b *BeaconState) AppendEth1DataVotes(val *ethpb.Eth1Data) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Eth1DataVotes = append(b.state.Eth1DataVotes, val)
	b.markFieldAsDirty(eth1DataVotes)
	return nil
}

// SetEth1DepositIndex for the beacon state.
func (b *BeaconState) SetEth1DepositIndex(val uint64) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Eth1DepositIndex = val
	b.markFieldAsDirty(eth1DepositIndex)
	return nil
}

// SetValidators for the beacon state. This PR updates the entire
// to a new value by overwriting the previous one.
func (b *BeaconState) SetValidators(val []*ethpb.Validator) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Validators = val
	b.markFieldAsDirty(validators)
	return nil
}

// ApplyToEveryValidator applies the provided callback function to each validator in the
// validator registry.
func (b *BeaconState) ApplyToEveryValidator(f func(idx int, val *ethpb.Validator) error) error {
	// Copy on write since this is a shared array.
	v := b.Validators()

	for i, val := range v {
		err := f(i, val)
		if err != nil {
			return err
		}
	}

	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Validators = v
	b.markFieldAsDirty(validators)
	return nil
}

// UpdateValidatorAtIndex for the beacon state. This PR updates the randao mixes
// at a specific index to a new value.
func (b *BeaconState) UpdateValidatorAtIndex(idx uint64, val *ethpb.Validator) error {
	if len(b.state.Validators) <= int(idx) {
		return errors.Errorf("invalid index provided %d", idx)
	}
	// Copy on write since this is a shared array.
	v := b.Validators()

	b.lock.Lock()
	defer b.lock.Unlock()

	v[idx] = val
	b.state.Validators = v
	b.markFieldAsDirty(validators)
	return nil
}

// SetValidatorIndexByPubkey updates the validator index mapping maintained internally to
// a given input 48-byte, public key.
func (b *BeaconState) SetValidatorIndexByPubkey(pubKey [48]byte, validatorIdx uint64) {
	// Copy on write since this is a shared map.
	m := b.validatorIndexMap()

	b.lock.Lock()
	defer b.lock.Unlock()

	m[pubKey] = validatorIdx
	b.valIdxMap = m
}

// SetBalances for the beacon state. This PR updates the entire
// list to a new value by overwriting the previous one.
func (b *BeaconState) SetBalances(val []uint64) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Balances = val
	b.markFieldAsDirty(balances)
	return nil
}

// UpdateBalancesAtIndex for the beacon state. This PR updates the randao mixes
// at a specific index to a new value.
func (b *BeaconState) UpdateBalancesAtIndex(idx uint64, val uint64) error {
	if len(b.state.Balances) <= int(idx) {
		return errors.Errorf("invalid index provided %d", idx)
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Balances[idx] = val
	b.markFieldAsDirty(balances)
	return nil
}

// SetRandaoMixes for the beacon state. This PR updates the entire
// list to a new value by overwriting the previous one.
func (b *BeaconState) SetRandaoMixes(val [][]byte) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.RandaoMixes = val
	b.markFieldAsDirty(randaoMixes)
	return nil
}

// UpdateRandaoMixesAtIndex for the beacon state. This PR updates the randao mixes
// at a specific index to a new value.
func (b *BeaconState) UpdateRandaoMixesAtIndex(val []byte, idx uint64) error {
	if len(b.state.RandaoMixes) <= int(idx) {
		return errors.Errorf("invalid index provided %d", idx)
	}

	// Copy on write since this is a shared array.
	mixes := b.RandaoMixes()

	b.lock.Lock()
	defer b.lock.Unlock()

	mixes[idx] = val
	b.state.RandaoMixes = mixes

	b.markFieldAsDirty(randaoMixes)
	return nil
}

// SetSlashings for the beacon state. This PR updates the entire
// list to a new value by overwriting the previous one.
func (b *BeaconState) SetSlashings(val []uint64) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Slashings = val
	b.markFieldAsDirty(slashings)
	return nil
}

// UpdateSlashingsAtIndex for the beacon state. This PR updates the randao mixes
// at a specific index to a new value.
func (b *BeaconState) UpdateSlashingsAtIndex(idx uint64, val uint64) error {
	if len(b.state.Slashings) <= int(idx) {
		return errors.Errorf("invalid index provided %d", idx)
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Slashings[idx] = val
	b.markFieldAsDirty(slashings)
	return nil
}

// SetPreviousEpochAttestations for the beacon state. This PR updates the entire
// list to a new value by overwriting the previous one.
func (b *BeaconState) SetPreviousEpochAttestations(val []*pbp2p.PendingAttestation) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.PreviousEpochAttestations = val
	b.markFieldAsDirty(previousEpochAttestations)
	return nil
}

// SetCurrentEpochAttestations for the beacon state. This PR updates the entire
// list to a new value by overwriting the previous one.
func (b *BeaconState) SetCurrentEpochAttestations(val []*pbp2p.PendingAttestation) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.CurrentEpochAttestations = val
	b.markFieldAsDirty(currentEpochAttestations)
	return nil
}

// AppendHistoricalRoots for the beacon state. This PR appends the new value
// to the the end of list.
func (b *BeaconState) AppendHistoricalRoots(root [32]byte) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.HistoricalRoots = append(b.state.HistoricalRoots, root[:])
	b.markFieldAsDirty(historicalRoots)
	return nil
}

// AppendCurrentEpochAttestations for the beacon state. This PR appends the new value
// to the the end of list.
func (b *BeaconState) AppendCurrentEpochAttestations(val *pbp2p.PendingAttestation) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.CurrentEpochAttestations = append(b.state.CurrentEpochAttestations, val)
	b.markFieldAsDirty(currentEpochAttestations)
	return nil
}

// AppendPreviousEpochAttestations for the beacon state. This PR appends the new value
// to the the end of list.
func (b *BeaconState) AppendPreviousEpochAttestations(val *pbp2p.PendingAttestation) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.PreviousEpochAttestations = append(b.state.PreviousEpochAttestations, val)
	b.markFieldAsDirty(previousEpochAttestations)
	return nil
}

// AppendValidator for the beacon state. This PR appends the new value
// to the the end of list.
func (b *BeaconState) AppendValidator(val *ethpb.Validator) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Validators = append(b.state.Validators, val)
	b.markFieldAsDirty(validators)
	return nil
}

// AppendBalance for the beacon state. This PR appends the new value
// to the the end of list.
func (b *BeaconState) AppendBalance(bal uint64) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.Balances = append(b.state.Balances, bal)
	b.markFieldAsDirty(balances)
	return nil
}

// SetJustificationBits for the beacon state.
func (b *BeaconState) SetJustificationBits(val bitfield.Bitvector4) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.JustificationBits = val
	b.markFieldAsDirty(justificationBits)
	return nil
}

// SetPreviousJustifiedCheckpoint for the beacon state.
func (b *BeaconState) SetPreviousJustifiedCheckpoint(val *ethpb.Checkpoint) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.PreviousJustifiedCheckpoint = val
	b.markFieldAsDirty(previousJustifiedCheckpoint)
	return nil
}

// SetCurrentJustifiedCheckpoint for the beacon state.
func (b *BeaconState) SetCurrentJustifiedCheckpoint(val *ethpb.Checkpoint) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.CurrentJustifiedCheckpoint = val
	b.markFieldAsDirty(currentJustifiedCheckpoint)
	return nil
}

// SetFinalizedCheckpoint for the beacon state.
func (b *BeaconState) SetFinalizedCheckpoint(val *ethpb.Checkpoint) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.state.FinalizedCheckpoint = val
	b.markFieldAsDirty(finalizedCheckpoint)
	return nil
}

// Recomputes the branch up the index in the Merkle trie representation
// of the beacon state. This method performs map reads and the caller MUST
// hold the lock before calling this method.
func (b *BeaconState) recomputeRoot(idx int) {
	hashFunc := hashutil.CustomSHA256Hasher()
	layers := b.merkleLayers
	// The merkle tree structure looks as follows:
	// [[r1, r2, r3, r4], [parent1, parent2], [root]]
	// Using information about the index which changed, idx, we recompute
	// only its branch up the tree.
	currentIndex := idx
	root := b.merkleLayers[0][idx]
	for i := 0; i < len(layers)-1; i++ {
		isLeft := currentIndex%2 == 0
		neighborIdx := currentIndex ^ 1

		neighbor := make([]byte, 32)
		if layers[i] != nil && len(layers[i]) != 0 && neighborIdx < len(layers[i]) {
			neighbor = layers[i][neighborIdx]
		}
		if isLeft {
			parentHash := hashFunc(append(root, neighbor...))
			root = parentHash[:]
		} else {
			parentHash := hashFunc(append(neighbor, root...))
			root = parentHash[:]
		}
		parentIdx := currentIndex / 2
		// Update the cached layers at the parent index.
		layers[i+1][parentIdx] = root
		currentIndex = parentIdx
	}
	b.merkleLayers = layers
}

func (b *BeaconState) markFieldAsDirty(field fieldIndex) {
	_, ok := b.dirtyFields[field]
	if !ok {
		b.dirtyFields[field] = true
	}
	// do nothing if field already exists
}
