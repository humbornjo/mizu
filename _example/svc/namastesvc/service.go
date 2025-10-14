package namastesvc

import (
	"context"

	"connectrpc.com/connect"

	namastev1 "mizu.example/protogen/app_bar/namaste/v1"
	"mizu.example/protogen/app_bar/namaste/v1/namastev1connect"
)

type Service struct {
	namastev1connect.UnimplementedNamasteServiceHandler
}

func NewService() namastev1connect.NamasteServiceHandler {
	return &Service{}
}

func (s *Service) Namaste(ctx context.Context, req *connect.Request[namastev1.NamasteRequest],
) (*connect.Response[namastev1.NamasteResponse], error) {
	return connect.NewResponse(&namastev1.NamasteResponse{
		Message: "Namaste, " + req.Msg.Name,
	}), nil
}