package cache

import (
	"context"
	"sync"
	"time"

	"github.com/blinklabs-io/gouroboros/ledger/common"

	"github.com/Salvionied/apollo/v2/backend"
)

// CachedChainContext wraps another ChainContext with time-based caching.
type CachedChainContext struct {
	inner backend.ChainContext
	ttl   time.Duration

	mu             sync.Mutex
	cachedParams   *backend.ProtocolParameters
	cachedGenesis  *backend.GenesisParameters
	paramsCacheAt  time.Time
	genesisCacheAt time.Time
}

// NewCachedChainContext creates a new cached wrapper around the given ChainContext.
func NewCachedChainContext(inner backend.ChainContext, ttl time.Duration) *CachedChainContext {
	return &CachedChainContext{
		inner: inner,
		ttl:   ttl,
	}
}

func (c *CachedChainContext) ProtocolParams(ctx context.Context) (backend.ProtocolParameters, error) {
	ctx = backend.ContextOrBackground(ctx)
	c.mu.Lock()
	if c.cachedParams != nil && time.Since(c.paramsCacheAt) < c.ttl {
		pp := *c.cachedParams
		// Deep copy CostModels to prevent callers from mutating the cache.
		if pp.CostModels != nil {
			cm := make(map[string][]int64, len(pp.CostModels))
			for k, v := range pp.CostModels {
				dup := make([]int64, len(v))
				copy(dup, v)
				cm[k] = dup
			}
			pp.CostModels = cm
		}
		c.mu.Unlock()
		return pp, nil
	}
	c.mu.Unlock()

	pp, err := c.inner.ProtocolParams(ctx)
	if err != nil {
		return pp, err
	}

	// Deep copy CostModels before storing to prevent callers from mutating the cache.
	cached := pp
	if cached.CostModels != nil {
		cm := make(map[string][]int64, len(cached.CostModels))
		for k, v := range cached.CostModels {
			dup := make([]int64, len(v))
			copy(dup, v)
			cm[k] = dup
		}
		cached.CostModels = cm
	}

	c.mu.Lock()
	c.cachedParams = &cached
	c.paramsCacheAt = time.Now()
	c.mu.Unlock()

	return pp, nil
}

func (c *CachedChainContext) GenesisParams(ctx context.Context) (backend.GenesisParameters, error) {
	ctx = backend.ContextOrBackground(ctx)
	c.mu.Lock()
	if c.cachedGenesis != nil && time.Since(c.genesisCacheAt) < c.ttl {
		gp := *c.cachedGenesis
		c.mu.Unlock()
		return gp, nil
	}
	c.mu.Unlock()

	gp, err := c.inner.GenesisParams(ctx)
	if err != nil {
		return gp, err
	}

	c.mu.Lock()
	c.cachedGenesis = &gp
	c.genesisCacheAt = time.Now()
	c.mu.Unlock()

	return gp, nil
}

func (c *CachedChainContext) NetworkId() uint8 {
	return c.inner.NetworkId()
}

func (c *CachedChainContext) CurrentEpoch(ctx context.Context) (uint64, error) {
	return c.inner.CurrentEpoch(backend.ContextOrBackground(ctx))
}

func (c *CachedChainContext) MaxTxFee(ctx context.Context) (uint64, error) {
	return c.inner.MaxTxFee(backend.ContextOrBackground(ctx))
}

func (c *CachedChainContext) Tip(ctx context.Context) (uint64, error) {
	return c.inner.Tip(backend.ContextOrBackground(ctx))
}

func (c *CachedChainContext) Utxos(ctx context.Context, address common.Address) ([]common.Utxo, error) {
	return c.inner.Utxos(backend.ContextOrBackground(ctx), address)
}

func (c *CachedChainContext) SubmitTx(ctx context.Context, txCbor []byte) (common.Blake2b256, error) {
	return c.inner.SubmitTx(backend.ContextOrBackground(ctx), txCbor)
}

func (c *CachedChainContext) EvaluateTx(ctx context.Context, txCbor []byte, additionalUtxos []common.Utxo) (map[common.RedeemerKey]common.ExUnits, error) {
	return c.inner.EvaluateTx(backend.ContextOrBackground(ctx), txCbor, additionalUtxos)
}

func (c *CachedChainContext) UtxoByRef(ctx context.Context, txHash common.Blake2b256, index uint32) (*common.Utxo, error) {
	return c.inner.UtxoByRef(backend.ContextOrBackground(ctx), txHash, index)
}

func (c *CachedChainContext) ScriptCbor(ctx context.Context, scriptHash common.Blake2b224) ([]byte, error) {
	return c.inner.ScriptCbor(backend.ContextOrBackground(ctx), scriptHash)
}
