package greetsvc

import (
	"github.com/humbornjo/mizu/mizudi"

	// INFO: root config collect should be decleared to ensure the
	// dependency
	_ "mizu.example/config"
)

type Config struct {
	Greet string `yaml:"greet"`
}

var config *Config

func init() {
	config = mizudi.Enchant[Config](nil)
	if config.Greet == "" {
		config.Greet = "Hello"
	}
}
