package greetsvc

import (
	"context"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu/mizudi"

	greetv1 "mizu.example/protogen/barapp/greet/v1"
	"mizu.example/protogen/barapp/greet/v1/greetv1connect"
)

type Service struct {
	WhatToSay string
}

var _ greetv1connect.GreetServiceHandler = (*Service)(nil)

var NewService = mizudi.MustRetrieve[greetv1connect.GreetServiceHandler]

func (s *Service) Greet(ctx context.Context, req *connect.Request[greetv1.GreetRequest],
) (*connect.Response[greetv1.GreetResponse], error) {
	return connect.NewResponse(&greetv1.GreetResponse{Message: s.WhatToSay + ", " + req.Msg.Name}), nil
}
