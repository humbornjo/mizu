package namastesvc

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu/mizuconnect/restful/bidikit"
	"github.com/humbornjo/mizu/mizudi"

	namastev1 "mizu.example/protogen/fooapp/namaste/v1"
	"mizu.example/protogen/fooapp/namaste/v1/namastev1connect"
)

type Service struct{}

var _ namastev1connect.NamasteServiceHandler = (*Service)(nil)

var NewService = mizudi.MustRetrieve[namastev1connect.NamasteServiceHandler]

func (s *Service) Namaste(
	ctx context.Context,
	stream *connect.BidiStream[namastev1.NamasteRequest, namastev1.NamasteResponse],
) error {

	for req, err := range bidikit.NewIterator(stream) {
		if err != nil {
			slog.ErrorContext(ctx, "failed receive stream request", "error", err)
			return connect.NewError(connect.CodeInternal, err)
		}

		name := req.GetName()
		if err := stream.Send(&namastev1.NamasteResponse{Message: "Hello " + name}); err != nil {
			slog.ErrorContext(ctx, "failed send stream response", "error", err)
			return connect.NewError(connect.CodeInternal, nil)
		}
	}

	return nil
}
