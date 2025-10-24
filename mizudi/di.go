// Package mizudi provides dependency injection and configuration
// management for Go applications with automatic service
// discovery and configuration loading.
//
// The package offers two main functionalities:
//  1. Configuration management through YAML files and
//     environment variables
//  2. Dependency injection using the samber/do library
//
// Configuration files are loaded from paths specified in options
// or default to "local.yaml".
// Environment variables with prefix "MIZU_" are automatically
// loaded and converted to config paths.
package mizudi

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

const _PATH_SEPARATOR = string(os.PathSeparator)

var (
	_K        *koanf.Koanf
	_INJECTOR = do.New()

	_ROOT        string
	_PATHS       []string
	_SUBS_MAP    = make(map[string]string)
	_TRIM_TARGET = make([]string, 0, 1)
)

// Option represents a configuration option for mizudi package.
// Options can be used to customize the behavior of Init.
type Option func()

func WithLoadPath(path string) Option {
	return func() {
		_PATHS = append(_PATHS, path)
	}
}

func WithSubstitutePrefix(from string, to string) Option {
	return func() {
		_SUBS_MAP[from] = to
	}
}

func WithTrimPrefix(prefix string) Option {
	return func() {
		_TRIM_TARGET = append(_TRIM_TARGET, prefix)
	}
}

// WARN: TL;DR: use `go build -trimpath` and `go run -trimpath`.
// The -trimpath flag is critical for mizudi's configuration path
// resolution mechanism. Without -trimpath, runtime.Caller(1)
// returns full absolute file paths that include the build
// machine's directory structure
// (e.g., "/app/src/project/service/config.go"). This makes the
// configuration path extraction unreliable across different
// development environments and deployment scenarios.
//
// Init initializes the mizudi package with the provided options.
// It sets up the configuration system by loading YAML files and
// environment variables.
//
// The function automatically determines the runtime root
// directory and loads configuration
// from the specified paths (or defaults to "local.yaml" in the
// current working directory).
//
// Environment variables with prefix "MIZU_" are automatically
// loaded and mapped to configuration paths (e.g., MIZU_DB_HOST
// becomes db.host).
func Init(opts ...Option) {
	// Extract compiling root directory
	_, runtimePath, _, ok := runtime.Caller(1)
	if !ok {
		panic("failed to get runtime caller")
	}
	_ROOT = strings.Split(runtimePath, _PATH_SEPARATOR)[0]

	// Load config
	k, parser := koanf.New("/"), yaml.Parser()
	_K = k
	for _, opt := range opts {
		opt()
	}
	if len(_PATHS) == 0 {
		wd, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		_PATHS = []string{path.Join(wd, "local.yaml")}
	}
	for _, path := range _PATHS {
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

// Enchant extracts configuration for a specific type T from the
// loaded configuration files.
//
// It uses the caller's directory path to determine the
// configuration path within the YAML structure.
//
// The function automatically determines the configuration path
// based on the caller's file location relative to the project
// root. For example, if called from "service/greetsvc/config.go",
// it will look for configuration under the "service/greetsvc"
// path in the YAML files.
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
//	config := mizudi.Enchant[MyConfig]()
func Enchant[T any](defaultConfig *T) *T {
	_, runtimePath, _, ok := runtime.Caller(1)
	if !ok {
		panic("failed to get runtime caller")
	}

	if defaultConfig == nil {
		defaultConfig = new(T)
	}
	unmarshalConf := koanf.UnmarshalConf{Tag: "yaml"}

	unmarshalPath := strings.TrimPrefix(path.Dir(runtimePath), _ROOT)
	qualifiedPath := strings.TrimPrefix(unmarshalPath, _PATH_SEPARATOR)

	if err := _K.UnmarshalWithConf(qualifiedPath, defaultConfig, unmarshalConf); err != nil {
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
