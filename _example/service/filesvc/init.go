package filesvc

import (
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"

	"mizu.example/config"
	"mizu.example/package/storage"
	"mizu.example/protogen/barapp/file/v1/filev1connect"
)

type Config struct {
	ServePrefix string `yaml:"serve_prefix"`
}

func Initialize(global *config.Config) {
	// Extract service config
	local := mizudi.Enchant[Config](nil)

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

	if local.ServePrefix != "mycustomprefix" {
		panic("serve prefix not loaded")
	}

	scp := mizudi.MustRetrieve[*mizuconnect.Scope]()
	scp.Register(&Service{storage.NewStorage()}, filev1connect.NewFileServiceHandler)
}
