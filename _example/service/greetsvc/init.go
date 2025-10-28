package greetsvc

import (
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

func init() {
	config = mizudi.Enchant[Config](nil)
	if config.Greet == "" {
		config.Greet = "Hello"
	}

	mizudi.Register(func() (greetv1connect.GreetServiceHandler, error) {
		return &Service{WhatToSay: config.Greet}, nil
	})
}
