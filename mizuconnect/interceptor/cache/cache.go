package cache

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu"
)

type interceptor struct {
}

type option func(*config)

type config struct {
	enableUnary                 bool
	enableCacheStreamingClient  bool
	enableCacheStreamingHandler bool

	identifier func(context.Context, connect.AnyRequest) (any, mizu.R[time.Duration], bool)
}

var defaultConfig = config{
	enableUnary:                 true,
	enableCacheStreamingClient:  false,
	enableCacheStreamingHandler: false,
	identifier: func(ctx context.Context, ar connect.AnyRequest) (any, mizu.R[time.Duration], bool) {
		return nil, mizu.None, false
	},
}

func WithCacheUnary(val bool) option {
	return func(c *config) {
		c.enableUnary = val
	}
}

func WithCacheStreamingClient(val bool) option {
	return func(c *config) {
		c.enableCacheStreamingClient = val
	}
}

func WithCacheStreamingHandler(val bool) option {
	return func(c *config) {
		c.enableCacheStreamingHandler = val
	}
}

func New() connect.Interceptor {
	interceptor := &interceptor{}
	return connect.UnaryInterceptorFunc(interceptor.WrapUnary)
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
		return next(ctx, ar)
	}
}

//
// func (i *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
//
// }
//
// func (i *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
//
// }
