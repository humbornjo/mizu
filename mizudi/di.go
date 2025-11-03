// Package mizudi provides dependency injection and configuration
// management for Go applications with automatic configuration
// loading.
//
// The package offers two main functionalities:
//  1. Configuration management through YAML files and
//     environment variables
//  2. Dependency injection using the samber/do library
package mizudi

import (
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"slices"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

const _PATH_SEPARATOR = string(os.PathSeparator)

var (
	_ROOT     string
	_INJECTOR = do.New()
	_KOANF    *koanf.Koanf

	ErrNotInitialized = fmt.Errorf("mizudi is not initialized")
)

// Option represents a configuration option for mizudi package.
// Options can be used to customize the behavior of Enchant.
type Option func(*config)

type config struct {
	substituteMap map[string]string
}

// WithSubstitutePrefix is an option that allows you to specify a
// mapping of strings to be replaced in configuration paths. An
// empty second input parameter is with the trim semantics.
//
// Example:
//
//	# service/greetsvc/config/config.go
//	mizudi.Enchant[Config](nil,
//	  mizudi.WithSubstitutePrefix("service/greetsvc/config", "service/greet"),
//	)
//
// The configuration under path "service/greetsvc" will be loaded
// even if the current directory is "service/greetsvc/config/".
func WithSubstitutePrefix(from string, to string) Option {
	return func(c *config) {
		c.substituteMap[from] = to
	}
}

// Init initializes the mizudi package with the provided options.
// It sets up the configuration system by loading YAML files and
// environment variables. `relativePath` is the relative path to
// the current directory from repository root.
//
// The function automatically determines the compiling time
// prefix and loads configuration from the specified paths (or
// defaults to "local.yaml" in the current working directory).
//
// Environment variables with prefix "MIZU_" are automatically
// loaded and mapped to configuration paths (e.g., MIZU_DB_HOST
// becomes db.host).
func Initialize(relativePath string, loadPaths ...string) {
	if _KOANF != nil {
		panic("mizudi already initialized")
	}

	// Extract compiling root directory
	_, runtimePath, _, ok := runtime.Caller(1)
	if !ok {
		panic("failed to get runtime caller")
	}
	dir := path.Dir(runtimePath)
	root := strings.TrimSuffix(dir, relativePath)
	_ROOT = strings.TrimSuffix(root, _PATH_SEPARATOR)

	// Load config
	k, parser := koanf.New("/"), yaml.Parser()
	_KOANF = k
	if len(loadPaths) == 0 {
		wd, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		loadPaths = []string{path.Join(wd, "local.yaml")}
	}
	for _, path := range loadPaths {
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err := k.Load(file.Provider(path), parser); err != nil {
			panic(err)
		}
	}
	if err := k.Load(env.Provider("MIZU_", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "MIZU_")), "_", ".")
	}), nil); err != nil {
		panic(err)
	}
}

// Reveal prints the loaded configuration to the provided
// io.Writer. This function should be used after calling
// Initialize.
func RevealConfig(tx io.Writer) error {
	if _KOANF == nil {
		return ErrNotInitialized
	}

	bytes, err := _KOANF.Marshal(yaml.Parser())
	if err != nil {
		return err
	}

	bytes = append(bytes, byte('\n'))
	_, err = tx.Write(bytes)

	return err
}

// Enchant extracts configuration for a specific type T from the
// loaded configuration files.
//
// It uses the caller's directory path to determine the
// configuration path within the YAML structure.
//
// The function automatically determines the configuration path
// based on the caller's file location relative to project root.
// For example, if called from "service/greetsvc/config.go", it
// will look for configuration under the "service/greetsvc" path
// in the YAML files.
//
// The configuration is unmarshaled into the provided type T
// using YAML tags.
//
// Example:
//
//	type MyConfig struct {
//	    Port int `yaml:"port"`
//	    Host string `yaml:"host"`
//	}
//
//	config := mizudi.Enchant[MyConfig](nil)
func Enchant[T any](defaultConfig *T, opts ...Option) *T {
	_, runtimePath, _, ok := runtime.Caller(1)
	if !ok {
		panic("failed to get runtime caller")
	}

	if defaultConfig == nil {
		defaultConfig = new(T)
	}
	unmarshalConf := koanf.UnmarshalConf{Tag: "yaml"}

	config := &config{substituteMap: make(map[string]string)}
	for _, opt := range opts {
		opt(config)
	}

	unmarshalPath := strings.TrimPrefix(path.Dir(runtimePath), _ROOT)
	unmarshalPath = strings.TrimPrefix(unmarshalPath, _PATH_SEPARATOR)

	{ // Apply trim prefix and substitution
		blocks := strings.Split(unmarshalPath, _PATH_SEPARATOR)
		for from, to := range config.substituteMap {
			matchBlocks := strings.Split(from, _PATH_SEPARATOR)
			if len(matchBlocks) > len(blocks) {
				continue
			}
			if slices.Equal(matchBlocks, blocks[:len(matchBlocks)]) {
				subedBlocks := append(strings.Split(to, _PATH_SEPARATOR), blocks[len(matchBlocks):]...)
				unmarshalPath = strings.Join(subedBlocks, _PATH_SEPARATOR)

				goto load
			}
		}
	}

load:
	if err := _KOANF.UnmarshalWithConf(unmarshalPath, defaultConfig, unmarshalConf); err != nil {
		panic(err)
	}
	return defaultConfig
}

// Retrieve is a handy wrapper around samber/do/v2's Invoke
// function.
//
// Example:
//
//	// Register a service first
//	mizudi.Register(func() (*DatabaseService, error) {
//	    return NewDatabaseService(), nil
//	})
//
//	// Retrieve the service
//	db, err := mizudi.Retrieve[*DatabaseService]()
//	if err != nil {
//	    log.Fatal("Failed to get database service:", err)
//	}
func Retrieve[T any]() (T, error) {
	return do.Invoke[T](_INJECTOR)
}

// MustRetrieve is a handy wrapper around samber/do/v2's
// MustInvoke function.
func MustRetrieve[T any]() T {
	return do.MustInvoke[T](_INJECTOR)
}

// RetrieveNamed is a handy wrapper around samber/do/v2's
// InvokeNamed function,
//
// RetrieveNamed allows:
//  1. Retrieve multiple same type with different names.
//  2. Retrieve multiple different type with the same name.
func RetrieveNamed[T any](name string) (T, error) {
	typedName := fmt.Sprintf("%s:%T", name, new(T))
	return do.InvokeNamed[T](_INJECTOR, typedName)
}

// MustRetrieveNamed is a handy wrapper around samber/do/v2's
// MustInvokeNamed function
func MustRetrieveNamed[T any](name string) T {
	typedName := fmt.Sprintf("%s:%T", name, new(T))
	return do.MustInvokeNamed[T](_INJECTOR, typedName)
}

// Register is a handy wrapper around samber/do/v2's Provide
// function.
func Register[T any](fn func() (T, error)) {
	do.Provide(_INJECTOR, func(i do.Injector) (T, error) { return fn() })
}

// RegisterNamed is a handy wrapper around samber/do/v2's
// ProvideNamed function.
//
// RegisterNamed allows:
//  1. Register multiple same type with different names.
//  2. Register multiple different type with the same name.
func RegisterNamed[T any](name string, fn func() (T, error)) {
	typedName := fmt.Sprintf("%s:%T", name, new(T))
	do.ProvideNamed(_INJECTOR, typedName, func(i do.Injector) (T, error) { return fn() })
}
