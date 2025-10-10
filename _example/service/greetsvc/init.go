package greetsvc

import (
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"

	// INFO: root config collect should be decleared to ensure the
	// dependency
	_ "mizu.example/config"
	"mizu.example/protogen/barapp/greet/v1/greetv1connect"
)

type Config struct {
	Greet string `yaml:"greet"`
}

var config *Config

func Initialize() {
	config = mizudi.Enchant[Config](nil)
	if config.Greet == "" {
		config.Greet = "Hello"
	}

	srv := mizudi.MustRetrieve[*mizuconnect.Scope]()
	srv.Register(&Service{WhatToSay: config.Greet}, greetv1connect.NewGreetServiceHandler)
}
