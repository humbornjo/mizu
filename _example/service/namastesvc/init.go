package namastesvc

import (
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"

	"mizu.example/config"
	"mizu.example/protogen/fooapp/namaste/v1/namastev1connect"
)

type Config struct {
	Berserk string `yaml:"berserk"`
}

func Initialize(_ *config.Config) {
	scp := mizudi.MustRetrieve[*mizuconnect.Scope]()
	scp.Register(&Service{}, namastev1connect.NewNamasteServiceHandler)
}
