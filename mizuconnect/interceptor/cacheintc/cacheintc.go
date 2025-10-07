package cacheintc

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/sync/singleflight"
)

type interceptor struct {
	cache
	singleflight.Group

	enableSingleFlight bool
	keyFunc            func(context.Context, connect.AnyRequest) (any, time.Duration)
	cleanupChecker     func(context.Context, connect.AnyResponse) bool
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

func WithSingleFlight(val bool) option {
	return func(c *config) {
		c.enableSingleFlight = val
	}
}

func WithJitter(f func(expiry time.Duration) time.Duration) option {
	return func(c *config) {
		if f == nil {
			return
		}
		c.jitterFunc = f
	}
}

func New(opts ...option) connect.Interceptor {
	config := defaultConfig
	for _, opt := range opts {
		opt(&config)
	}
	interceptor := &interceptor{
		cache:              cache{mp: &sync.Map{}, jitterFunc: config.jitterFunc},
		enableSingleFlight: config.enableSingleFlight,
		keyFunc:            config.keyFunc,
	}

	return connect.UnaryInterceptorFunc(interceptor.WrapUnary)
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
		key, expiry := i.keyFunc(ctx, ar)
		if expiry == 0 {
			return next(ctx, ar)
		}

		if resp, ok := i.Get(key); ok {
			return resp, nil
		}

		var resp connect.AnyResponse
		var err error
		if !i.enableSingleFlight {
			resp, err = next(ctx, ar)
			if err != nil {
				return resp, err
			}
			i.Set(key, resp, expiry)
		} else {
			_, _, _ = i.Do(fmt.Sprintf("%T:%v", key, key), func() (any, error) {
				resp, err = next(ctx, ar)
				if err != nil {
					return resp, err
				}
				i.Set(key, resp, expiry)
				return resp, nil
			})
			if err != nil {
				return resp, err
			}
		}

		if i.cleanupChecker(ctx, resp) {
			for key, val := range i.mp.Range {
				if e := val.(*entry); e.expiration.Before(time.Now()) {
					i.mp.Delete(key)
				}
			}
		}

		return resp, err
	}
}

type entry struct {
	expiration time.Time
	value      connect.AnyResponse
}

type cache struct {
	mp         *sync.Map
	jitterFunc func(expiry time.Duration) time.Duration
}

func (c cache) Get(key any) (connect.AnyResponse, bool) {
	v, ok := c.mp.Load(key)
	if !ok {
		return nil, false
	}

	e := v.(*entry)
	if e.expiration.Before(time.Now()) {
		c.mp.Delete(key)
		return nil, false
	}
	return e.value, true
}

func (c cache) Set(key any, value connect.AnyResponse, expiry time.Duration) {
	c.mp.Store(key, &entry{
		value:      value,
		expiration: time.Now().Add(c.jitterFunc(expiry)),
	})
}
