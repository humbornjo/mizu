package greetsvc

import (
	"context"

	"connectrpc.com/connect"

	greetv1 "mizu.example/protogen/barapp/greet/v1"
	"mizu.example/protogen/barapp/greet/v1/greetv1connect"
)

type Service struct{}

var _ greetv1connect.GreetServiceHandler = (*Service)(nil)

func NewService() greetv1connect.GreetServiceHandler {
	return &Service{}
}

func (s *Service) Greet(ctx context.Context, req *connect.Request[greetv1.GreetRequest],
) (*connect.Response[greetv1.GreetResponse], error) {
	return connect.NewResponse(&greetv1.GreetResponse{Message: "Hello, " + req.Msg.Name}), nil
}
