package greetsvc

import (
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"example.com/mizu/config"
	greetv1 "example.com/mizu/protogen/barapp/greet/v1"
	"example.com/mizu/protogen/barapp/greet/v1/greetv1connect"
)

type Config struct {
	Greet string `json:"greet"`
}

var cfg *Config

func Initialize(global *config.Config) {
	cfg = mizudi.Enchant[Config](nil)
	if cfg.Greet == "" {
		cfg.Greet = "Hello"
	}

	scope := mizudi.MustRetrieve[*mizuconnect.Scope]()
	scope.
		UseGateway(
			greetv1.RegisterGreetServiceHandlerFromEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		).
		Register(&Service{WhatToSay: cfg.Greet}, greetv1connect.NewGreetServiceHandler)
}
