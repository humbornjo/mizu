package debug

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
)

type interceptor struct {
}

func NewInterceptor() connect.Interceptor {
	interceptor := &interceptor{}
	return connect.UnaryInterceptorFunc(interceptor.WrapUnary)
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
		slog.InfoContext(ctx, "unary request", "request", ar.Spec().Procedure)
		return next(ctx, ar)
	}
}
