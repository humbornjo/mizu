package config

import (
	"errors"
	"os"

	"github.com/humbornjo/mizu/mizudi"
)

type Config struct {
	Env string `yaml:"env"`
}

func init() {
	mizudi.Initialize()

	if err := mizudi.RevealConfig(os.Stdout); err != nil {
		if !errors.Is(err, mizudi.ErrNotInitialized) {
			panic(err)
		}
	}

	c := mizudi.Enchant[Config](nil)

	mizudi.Register(func() (*Config, error) { return c, nil })

	// e.g. Register Default Database using mizudi.Register and use
	// them across services.
}
