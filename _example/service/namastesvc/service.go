package namastesvc

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"

	namastev1 "mizu.example/protogen/fooapp/namaste/v1"
	"mizu.example/protogen/fooapp/namaste/v1/namastev1connect"
)

type Service struct{}

var _ namastev1connect.NamasteServiceHandler = (*Service)(nil)

func (s *Service) Namaste(
	ctx context.Context,
	req *connect.Request[namastev1.NamasteRequest],
	stream *connect.ServerStream[namastev1.NamasteResponse],
) error {
	name := req.Msg.GetName()

	if err := stream.Send(&namastev1.NamasteResponse{Message: "Hello " + name}); err != nil {
		slog.ErrorContext(ctx, "failed send stream response", "error", err)
		return connect.NewError(connect.CodeInternal, nil)
	}

	return nil
}
