package cacheintp

import (
	"context"
	"errors"
	"sync"
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

func TestCacheintp_WrapUnary_SingleFlightConcurrent(t *testing.T) {
	wantErr := errors.New("upstream error")

	cases := []struct {
		name    string
		retResp func() connect.AnyResponse
		retErr  error
	}{
		{
			name: "valid response",
			retResp: func() connect.AnyResponse {
				resp := connect.NewResponse(&emptypb.Empty{})
				resp.Header().Set("X-Test", "ok")
				return resp
			},
		},
		{
			name:    "nil response and error",
			retResp: func() connect.AnyResponse { return nil },
			retErr:  wantErr,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			started := make(chan struct{})
			release := make(chan struct{})

			next := connect.UnaryFunc(func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				return tc.retResp(), tc.retErr
			})

			itp := New(
				WithSingleFlight(true),
				WithKeyFunc(func(context.Context, connect.AnyRequest) (any, time.Duration) {
					return "shared-key", time.Minute
				}),
				WithCleanupArbiter(func(context.Context, connect.AnyResponse) bool { return false }),
			)
			unary := itp.WrapUnary(next)

			type result struct {
				resp connect.AnyResponse
				err  error
			}
			results := make([]result, 2)
			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				resp, err := unary(t.Context(), connect.NewRequest(&emptypb.Empty{}))
				results[0] = result{resp: resp, err: err}
			}()

			<-started

			go func() {
				defer wg.Done()
				resp, err := unary(t.Context(), connect.NewRequest(&emptypb.Empty{}))
				results[1] = result{resp: resp, err: err}
			}()

			time.Sleep(10 * time.Millisecond)
			close(release)
			wg.Wait()

			assert.EqualValues(t, 1, calls.Load(), "overlapping shared-key calls must coalesce")
			for idx, res := range results {
				if tc.retErr != nil {
					require.ErrorIs(t, res.err, tc.retErr, "caller %d error", idx)
					assert.Nil(t, res.resp, "caller %d resp", idx)
					continue
				}
				require.NoError(t, res.err, "caller %d error", idx)
				require.NotNil(t, res.resp, "caller %d resp", idx)
				assert.Equal(t, "ok", res.resp.Header().Get("X-Test"), "caller %d header", idx)
			}
			if tc.retErr == nil {
				results[0].resp.Header().Set("X-Test", "mutated")
				assert.Equal(t, "ok", results[1].resp.Header().Get("X-Test"), "callers must receive independent clones")
			}
		})
	}
}
