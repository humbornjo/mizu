# <div align="center"><img alt="mizu" src="https://cdn.rawgit.com/humbornjo/mizu/main/_example/mizu.jpg" width="600"/></div>

# 🌊 Mizu - HTTP Framework for Go

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![CI Status](https://github.com/humbornjo/mizu/workflows/CI/badge.svg)](https://github.com/humbornjo/mizu/actions)
![Alpha](https://img.shields.io/badge/status-alpha-orange.svg)

> **Mizu** (水) - Japanese for "water", also the name of main character in the anime [Blue Eye Samurai](https://www.imdb.com/title/tt13309742/) - An HTTP framework built on Go's standard library.

Mizu provides middleware composition, lifecycle hooks, and observability features while staying close to Go's native `net/http`.

> ⚠️ **Alpha Status**: Mizu is currently in alpha development. APIs may change and the framework is not recommended for production use.

## ✨ Features

- **🚀 Native Performance** - Built on Go's `net/http`
- **🔧 Middleware** - Composable middleware system with scoping
- **📊 Observability** - OpenTelemetry, Prometheus metrics, and structured logging
- **🔌 Connect RPC** - Support for protobuffer development
- **⚡ Graceful Shutdown** - Configurable timeouts and drain periods

## 📦 Installation

```bash
go get github.com/humbornjo/mizu
```

## 🚀 Quick Start

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/humbornjo/mizu"
)

func MiddlewareLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func MiddlewareAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add authentication logic here
		w.Header().Set("X-Auth", "validated")
		next.ServeHTTP(w, r)
	})
}

func main() {
	// Create a new Mizu server
	server := mizu.NewServer("my-api")

	// Apply logging middleware to all routes
	server.Use(MiddlewareLog)

	// Add some routes
	server.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello, Mizu! 🌊"))
	})

	server.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("id")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("User ID: " + userID))
	})

	// Add authentication middleware to only a specific route
	server.Use(MiddlewareAuth).
		Post("/users", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("User created"))
		})

	// Start server with graceful shutdown
	server.ServeContext(context.Background(), ":8080")
}
```

Run the server:

```bash
go run main.go
```

Test the endpoints:

```bash
curl http://localhost:8080/                # Hello, Mizu! 🌊
curl http://localhost:8080/users/123       # User ID: 123
curl -X POST http://localhost:8080/users   # User created (with auth header)
curl http://localhost:8080/healthz         # OK (built-in health check)
```

## 🗺️ Roadmap to Beta

- [ ] Complete documentation for each sub-module
- [ ] Add common HTTP middleware support like CORS
- [ ] Using mizuoai to compare performance with popular OpenAPI Go frameworks like Fuego

## 🔧 Configuration Options

### Server Options

| Option                                        | Description                                                                                  | Default     |
| --------------------------------------------- | -------------------------------------------------------------------------------------------- | ----------- |
| `WithReadinessDrainDelay(duration)`           | Graceful shutdown delay for load balancer propagation                                        | `5s`        |
| `WithShutdownPeriod(duration)`                | Timeout for graceful shutdown                                                                | `15s`       |
| `WithHardShutdownPeriod(duration)`            | Hard shutdown timeout after graceful fails                                                   | `3s`        |
| `WithCustomHttpServer(*http.Server)`          | Use custom HTTP server configuration                                                         | `nil`       |
| `WithWizardHandleReadiness(pattern, handler)` | Custom health check endpoint and handler                                                     | `/healthz`  |
| `WithPrometheusMetrics()`                     | Enable Prometheus metrics endpoint                                                           | Disabled    |
| `WithProfilingHandlers()`                     | Enable pprof debugging endpoints                                                             | Disabled    |
| `WithRevealRoutesOnStartup()`                 | Log registered routes on startup                                                             | Disabled    |
| `WithServerProtocols(protocols)`              | Configure HTTP protocol support, see [example](./_example) for the RPC case that uses HTTP/2 | HTTP/1 only |

### HTTP Server Timeouts

Mizu configures timeouts by default:

```go
ReadHeaderTimeout: 15 * time.Second  // Prevent Slowloris attacks
ReadTimeout:       60 * time.Second  // Total request read time
WriteTimeout:      60 * time.Second  // Response write time
IdleTimeout:       300 * time.Second // Keep-alive timeout
```

## 📦 Sub-packages

For basic usage of sub-packages, see [example](https://github.com/humbornjo/mizu/tree/main/_example).

### 🔍 mizuotel - OpenTelemetry Integration

Simplified OpenTelemetry setup for distributed tracing and metrics.

**Key Features:**

- Provider initialization with sane defaults
- Configurable service metadata (name, version, environment)
- Custom resource and attribute injection
- Support for custom tracer/meter providers
- Automatic propagation setup

**Configuration Options:**

| Option                         | Description                       | Default          |
| ------------------------------ | --------------------------------- | ---------------- |
| `WithServiceName(name)`        | Set service name for telemetry    | `"mizu-service"` |
| `WithServiceVersion(version)`  | Set service version               | `"1.0.0"`        |
| `WithEnvironment(env)`         | Set deployment environment        | `"development"`  |
| `WithAttributes(attrs...)`     | Add custom resource attributes    | `nil`            |
| `WithResource(resource)`       | Use custom OpenTelemetry resource | -                |
| `WithTracerProvider(provider)` | Use custom tracer provider        | -                |
| `WithMeterProvider(provider)`  | Use custom meter provider         | -                |

### 📝 mizulog - Structured Logging

Enhanced structured logging with context-aware attribute injection.

**Key Features:**

- Wraps Go's standard `log/slog` package
- Context-aware attribute injection
- Configurable log levels
- Seamless integration with existing slog code

**Configuration Options:**

| Option                  | Description                               | Default          |
| ----------------------- | ----------------------------------------- | ---------------- |
| `WithLogLevel(level)`   | Set minimum log level for filtering       | `slog.LevelInfo` |
| `WithAttributes(attrs)` | Add default attributes to all log records | `nil`            |

### 🔌 mizuconnect - Connect RPC Integration

Streamlined integration with Connect protocol for type-safe RPC services.

**Key Features:**

- Automatic service registration
- Built-in health checks via grpchealth
- gRPC reflection support
- Vanguard transcoding for REST compatibility
- Handler option composition

**Configuration Options:**

| Option                                      | Description                           | Default  |
| ------------------------------------------- | ------------------------------------- | -------- |
| `WithHealth()`                              | Enable gRPC health check              | Disabled |
| `WithValidate()`                            | Enable buf proto validation           | Disabled |
| `WithReflect(opts)`                         | Enable gRPC reflection                | Disabled |
| `WithVanguard(pattern, svcOpts, transOpts)` | Enable REST transcoding with Vanguard | Disabled |
| `WithHandlerOptions(opts...)`               | Add Connect-specific handler options  | `nil`    |

## 🏗️ Development

### Prerequisites

Go 1.24+

### Git Hooks

For contributors, it's recommended to install the pre-commit hooks:

```bash
# Install pre-commit hooks that run formatting and linting
make install-hooks
```

This installs a pre-commit hook that automatically runs `make format` and `make lint` before each commit, ensuring code quality and consistency.

## 🙏 References

- [Twine Framework](http://127.0.0.1:5755)
- [Graceful Shutdown in Go: Practical Patterns](https://victoriametrics.com/blog/go-graceful-shutdown/)
- [Claude Code (mizuotel & unittest & CI & docs)](https://www.claudecode.io)

---

<div align="center">

**🌊 Mizu - Flow naturally with Go**

</div>
