# mizuotel - OpenTelemetry Integration

Simplified OpenTelemetry setup for distributed tracing and metrics.

## Installation

```bash
go get github.com/humbornjo/mizu/mizuotel
```

## Quick Start

### Basic Initialization

```go
package main

import (
    "context"
    "github.com/humbornjo/mizu"
    "github.com/humbornjo/mizu/mizuotel"
    "go.opentelemetry.io/otel"
)

func main() {
    // Initialize OpenTelemetry with defaults
    err := mizuotel.Initialize()
    if err != nil {
        panic(err)
    }
    defer mizuotel.Shutdown(context.Background())

    // Create tracer and use it
    tracer := otel.Tracer("my.service")

    // Use with Mizu
    server := mizu.NewServer("example-service")

    server.Get("/", func(w http.ResponseWriter, r *http.Request) {
        ctx, span := tracer.Start(r.Context(), "handler.request")
        defer span.End()

        // Your business logic here
        w.Write([]byte("Hello with tracing!"))
    })

    server.ServeContext(context.Background(), ":8080")
}
```
