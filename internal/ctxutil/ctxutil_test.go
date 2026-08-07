package ctxutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestFromChanClose(t *testing.T) {
	ch := make(chan struct{})
	ctx, cancel := FromChan(ch)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context cancelled before channel closed")
	default:
	}

	close(ch)
	<-ctx.Done()
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestFromChanCancel(t *testing.T) {
	ch := make(chan struct{})
	ctx, cancel := FromChan(ch)

	cancel()
	<-ctx.Done()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}
