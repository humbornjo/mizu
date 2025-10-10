package namastesvc

import (
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"

	_ "mizu.example/config"
	"mizu.example/protogen/fooapp/namaste/v1/namastev1connect"
)

type Config struct {
	Berserk string `yaml:"berserk"`
}

func Initialize() {
	srv := mizudi.MustRetrieve[*mizuconnect.Scope]()
	srv.Register(&Service{}, namastev1connect.NewNamasteServiceHandler)
}
