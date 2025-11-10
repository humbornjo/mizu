package greetsvc

import (
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"

	"mizu.example/config"
	"mizu.example/protogen/barapp/greet/v1/greetv1connect"
)

type Config struct {
	Greet string `yaml:"greet"`
}

var cfg *Config

func Initialize(global *config.Config) {
	cfg = mizudi.Enchant[Config](nil)
	if cfg.Greet == "" {
		cfg.Greet = "Hello"
	}

	scp := mizudi.MustRetrieve[*mizuconnect.Scope]()
	scp.Register(&Service{WhatToSay: cfg.Greet}, greetv1connect.NewGreetServiceHandler)
}
