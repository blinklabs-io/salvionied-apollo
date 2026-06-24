package cache

import (
	"context"
	"testing"
	"time"

	"github.com/blinklabs-io/gouroboros/ledger/common"

	"github.com/Salvionied/apollo/v2/backend/fixed"
)

type contextRecordingChainContext struct {
	*fixed.FixedChainContext
	got context.Context
}

func (c *contextRecordingChainContext) Utxos(ctx context.Context, address common.Address) ([]common.Utxo, error) {
	c.got = ctx
	return c.FixedChainContext.Utxos(ctx, address)
}

func TestCachedChainContextPassesContextThrough(t *testing.T) {
	inner := &contextRecordingChainContext{FixedChainContext: fixed.NewEmptyFixedChainContext()}
	cached := NewCachedChainContext(inner, time.Minute)
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")

	_, err := cached.Utxos(ctx, common.Address{})
	if err != nil {
		t.Fatal(err)
	}
	if inner.got != ctx {
		t.Fatalf("inner backend got context %p, want %p", inner.got, ctx)
	}
}
