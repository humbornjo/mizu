package mizudi

import (
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Option func(*config)

type config struct {
	*koanf.Koanf
	root string

	paths []string
}

func InitConfig(root string, opts ...Option) config {
	k, parser := koanf.New("/"), yaml.Parser()

	c := config{Koanf: k, root: root}
	for _, opt := range opts {
		opt(&c)
	}

	if c.root != "" {
		c.paths = append(c.paths, "./local.yaml")
	}
	for _, path := range c.paths {
		if err := k.Load(file.Provider(path), parser); err != nil {
			panic(err)
		}
	}

	if err := k.Load(env.Provider("MIZU_", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(
			strings.TrimPrefix(s, "MIZU_")), "_", ".")
	}), nil); err != nil {
		panic(err)
	}

	return c
}

func Enchant[T any](config config, opts ...Option) *T {
	typ := new(T)
	unmarshalConf := koanf.UnmarshalConf{Tag: "yaml"}

	if err := config.UnmarshalWithConf("", typ, unmarshalConf); err != nil {
		panic(err)
	}

	return typ
}
