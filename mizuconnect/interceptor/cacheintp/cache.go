package cacheintp

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"time"
	"unsafe"

	"connectrpc.com/connect"
	"golang.org/x/sync/singleflight"

	"github.com/humbornjo/mizu/x/cachex"
)

// INFO: init check the type structure of connect.Response[T] to make sure
// the clone() method works as expected.
func init() {
	st := connect.Response[struct{}]{}
	var _ connect.AnyResponse = &st

	fieldHeader := reflect.ValueOf(st).Type().Field(1)
	if fieldHeader.Name != "header" || fieldHeader.Type.Name() != "Header" {
		panic("Breaking change in current version of Connect RPC, header field not found")
	}

	fieldTrailer := reflect.ValueOf(st).Type().Field(2)
	if fieldTrailer.Name != "trailer" || fieldTrailer.Type.Name() != "Header" {
		panic("Breaking change in current version of Connect RPC, trailer field not found")
	}
}

type interceptor struct {
	cache *cachex.TTL[any, connect.AnyResponse]
	singleflight.Group

	enableSingleFlight bool
	keyFunc            func(context.Context, connect.AnyRequest) (any, time.Duration)
	cleanupArbiter     func(context.Context, connect.AnyResponse) bool
	jitterFunc         func(expiry time.Duration) time.Duration
}

type option func(*config)

type config struct {
	enableSingleFlight bool
	keyFunc            func(context.Context, connect.AnyRequest) (any, time.Duration)
	cleanupArbiter     func(context.Context, connect.AnyResponse) bool

	jitterFunc func(expiry time.Duration) time.Duration
}

var defaultConfig = config{
	enableSingleFlight: false,
	keyFunc: func(ctx context.Context, ar connect.AnyRequest) (any, time.Duration) {
		return nil, 0
	},
	cleanupArbiter: func(ctx context.Context, ar connect.AnyResponse) bool {
		// nolint:gosec
		return rand.IntN(1_000) == 0
	},

	jitterFunc: func(expiry time.Duration) time.Duration {
		// nolint:gosec
		return time.Duration(expiry.Nanoseconds() - rand.Int64N(expiry.Nanoseconds()/10))
	},
}

// WithSingleFlight enables or disables single-flight request
// deduplication. When enabled, concurrent identical requests will be
// coalesced into a single request to prevent duplicate work. Default is
// disabled (false).
func WithSingleFlight(val bool) option {
	return func(c *config) {
		c.enableSingleFlight = val
	}
}

// WithKeyFunc sets a custom function to generate cache keys and determine
// cache expiration time from requests. The function should return nil key
// and 0 duration for requests that should not be cached. Default function
// returns nil key and 0 duration (no caching).
func WithKeyFunc(f func(context.Context, connect.AnyRequest) (any, time.Duration)) option {
	return func(c *config) {
		if f == nil {
			return
		}
		c.keyFunc = f
	}
}

// WithJitterFunc sets a custom function to add jitter to cache expiration
// times. This helps prevent cache stampedes by spreading out expiration
// times. The function receives the original expiry duration and should
// return the adjusted duration. Default function reduces expiry by up to
// 10%.
func WithJitterFunc(f func(expiry time.Duration) time.Duration) option {
	return func(c *config) {
		if f == nil {
			return
		}
		c.jitterFunc = f
	}
}

// WithCleanupArbiter sets a custom function to determine when cache
// cleanup should occur. The function receives the response and should
// return true to trigger cleanup of expired entries. Default function
// triggers cleanup randomly with 0.1% probability (1 in 1000 requests).
func WithCleanupArbiter(f func(context.Context, connect.AnyResponse) bool) option {
	return func(c *config) {
		if f == nil {
			return
		}
		c.cleanupArbiter = f
	}
}

// New creates a new cache interceptor with the given options. The
// interceptor provides response caching for Connect RPC unary calls with
// support for single-flight deduplication, custom key generation, jittered
// expiration, and automatic cleanup of expired entries.
func New(opts ...option) connect.Interceptor {
	config := defaultConfig
	for _, opt := range opts {
		opt(&config)
	}
	interceptor := &interceptor{
		cache:              cachex.NewTTL[any, connect.AnyResponse](0),
		enableSingleFlight: config.enableSingleFlight,
		keyFunc:            config.keyFunc,
		cleanupArbiter:     config.cleanupArbiter,
		jitterFunc:         config.jitterFunc,
	}

	return connect.UnaryInterceptorFunc(interceptor.WrapUnary)
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
		key, expiry := i.keyFunc(ctx, ar)
		if expiry == 0 {
			return next(ctx, ar)
		}

		if resp, ok := i.cache.Get(key); ok {
			return clone(resp), nil
		}

		var resp connect.AnyResponse
		var err error

		defer func() {
			if i.cleanupArbiter(ctx, resp) {
				i.cache.Sweep()
			}
		}()

		if !i.enableSingleFlight {
			resp, err = next(ctx, ar)
			if err != nil {
				return resp, err
			}
			i.set(key, resp, expiry)
			return resp, nil
		}

		_, err, _ = i.Do(fmt.Sprintf("%T:%v", key, key), func() (any, error) {
			resp, err = next(ctx, ar)
			if err != nil {
				return resp, err
			}
			i.set(key, resp, expiry)
			return resp, nil
		})
		if err != nil {
			return clone(resp), err
		}
		return clone(resp), nil
	}
}

// set stores resp under key against a jittered deadline.
func (i *interceptor) set(key any, resp connect.AnyResponse, expiry time.Duration) {
	i.cache.Set(key, resp, time.Now().Add(i.jitterFunc(expiry)))
}

func clone(response connect.AnyResponse) connect.AnyResponse {
	st := reflect.ValueOf(response).Elem()
	if st.IsZero() {
		return response
	}

	newResp := reflect.New(st.Type())
	newResp.Elem().Set(st)

	fieldHeader := newResp.Elem().Field(1)
	settableHeader := reflect.NewAt(
		fieldHeader.Type(),
		unsafe.Pointer(fieldHeader.UnsafeAddr()), // nolint: gosec
	).Elem()
	settableHeader.Set(reflect.ValueOf(response.Header().Clone()))

	fieldTrailer := newResp.Elem().Field(2)
	settableTrailer := reflect.NewAt(
		fieldTrailer.Type(),
		unsafe.Pointer(fieldTrailer.UnsafeAddr()), // nolint: gosec
	).Elem()
	settableTrailer.Set(reflect.ValueOf(response.Trailer().Clone()))

	return newResp.Interface().(connect.AnyResponse)
}
