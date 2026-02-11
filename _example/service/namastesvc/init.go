package namastesvc

import (
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"mizu.example/config"
	namastev1 "mizu.example/protogen/fooapp/namaste/v1"
	"mizu.example/protogen/fooapp/namaste/v1/namastev1connect"
)

type Config struct {
	Berserk string `yaml:"berserk"`
}

func Initialize(_ *config.Config) {
	scope := mizudi.MustRetrieve[*mizuconnect.Scope]()
	scope.
		UseGateway(
			namastev1.RegisterNamasteServiceHandlerFromEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		).
		Register(&Service{}, namastev1connect.NewNamasteServiceHandler)
}
