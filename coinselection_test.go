package apollo

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger/common"
)

// makeSelectorUtxo builds a UTxO for selector tests with a deterministic ref.
func makeSelectorUtxo(t *testing.T, txHashByte byte, index uint32, lovelace uint64, assets *common.MultiAsset[common.MultiAssetTypeOutput]) common.Utxo {
	t.Helper()
	var txHash common.Blake2b256
	txHash[0] = txHashByte
	return makeAssetTestUtxo(t, txHash, index, lovelace, assets)
}

// makeTestAssets builds a MultiAsset with a single policy and asset name.
func makeTestAssets(policyByte byte, name string, qty int64) *common.MultiAsset[common.MultiAssetTypeOutput] {
	var policy common.Blake2b224
	policy[0] = policyByte
	return MultiAssetFromMap(map[common.Blake2b224]map[cbor.ByteString]*big.Int{
		policy: {cbor.NewByteString([]byte(name)): big.NewInt(qty)},
	})
}

func sumSelected(t *testing.T, selected []common.Utxo) Value {
	t.Helper()
	total := Value{}
	for _, u := range selected {
		amt := u.Output.Amount()
		if amt == nil || !amt.IsUint64() {
			t.Fatalf("selected UTxO %s has invalid amount", utxoRef(u))
		}
		uv := NewValue(amt.Uint64(), CloneMultiAsset(u.Output.Assets()))
		var err error
		total, err = total.Add(uv)
		if err != nil {
			t.Fatalf("sum overflow: %v", err)
		}
	}
	return total
}

// runSelectorConformance runs the shared conformance suite that any
// CoinSelector implementation must pass.
func runSelectorConformance(t *testing.T, newSelector func() CoinSelector) {
	t.Helper()

	t.Run("CoversAdaTarget", func(t *testing.T) {
		pool := []common.Utxo{
			makeSelectorUtxo(t, 0x01, 0, 3_000_000, nil),
			makeSelectorUtxo(t, 0x02, 0, 10_000_000, nil),
			makeSelectorUtxo(t, 0x03, 0, 5_000_000, nil),
		}
		target := NewSimpleValue(12_000_000)
		selected, err := newSelector().Select(pool, target)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if !sumSelected(t, selected).GreaterOrEqual(target) {
			t.Errorf("selection does not cover target")
		}
	})

	t.Run("CoversMultiAssetTarget", func(t *testing.T) {
		pool := []common.Utxo{
			makeSelectorUtxo(t, 0x01, 0, 2_000_000, makeTestAssets(0xAA, "tokenA", 100)),
			makeSelectorUtxo(t, 0x02, 0, 10_000_000, nil),
			makeSelectorUtxo(t, 0x03, 0, 2_000_000, makeTestAssets(0xBB, "tokenB", 7)),
		}
		target := NewValue(5_000_000, makeTestAssets(0xAA, "tokenA", 50))
		selected, err := newSelector().Select(pool, target)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if !sumSelected(t, selected).GreaterOrEqual(target) {
			t.Errorf("selection does not cover multi-asset target")
		}
	})

	t.Run("NoDuplicateSelections", func(t *testing.T) {
		pool := []common.Utxo{
			makeSelectorUtxo(t, 0x01, 0, 2_000_000, makeTestAssets(0xAA, "tokenA", 100)),
			makeSelectorUtxo(t, 0x02, 0, 3_000_000, nil),
			makeSelectorUtxo(t, 0x03, 0, 4_000_000, nil),
		}
		target := NewValue(8_000_000, makeTestAssets(0xAA, "tokenA", 100))
		selected, err := newSelector().Select(pool, target)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		seen := make(map[string]bool)
		for _, u := range selected {
			ref := utxoRef(u)
			if seen[ref] {
				t.Errorf("UTxO %s selected twice", ref)
			}
			seen[ref] = true
		}
	})

	t.Run("ErrorOnInsufficientCoin", func(t *testing.T) {
		pool := []common.Utxo{
			makeSelectorUtxo(t, 0x01, 0, 1_000_000, nil),
		}
		_, err := newSelector().Select(pool, NewSimpleValue(100_000_000))
		if err == nil {
			t.Fatal("expected error for insufficient coin")
		}
		if !strings.Contains(err.Error(), "insufficient") {
			t.Errorf("expected insufficiency error, got: %v", err)
		}
	})

	t.Run("ErrorOnMissingAsset", func(t *testing.T) {
		pool := []common.Utxo{
			makeSelectorUtxo(t, 0x01, 0, 10_000_000, nil),
		}
		target := NewValue(1_000_000, makeTestAssets(0xAA, "tokenA", 1))
		_, err := newSelector().Select(pool, target)
		if err == nil {
			t.Fatal("expected error for missing asset")
		}
		if !strings.Contains(err.Error(), "insufficient") {
			t.Errorf("expected insufficiency error, got: %v", err)
		}
	})

	t.Run("EmptyTargetSelectsNothing", func(t *testing.T) {
		pool := []common.Utxo{
			makeSelectorUtxo(t, 0x01, 0, 1_000_000, nil),
		}
		selected, err := newSelector().Select(pool, Value{})
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if len(selected) != 0 {
			t.Errorf("expected empty selection for empty target, got %d UTxOs", len(selected))
		}
	})

	t.Run("Deterministic", func(t *testing.T) {
		pool := []common.Utxo{
			makeSelectorUtxo(t, 0x01, 0, 2_000_000, makeTestAssets(0xAA, "tokenA", 30)),
			makeSelectorUtxo(t, 0x02, 0, 2_000_000, makeTestAssets(0xAA, "tokenA", 30)),
			makeSelectorUtxo(t, 0x03, 0, 5_000_000, nil),
			makeSelectorUtxo(t, 0x04, 0, 5_000_000, nil),
		}
		target := NewValue(6_000_000, makeTestAssets(0xAA, "tokenA", 40))
		first, err := newSelector().Select(pool, target)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		second, err := newSelector().Select(pool, target)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if len(first) != len(second) {
			t.Fatalf("non-deterministic selection size: %d vs %d", len(first), len(second))
		}
		for i := range first {
			if utxoRef(first[i]) != utxoRef(second[i]) {
				t.Errorf("non-deterministic selection at %d: %s vs %s", i, utxoRef(first[i]), utxoRef(second[i]))
			}
		}
	})
}

func TestLargestFirstSelectorConformance(t *testing.T) {
	runSelectorConformance(t, func() CoinSelector { return &LargestFirstSelector{} })
}

func TestRandomImproveSelectorConformance(t *testing.T) {
	runSelectorConformance(t, func() CoinSelector { return NewRandomImproveSelector() })
}

func TestRandomImproveThenLargestFirstSelectorConformance(t *testing.T) {
	runSelectorConformance(t, func() CoinSelector { return NewRandomImproveThenLargestFirstSelector() })
}

func TestLargestFirstSelectorName(t *testing.T) {
	if name := (&LargestFirstSelector{}).Name(); name != "largest-first" {
		t.Errorf("expected name largest-first, got %q", name)
	}
}

func TestRandomImproveSelectorName(t *testing.T) {
	if name := NewRandomImproveSelector().Name(); name != "random-improve" {
		t.Errorf("expected name random-improve, got %q", name)
	}
}

func TestRandomImproveThenLargestFirstSelectorName(t *testing.T) {
	if name := NewRandomImproveThenLargestFirstSelector().Name(); name != "random-improve-largest-first" {
		t.Errorf("expected name random-improve-largest-first, got %q", name)
	}
}

// TestLargestFirstSelectorOrder pins the exact legacy behavior: ADA-only
// UTxOs are consumed first in descending lovelace order, before any
// asset-carrying UTxOs.
func TestLargestFirstSelectorOrder(t *testing.T) {
	withAssets := makeSelectorUtxo(t, 0x0A, 0, 20_000_000, makeTestAssets(0xAA, "tokenA", 5))
	pool := []common.Utxo{
		makeSelectorUtxo(t, 0x01, 0, 3_000_000, nil),
		withAssets,
		makeSelectorUtxo(t, 0x02, 0, 10_000_000, nil),
		makeSelectorUtxo(t, 0x03, 0, 5_000_000, nil),
	}
	selected, err := (&LargestFirstSelector{}).Select(pool, NewSimpleValue(12_000_000))
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("expected 2 UTxOs selected, got %d", len(selected))
	}
	if got := selected[0].Output.Amount().Uint64(); got != 10_000_000 {
		t.Errorf("expected first selection of 10 ADA, got %d lovelace", got)
	}
	if got := selected[1].Output.Amount().Uint64(); got != 5_000_000 {
		t.Errorf("expected second selection of 5 ADA, got %d lovelace", got)
	}
}

func TestRandomImproveSelectorImprovesTowardDoubleTarget(t *testing.T) {
	pool := []common.Utxo{
		makeSelectorUtxo(t, 0x01, 0, 10_000_000, nil),
		makeSelectorUtxo(t, 0x02, 0, 5_000_000, nil),
	}
	selected, err := NewRandomImproveSelector().Select(pool, NewSimpleValue(10_000_000))
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("expected improvement to add second UTxO, got %d selections", len(selected))
	}
	if got := sumSelected(t, selected).Coin; got != 15_000_000 {
		t.Errorf("expected improved total of 15 ADA, got %d lovelace", got)
	}
}

func TestRandomImproveSelectorDoesNotImprovePastDoubleTarget(t *testing.T) {
	pool := []common.Utxo{
		makeSelectorUtxo(t, 0x01, 0, 10_000_000, nil),
		makeSelectorUtxo(t, 0x02, 0, 11_000_000, nil),
	}
	selected, err := NewRandomImproveSelector().Select(pool, NewSimpleValue(10_000_000))
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("expected improvement to skip overshooting UTxO, got %d selections", len(selected))
	}
	if got := sumSelected(t, selected).Coin; got > 20_000_000 {
		t.Errorf("selection exceeded twice the target: %d lovelace", got)
	}
}

func TestRandomImproveSelectorImprovesTargetAssets(t *testing.T) {
	pool := []common.Utxo{
		makeSelectorUtxo(t, 0x01, 0, 2_000_000, makeTestAssets(0xAA, "tokenA", 10)),
		makeSelectorUtxo(t, 0x02, 0, 1_000_000, makeTestAssets(0xAA, "tokenA", 5)),
	}
	target := NewValue(2_000_000, makeTestAssets(0xAA, "tokenA", 10))
	selected, err := NewRandomImproveSelector().Select(pool, target)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	got := sumSelected(t, selected)
	if got.Coin != 3_000_000 {
		t.Fatalf("expected improved coin total of 3 ADA, got %d lovelace", got.Coin)
	}
	var policy common.Blake2b224
	policy[0] = 0xAA
	qty := got.Assets.Asset(policy, []byte("tokenA"))
	if qty == nil || qty.Cmp(big.NewInt(15)) != 0 {
		t.Fatalf("expected improved token total of 15, got %v", qty)
	}
}

type alwaysFailSelector struct{}

func (s alwaysFailSelector) Name() string { return "always-fail" }

func (s alwaysFailSelector) Select([]common.Utxo, Value) ([]common.Utxo, error) {
	return nil, errors.New("forced failure")
}

type alwaysInsufficientSelector struct{}

func (s alwaysInsufficientSelector) Name() string { return "always-insufficient" }

func (s alwaysInsufficientSelector) Select([]common.Utxo, Value) ([]common.Utxo, error) {
	return nil, errInsufficientUtxos
}

func TestRandomImproveThenLargestFirstSelectorFallsBack(t *testing.T) {
	pool := []common.Utxo{
		makeSelectorUtxo(t, 0x01, 0, 3_000_000, nil),
		makeSelectorUtxo(t, 0x02, 0, 10_000_000, nil),
		makeSelectorUtxo(t, 0x03, 0, 5_000_000, nil),
	}
	selector := &RandomImproveThenLargestFirstSelector{
		RandomImprove: alwaysInsufficientSelector{},
		Fallback:      &LargestFirstSelector{},
	}
	selected, err := selector.Select(pool, NewSimpleValue(12_000_000))
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("expected 2 UTxOs selected by fallback, got %d", len(selected))
	}
	if got := selected[0].Output.Amount().Uint64(); got != 10_000_000 {
		t.Errorf("expected fallback to select 10 ADA first, got %d lovelace", got)
	}
	if got := selected[1].Output.Amount().Uint64(); got != 5_000_000 {
		t.Errorf("expected fallback to select 5 ADA second, got %d lovelace", got)
	}
}

func TestRandomImproveThenLargestFirstSelectorDoesNotFallbackOnPrimaryError(t *testing.T) {
	pool := []common.Utxo{
		makeSelectorUtxo(t, 0x01, 0, 10_000_000, nil),
	}
	fallback := &recordingSelector{inner: &LargestFirstSelector{}}
	selector := &RandomImproveThenLargestFirstSelector{
		RandomImprove: alwaysFailSelector{},
		Fallback:      fallback,
	}
	_, err := selector.Select(pool, NewSimpleValue(1_000_000))
	if err == nil {
		t.Fatal("expected primary error")
	}
	if fallback.called {
		t.Fatal("fallback should not run for non-selection primary errors")
	}
	if !strings.Contains(err.Error(), "forced failure") {
		t.Fatalf("expected forced failure error, got: %v", err)
	}
}

func TestRandomImproveThenLargestFirstSelectorDoesNotFallbackOnInvalidAmount(t *testing.T) {
	good := makeSelectorUtxo(t, 0x01, 0, 10_000_000, nil)
	badBase := makeSelectorUtxo(t, 0x02, 0, 1_000_000, nil)
	tooBig := new(big.Int).Lsh(big.NewInt(1), 64)
	bad := common.Utxo{Id: badBase.Id, Output: badAmountOutput{badBase.Output, tooBig}}
	fallback := &recordingSelector{inner: &LargestFirstSelector{}}
	selector := &RandomImproveThenLargestFirstSelector{
		Fallback: fallback,
	}

	_, err := selector.Select([]common.Utxo{good, bad}, NewSimpleValue(1_000_000))
	if err == nil {
		t.Fatal("expected invalid amount error")
	}
	if fallback.called {
		t.Fatal("fallback should not run for invalid backend amount errors")
	}
	if !strings.Contains(err.Error(), "invalid lovelace amount") {
		t.Fatalf("expected invalid lovelace amount error, got: %v", err)
	}
}

func TestRandomImproveThenLargestFirstSelectorDoesNotFallbackOnOverflow(t *testing.T) {
	maxUint64 := ^uint64(0)
	pool := []common.Utxo{
		makeSelectorUtxo(t, 0x01, 0, maxUint64-1, nil),
		makeSelectorUtxo(t, 0x02, 0, 2, nil),
	}
	fallback := &recordingSelector{inner: &LargestFirstSelector{}}
	selector := &RandomImproveThenLargestFirstSelector{
		Fallback: fallback,
	}

	_, err := selector.Select(pool, NewSimpleValue(maxUint64))
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if fallback.called {
		t.Fatal("fallback should not run for Random-Improve overflow errors")
	}
	if !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("expected overflow error, got: %v", err)
	}
}

// recordingSelector wraps a CoinSelector and records that it was invoked,
// proving the builder dispatches to the configured selector.
type recordingSelector struct {
	inner  CoinSelector
	called bool
}

func (r *recordingSelector) Name() string { return "recording" }

func (r *recordingSelector) Select(available []common.Utxo, target Value) ([]common.Utxo, error) {
	r.called = true
	return r.inner.Select(available, target)
}

func TestSetCoinSelectorUsedByComplete(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	rec := &recordingSelector{inner: &LargestFirstSelector{}}
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(NewExternalWallet(addr)).
		AddPayment(p).
		SetTtl(50000000).
		SetCoinSelector(rec)

	if _, err := a.Complete(); err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if !rec.called {
		t.Error("custom coin selector was not invoked by Complete")
	}
}
