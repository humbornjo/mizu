package config

import "github.com/humbornjo/mizu/mizudi"

type Config struct {
	Env string `yaml:"env"`
}

func init() {
	mizudi.Init()

	c := mizudi.Enchant[Config](nil)

	mizudi.Register(func() (*Config, error) { return c, nil })

	// e.g. Register Default Database using mizudi.Register and use
	// them across services.
}
