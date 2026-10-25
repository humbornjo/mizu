package cacheintp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestCacheintp_WrapUnary_CacheHit(t *testing.T) {
	var calls atomic.Int64
	next := connect.UnaryFunc(func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
		calls.Add(1)
		return connect.NewResponse(&emptypb.Empty{}), nil
	})
	itp := New(
		WithKeyFunc(func(context.Context, connect.AnyRequest) (any, time.Duration) {
			return "key", time.Minute
		}),
		WithCleanupArbiter(func(context.Context, connect.AnyResponse) bool { return false }),
	)

	unary := itp.WrapUnary(next)
	req := connect.NewRequest(&emptypb.Empty{})
	for range 2 {
		resp, err := unary(t.Context(), req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	}
	assert.EqualValues(t, 1, calls.Load(), "second identical request must hit the cache")
}

func TestCacheintp_WrapUnary_ZeroExpiryBypasses(t *testing.T) {
	var calls atomic.Int64
	next := connect.UnaryFunc(func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
		calls.Add(1)
		return connect.NewResponse(&emptypb.Empty{}), nil
	})
	itp := New(WithKeyFunc(func(context.Context, connect.AnyRequest) (any, time.Duration) {
		return "key", 0
	}))

	unary := itp.WrapUnary(next)
	req := connect.NewRequest(&emptypb.Empty{})
	for range 2 {
		_, err := unary(t.Context(), req)
		require.NoError(t, err)
	}
	assert.EqualValues(t, 2, calls.Load(), "zero expiry must pass through uncached")
}

func TestCacheintp_WrapUnary_ExpiresAfterTTL(t *testing.T) {
	var calls atomic.Int64
	next := connect.UnaryFunc(func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
		calls.Add(1)
		return connect.NewResponse(&emptypb.Empty{}), nil
	})
	itp := New(
		WithKeyFunc(func(context.Context, connect.AnyRequest) (any, time.Duration) {
			return "key", 20 * time.Millisecond
		}),
		WithCleanupArbiter(func(context.Context, connect.AnyResponse) bool { return false }),
	)

	unary := itp.WrapUnary(next)
	req := connect.NewRequest(&emptypb.Empty{})
	for range 2 {
		_, err := unary(t.Context(), req)
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, calls.Load())

	time.Sleep(60 * time.Millisecond)
	_, err := unary(t.Context(), req)
	require.NoError(t, err)
	assert.EqualValues(t, 2, calls.Load(), "entry must expire after the keyFunc TTL")
}
