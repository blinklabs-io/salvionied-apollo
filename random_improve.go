package apollo

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"sort"

	"github.com/blinklabs-io/gouroboros/ledger/common"
)

const (
	randomImproveCoverSalt   int64 = 0x4349503030303201
	randomImproveImproveSalt int64 = 0x52494d50524f5645
)

// RandomImproveSelector implements the Random-Improve algorithm described by
// CIP-0002, adapted to Apollo's aggregate CoinSelector interface. It first
// randomly covers the requested value, then tries to add remaining UTxOs that
// move the selection closer to the ideal value of twice the target.
//
// The randomness is deterministic: the same Seed, available pool, and target
// produce the same selection. This preserves the CoinSelector contract and
// makes transaction construction reproducible in tests.
type RandomImproveSelector struct {
	// Seed controls deterministic pseudo-random UTxO ordering. The zero value
	// is a valid deterministic seed.
	Seed int64
}

// NewRandomImproveSelector returns a deterministic Random-Improve selector.
func NewRandomImproveSelector() *RandomImproveSelector {
	return &RandomImproveSelector{}
}

// Name returns the algorithm's identifier.
func (s *RandomImproveSelector) Name() string { return "random-improve" }

// Select returns a subset of available whose summed value covers target.
func (s *RandomImproveSelector) Select(available []common.Utxo, target Value) ([]common.Utxo, error) {
	if target.Coin == 0 && !target.HasAssets() {
		return nil, nil
	}

	cands, err := randomImproveCandidates(available)
	if err != nil {
		return nil, err
	}

	coverOrder := randomImproveShuffle(cands, s.Seed^randomImproveCoverSalt)
	selected := make(map[string]bool, len(coverOrder))
	result := make([]common.Utxo, 0, len(coverOrder))
	total := Value{}

	for _, cand := range coverOrder {
		if total.GreaterOrEqual(target) {
			break
		}
		total, err = total.Add(cand.value)
		if err != nil {
			return nil, fmt.Errorf("selection value overflow after adding UTxO %s: %w", cand.ref, err)
		}
		selected[cand.ref] = true
		result = append(result, cand.utxo)
	}
	if !total.GreaterOrEqual(target) {
		return nil, errInsufficientUtxos
	}

	ideal, err := target.Add(target)
	if err != nil {
		// A target above half of uint64's range cannot be represented at
		// twice its value. The covering selection is still valid, so skip the
		// optional improvement phase instead of failing a satisfiable request.
		return result, nil
	}

	improveOrder := randomImproveShuffle(cands, s.Seed^randomImproveImproveSalt)
	for _, cand := range improveOrder {
		if selected[cand.ref] {
			continue
		}
		if randomImproveHasUntargetedAssets(cand.value, target) {
			continue
		}
		next, err := total.Add(cand.value)
		if err != nil {
			return nil, fmt.Errorf("selection value overflow after adding UTxO %s: %w", cand.ref, err)
		}
		if !randomImproveWithinIdeal(next, ideal) {
			continue
		}
		if randomImproveDistance(next, ideal).Cmp(randomImproveDistance(total, ideal)) >= 0 {
			continue
		}
		total = next
		selected[cand.ref] = true
		result = append(result, cand.utxo)
	}

	return result, nil
}

// RandomImproveThenLargestFirstSelector follows the Cardano Wallet strategy
// from CIP-0002: try Random-Improve first, then fall back to Largest-First
// only when Random-Improve cannot cover the target from the available pool.
type RandomImproveThenLargestFirstSelector struct {
	// RandomImprove is the primary selector. If nil, NewRandomImproveSelector
	// is used.
	RandomImprove CoinSelector
	// Fallback is used when RandomImprove reports insufficient UTxOs. If nil,
	// LargestFirstSelector is used.
	Fallback CoinSelector
}

// NewRandomImproveThenLargestFirstSelector returns a CIP-0002 style selector
// that tries Random-Improve before Largest-First.
func NewRandomImproveThenLargestFirstSelector() *RandomImproveThenLargestFirstSelector {
	return &RandomImproveThenLargestFirstSelector{}
}

// Name returns the algorithm's identifier.
func (s *RandomImproveThenLargestFirstSelector) Name() string {
	return "random-improve-largest-first"
}

// Select returns a subset of available whose summed value covers target.
func (s *RandomImproveThenLargestFirstSelector) Select(available []common.Utxo, target Value) ([]common.Utxo, error) {
	primary := s.RandomImprove
	if primary == nil {
		primary = NewRandomImproveSelector()
	}
	selected, err := primary.Select(available, target)
	if err == nil {
		return selected, nil
	}
	if !errors.Is(err, errInsufficientUtxos) {
		return nil, fmt.Errorf("%s failed: %w", primary.Name(), err)
	}

	fallback := s.Fallback
	if fallback == nil {
		fallback = &LargestFirstSelector{}
	}
	selected, fallbackErr := fallback.Select(available, target)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%s failed: %w; %s failed: %w", primary.Name(), err, fallback.Name(), fallbackErr)
	}
	return selected, nil
}

type randomImproveCandidate struct {
	utxo  common.Utxo
	ref   string
	value Value
}

func randomImproveCandidates(available []common.Utxo) ([]randomImproveCandidate, error) {
	cands := make([]randomImproveCandidate, 0, len(available))
	for i := range available {
		amt := available[i].Output.Amount()
		if amt == nil || !amt.IsUint64() {
			return nil, fmt.Errorf("UTxO %s has an invalid lovelace amount", utxoRef(available[i]))
		}
		cands = append(cands, randomImproveCandidate{
			utxo: available[i],
			ref:  utxoRef(available[i]),
			value: NewValue(
				amt.Uint64(),
				CloneMultiAsset(available[i].Output.Assets()),
			),
		})
	}
	sort.Slice(cands, func(i, j int) bool {
		return cands[i].ref < cands[j].ref
	})
	return cands, nil
}

func randomImproveShuffle(cands []randomImproveCandidate, seed int64) []randomImproveCandidate {
	shuffled := make([]randomImproveCandidate, len(cands))
	copy(shuffled, cands)
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic pseudo-random ordering is required here.
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled
}

func randomImproveHasUntargetedAssets(value Value, target Value) bool {
	if value.Assets == nil {
		return false
	}
	for _, policy := range value.Assets.Policies() {
		for _, name := range value.Assets.Assets(policy) {
			qty := value.Assets.Asset(policy, name)
			if qty == nil || qty.Sign() <= 0 {
				continue
			}
			if target.Assets == nil {
				return true
			}
			targetQty := target.Assets.Asset(policy, name)
			if targetQty == nil || targetQty.Sign() <= 0 {
				return true
			}
		}
	}
	return false
}

func randomImproveWithinIdeal(value Value, ideal Value) bool {
	if value.Coin > ideal.Coin {
		return false
	}
	if ideal.Assets == nil {
		return true
	}
	if value.Assets == nil {
		return true
	}
	for _, policy := range ideal.Assets.Policies() {
		for _, name := range ideal.Assets.Assets(policy) {
			idealQty := ideal.Assets.Asset(policy, name)
			if idealQty == nil || idealQty.Sign() <= 0 {
				continue
			}
			valueQty := value.Assets.Asset(policy, name)
			if valueQty != nil && valueQty.Cmp(idealQty) > 0 {
				return false
			}
		}
	}
	return true
}

func randomImproveDistance(value Value, ideal Value) *big.Int {
	dist := new(big.Int).SetUint64(absDiffUint64(value.Coin, ideal.Coin))
	if ideal.Assets == nil {
		return dist
	}
	for _, policy := range sortedPolicies(ideal.Assets) {
		for _, name := range sortedAssetNames(ideal.Assets, policy) {
			idealQty := ideal.Assets.Asset(policy, name)
			if idealQty == nil || idealQty.Sign() <= 0 {
				continue
			}
			valueQty := big.NewInt(0)
			if value.Assets != nil {
				if qty := value.Assets.Asset(policy, name); qty != nil {
					valueQty = qty
				}
			}
			diff := new(big.Int).Sub(idealQty, valueQty)
			if diff.Sign() < 0 {
				diff.Neg(diff)
			}
			dist.Add(dist, diff)
		}
	}
	return dist
}

func absDiffUint64(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

func sortedPolicies(m *common.MultiAsset[common.MultiAssetTypeOutput]) []common.Blake2b224 {
	policies := m.Policies()
	sort.Slice(policies, func(i, j int) bool {
		return bytes.Compare(policies[i].Bytes(), policies[j].Bytes()) < 0
	})
	return policies
}

func sortedAssetNames(m *common.MultiAsset[common.MultiAssetTypeOutput], policy common.Blake2b224) [][]byte {
	names := m.Assets(policy)
	sort.Slice(names, func(i, j int) bool {
		return bytes.Compare(names[i], names[j]) < 0
	})
	return names
}
