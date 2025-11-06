# 📦 mizudi - Configuration Management & Dependency Injection

Built with [samber/do](https://github.com/samber/do) for dependency injection and [knadh/koanf](https://github.com/knadh/koanf) for configuration management.

A powerful Go framework that combines automatic configuration loading with dependency injection, designed to simplify service-oriented architecture.

## Philosophy

### Configuration Management Philosophy

mizudi treats configuration as **location-aware, type-safe, and environment-driven** data that should be:

1. **Convention-based**: Configuration paths are automatically determined by your code hierarchy.
2. **Type-safe**: Strong typing ensures compile-time validation of configuration access
3. **Environment-aware**: Seamlessly merges YAML configurations with environment variables
4. **Service-oriented**: Each service automatically loads its relevant configuration subset

### Dependency Injection Philosophy

mizudi embraces **explicit dependency registration** with **implicit dependency resolution**:

1. **Explicit Registration**: Only one registry (do.Injector) exist globally
2. **Service-oriented**: Designed for microservices with clear service boundaries

## Why This Design is Good

### Avoids Circular Dependencies & Monolithic Config

Traditional approaches create problematic dependencies and unmaintainable config structures:

```go
// UGLY: Traditional monolithic config approach
package config

type GlobalConfig struct {
    // 🤢 Single massive struct containing ALL service configs
    ServiceA ServiceAConfig  // ServiceA's private config exposed
    ServiceB ServiceBConfig  // ServiceB's private config exposed
    Shared   SharedConfig    // Public/shared config mixed in
}

var GlobalCfg *GlobalConfig  // Global singleton everyone imports

// Result: Every service imports the entire config = massive coupling
// Services can access configs they shouldn't see
```

```go
// NIGHTMARE: Traditional circular dependency pattern
package serviceA

type ServiceAConfig struct {
  ...
}
// CIRCULAR DEPENDENCY: If ServiceA need global config, then It
// has to load its own config individually, otherwise (introduce
// ServiceAConfig in package config) will cause circular dependency.
```

With mizudi, each service has **independent boundaries** and **clean separation**:

- **config pakcage**: Load global config
- **services**: Load their own config, import config package and use global config
- **main.go**: Import services and config package

```txt
  +--------------+  imported  +----------------+
  |   services   |<-----------| config package |
  +--------------+            +----------------+
          |                           |
          |                           |
          |                           |
          |                           |
          |                           |
          | imported                  | imported
          |                           |
          |                           |
          |                           |
          |                           |
          v                           |
  +--------------+                    |
  |    main.go   |<-------------------+
  +--------------+
```

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
    Port     int    `yaml:"port"`
    Database DBConfig `yaml:"database"`
}

func Initialize() {
    cfg := mizudi.Enchant[ConfigGreet](nil)

    // Do something with cfg
}
```

**Benefits:**

- **No circular dependencies**: Services only access their own configs via DI
- **Private by default**: Service configs are invisible to other services
- **Clean separation**: Each service loads only its relevant config subset
- **Independent evolution**: Change one service config = no recompile of others
- **Location-aware**: Auto-loading based on file location

### Clear Service Boundaries

Each service knows only about its own configuration:

```
service/
├── greetsvc/     → loads from "service.greet.*" config path
├── namastesvc/   → loads from "service.namaste.*" config path
└── filesvc/      → loads from "service.file.*" config path
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
# Automatically overrides YAML value
```

### Path Substitution for Flexible Structure

```go
config := mizudi.Enchant[Config](nil,
    mizudi.WithSubstitutePrefix("service/greetsvc/internal", "service/greet"))
// Loads from "service.greet.*" even when called from "service/greetsvc/internal"
```
