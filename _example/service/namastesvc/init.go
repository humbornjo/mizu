package namastesvc

import (
	"github.com/humbornjo/mizu/mizudi"

	_ "mizu.example/config"
	"mizu.example/protogen/fooapp/namaste/v1/namastev1connect"
)

type Config struct {
	Berserk string `yaml:"berserk"`
}

func init() {
	mizudi.Register(func() (namastev1connect.NamasteServiceHandler, error) {
		return &Service{}, nil
	})

	c := mizudi.Enchant[Config](nil, mizudi.WithSubstitutePrefix(
		"service/namastesvc",
		"service/gutssvc",
	))

	if c.Berserk != "Guts" {
		panic("berserk not loaded")
	}
}
