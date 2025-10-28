package filesvc

import (
	"github.com/humbornjo/mizu/mizudi"

	"mizu.example/config"
	"mizu.example/protogen/barapp/file/v1/filev1connect"
)

type Config struct {
	ServePrefix string `yaml:"serve_prefix"`
}

func init() {
	// Extract service config
	c := mizudi.Enchant[Config](nil)

	// Retrieve global config
	global := mizudi.MustRetrieve[*config.Config]()

	switch global.Env {
	case "local":
		// do something
	case "dev":
		// do something
	case "prod":
		// do something
	}

	if global.Env != "local" {
		panic("env not loaded")
	}

	if c.ServePrefix != "mycustomprefix" {
		panic("serve prefix not loaded")
	}

	mizudi.Register(func() (filev1connect.FileServiceHandler, error) {
		return &Service{}, nil
	})
}
