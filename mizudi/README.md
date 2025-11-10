# mizudi - Configuration Management & Dependency Injection

Built with [samber/do](https://github.com/samber/do) for dependency injection and [knadh/koanf](https://github.com/knadh/koanf) for configuration management.

A powerful Go framework that combines automatic configuration loading with dependency injection, designed to simplify service-oriented architecture.

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

Each service knows only about its own configuration:

```
service/
├── greetsvc/     → loads from "service.greetsvc.*"   config path
├── namastesvc/   → loads from "service.namastesvc.*" config path
└── filesvc/      → loads from "service.filesvc.*"    config path
```

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
