package config

import (
	"errors"
	"os"

	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizulog"
)

type Config struct {
	Env   string `yaml:"env"`
	Port  string `yaml:"port"`
	Level string `yaml:"level"`
}

func init() {
	// Dependency Injection ---------------------------------------
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
	// ...

	// Logging ----------------------------------------------------
	mizulog.Initialize(nil, mizulog.WithLogLevel(c.Level))
}
