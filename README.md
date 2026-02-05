# <div align="center"><img alt="mizu" src="https://cdn.rawgit.com/humbornjo/mizu/main/_example/mizu.jpg" width="600"/></div>

# 🌊 Mizu - HTTP Framework for Go

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![CI Status](https://github.com/humbornjo/mizu/workflows/CI/badge.svg)](https://github.com/humbornjo/mizu/actions)
![Alpha](https://img.shields.io/badge/status-alpha-orange.svg)

> **Mizu** (水) - Japanese for "water", also the name of main character in the anime [Blue Eye Samurai](https://www.imdb.com/title/tt13309742/) - An HTTP framework built on Go's standard library.

Mizu provides middleware composition, lifecycle hooks, and observability features while staying close to Go's native `net/http`.

> ⚠️ **Alpha Status**: Mizu is currently in alpha development. APIs may change and the framework is not recommended for production use. Go 1.26 will support `new` with init value, `mizu` will catch up when the release is published.

## Features

- **Native Performance** - Built on Go's `net/http`
- **Middleware** - Composable middleware system with scoping
- **Graceful Shutdown** - Configurable timeouts and drain periods for cloud native deployments
- **Modular Design** - Each feature lives in its own independent module for minimal dependencies and small compiling output

## Installation

```bash
go get github.com/humbornjo/mizu
```

## Quick Start

For a more comprehensive example, please refer to [examples](./_example/).

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

## Roadmap to Beta

- [x] Complete documentation for each sub-module
- [x] Add commonly used HTTP middleware implementations
- [x] Compare mizuoai with popular OpenAPI Go frameworks like Fuego on performance
- [ ] ~~Fix Connect-RPC download issue in `mizuconnect/restful/filekit`~~

## Configuration Options

### Server Options

| Option                                        | Description                                                                                  | Default     |
| --------------------------------------------- | -------------------------------------------------------------------------------------------- | ----------- |
| `WithReadinessDrainDelay(duration)`           | Graceful shutdown delay for load balancer propagation                                        | `5s`        |
| `WithShutdownPeriod(duration)`                | Timeout for graceful shutdown                                                                | `15s`       |
| `WithHardShutdownPeriod(duration)`            | Hard shutdown timeout after graceful fails                                                   | `3s`        |
| `WithCustomMux(Mux)`                          | Use custom mux to register routes (e.g. `github.com/go-chi/chi/v5/mux.go`)                   | `nil`       |
| `WithCustomHttpServer(*http.Server)`          | Use custom HTTP server configuration                                                         | `nil`       |
| `WithWizardHandleReadiness(pattern, handler)` | Custom health check endpoint and handler                                                     | `/healthz`  |
| `WithProfilingHandlers()`                     | Enable pprof debugging endpoints                                                             | Disabled    |
| `WithRevealRoutes()`                          | Log registered routes on startup                                                             | Disabled    |
| `WithServerProtocols(protocols)`              | Configure HTTP protocol support, see [example](./_example) for the RPC case that uses HTTP/2 | HTTP/1 only |

### HTTP Server Timeouts

Mizu configures timeouts by default:

```go
ReadHeaderTimeout: 15 * time.Second  // Prevent Slowloris attacks
ReadTimeout:       60 * time.Second  // Total request read time
WriteTimeout:      60 * time.Second  // Response write time
IdleTimeout:       300 * time.Second // Keep-alive timeout
```

## Modular Architecture

Mizu is now organized as a collection of independent modules, each with their own repository and documentation:

- **[mizudi](./mizudi/)** - Dependency injection utilities
- **[mizumw](./mizumw/)** - Common HTTP middleware implementations
- **[mizuoai](./mizuoai/)** - OpenAPI specification integration
- **[mizulog](./mizulog/)** - Structured logging with context-aware attributes
- **[mizuotel](./mizuotel/)** - OpenTelemetry integration for distributed tracing and metrics
- **[mizuconnect](./mizuconnect/)** - Connect-RPC integration for type-safe RPC services

Each module is self-contained with its own `go.mod` file and can be used independently. Visit each directory for specific documentation and usage examples.

## Development

### Prerequisites

Go 1.25+

```bash
# package `mizuoai` requires `encoding/json/v2` support
go env -w GOEXPERIMENT=jsonv2
```

## References

- Twine Framework
  - Twine framework is a prototype which is not published, the IDEA behind it is the `Register` function in `mizuconnect`,
    using reflection to dynamically register ConnectRPC services. What I did extra in `mizuconnect` is more fine-grained
    type check on the input and output parameters, scope management, cache intercepter and restful toolkits.
- [Graceful Shutdown in Go: Practical Patterns](https://victoriametrics.com/blog/go-graceful-shutdown/)
- [Larking](https://github.com/emcfarlane/larking)
- [Claude Code](https://www.claudecode.io)

## AI Contribution Disclaimer

All the contents in this repository not mentioned below has nothing to do with AI.

- Code comments accepts completion suggestion from Windsurf
- Unittest is generated by by Kimi-K2 with Claude Code and slightly adjusted manually
- Package `mizuotel` is generated by Kimi-K2 with Claude Code and slightly adjusted manually
- Documentation and Makefile are generated by by Kimi-K2 with Claude Code and slightly adjusted manually
