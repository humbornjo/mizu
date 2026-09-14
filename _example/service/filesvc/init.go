package filesvc

import (
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"example.com/mizu/config"
	"example.com/mizu/package/storage"
	filev1 "example.com/mizu/protogen/barapp/file/v1"
	"example.com/mizu/protogen/barapp/file/v1/filev1connect"
)

type Config struct {
	ServePrefix string `json:"serve_prefix"`
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

	scope := mizudi.MustRetrieve[*mizuconnect.Scope]()
	scope.
		UseGateway(
			filev1.RegisterFileServiceHandlerFromEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		).
		Register(&Service{storage.NewStorage()}, filev1connect.NewFileServiceHandler)
}
