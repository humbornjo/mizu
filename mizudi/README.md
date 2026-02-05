# mizudi - Configuration Management & Dependency Injection

Built with [samber/do](https://github.com/samber/do) for dependency injection and [knadh/koanf](https://github.com/knadh/koanf) for configuration management.

A powerful Go framework that combines automatic configuration loading with dependency injection, designed to simplify service-oriented architecture.

## Installation

```bash
go get github.com/humbornjo/mizu/mizudi
```

## Philosophy

Configuration should be:

1. **Convention-based**: Configuration paths are automatically determined by your code hierarchy.
2. **Environment-aware**: Seamlessly merges YAML configurations with environment variables.
3. **Scope-constrained**: A config for a individual service is only accessible by that service. And a global config
   should be easily accessible by all services.

## Why This Design is Good

### Avoids Circular Dependencies & Monolithic Config

Traditional approaches create problematic dependencies and unmaintainable config structures:

```go
package config

// Single massive struct containing ALL service configs accessed
// by a singleton method.
type GlobalConfig struct {
    ServiceA ServiceAConfig  // ServiceA's private config exposed
    ServiceB ServiceBConfig  // ServiceB's private config exposed

    Shared   SharedConfig    // Public/shared config mixed in
}
```

Say service A want to use shared config, it has to load the entire GloalConfig manually. Which results in that service A
"see" the config of service B, which is not what we want.

When it comes to ergonomic config management, it's better to have each service defines its own config struct. However,
if the config is imported by GloalConfig, then the service initialization must happened outside the service package,
which is counter-intuitive (otherwise introduce circular dependency).

### Quickstart

```plain
  +--------------+  imported  +----------------+
  |   services   |<-----------| config package |
  +--------------+            +----------------+
          |                           |
          | imported                  | imported
          |                           |
          v                           |
  +--------------+                    |
  |    main.go   |<-------------------+
  +--------------+
```

The config extraction in each service rely on the initialization of the config package (load from YAML). So the
dependency should be explicitly declared in each package where `mizudi.Enchant` is called. Refer to [examples](https://github.com/humbornjo/mizu/tree/main/_example) for detailed code structure instructions (which use `config.Config` as a placeholder input argument to maintain the dependency hierarchy).

```go
// config/config.go
package config

func init() {
    // "config" is the dir in "config/config.go", which is the
    // relative path from go.mod.
    //
    // If your path is "config", but your package name is "whatever",
    // then you should call `mizudi.Initialize("config")`
    mizudi.Initialize("config")
}

// service/greetsvc/config.go
type ConfigGreet struct {
    Port     int      `yaml:"port"`
    Database DBConfig `yaml:"database"`
}

func Initialize(_ *config.Config) {
    cfg := mizudi.Enchant[ConfigGreet](nil)

    // Do something with cfg
}
```

> - **`mizudi.Enchant` should be called exactly in the packge where each config is defined.**
> - **`mizudi.Initialize` should be called exactly in the packge where the global config is defined.**
>
> All the configuration loading depends on the relative_path passed to `mizudi.Initialize`. After config has been
> enchanted, you can directly use it or perform dependency injection to access it universally.

Each service knows only about its own configuration:

```
service/
├── greetsvc/     → loads from "service.greetsvc.*"   config path
├── namastesvc/   → loads from "service.namastesvc.*" config path
└── filesvc/      → loads from "service.filesvc.*"    config path
```

### Flag style Configuration Loading

It is common pattern to pass configuration path via command line flags:

```bash
./app --config /some/path/config.yaml
```

This pattern can also be applied easily in `mizudi`:

- Create `func Initialize(paths ...string)` in your global config package
- Invoke it in the main function (or the according cmd file). Just keep in mind that `Initialize` should always be called before any `Enchant`

Referring to [examples](https://github.com/humbornjo/mizu/tree/main/_example/config/config.go) for detailed example.

## Core Features

### Auto-location Configuration Loading

```go
func init() {
    // Called from service/greetsvc/config.go
    config := mizudi.Enchant[Config](nil)
    // Automatically loads from the "service/greetsvc" path
    // → maps to YAML: "service.greetsvc.*"
}
```

### Environment-Driven Configuration

Seamless integration with environment variables:

```bash
# YAML config
service:
  greetsvc:
    port: 8080

# Environment override
export MIZU_SERVICE_GREETSVC_PORT=9090
```

### Path Substitution for Flexible Structure

```go
config := mizudi.Enchant[Config](nil,
    mizudi.WithSubstitutePrefix("service/greetsvc/internal", "service/greet"))
// Loads from yaml path "service.greet.*" even when called from go project dir "service/greetsvc/internal"
```
