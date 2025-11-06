# 🔍 mizuotel - OpenTelemetry Integration

Simplified OpenTelemetry setup for distributed tracing and metrics in Mizu applications.

## ✨ Features

- **Easy Setup** - Simple initialization with sensible defaults
- **Configurable** - Customizable service metadata and attributes
- **Comprehensive** - Both tracing and metrics support
- **Integration Ready** - Seamless Mizu framework integration
- **Context Propagation** - Automatic trace context propagation

## 📦 Installation

```bash
go get github.com/humbornjo/mizu/mizuotel
```

## 🚀 Quick Start

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

### Custom Configuration

```go
import (
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

func main() {
    // Initialize with custom configuration
    err := mizuotel.Initialize(
        mizuotel.WithServiceName("api-service"),
        mizuotel.WithServiceVersion("2.1.0"),
        mizuotel.WithEnvironment("production"),
        mizuotel.WithAttributes(
            attribute.String("team", "platform"),
            attribute.String("region", "us-east-1"),
        ),
    )
    if err != nil {
        panic(err)
    }

    // Create custom meter for metrics
    meter := otel.Meter("api.meter")
    counter, _ := meter.Int64Counter("requests.total")

    server.Use(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Count requests
            counter.Add(r.Context(), 1, metric.WithAttributes(
                attribute.String("path", r.URL.Path),
                attribute.String("method", r.Method),
            ))
            next.ServeHTTP(w, r)
        })
    })
}
```

## 🔧 Configuration Options

### OpenTelemetry Options

| Option                         | Description                         | Default          |
| ------------------------------ | ----------------------------------- | ---------------- |
| `WithServiceName(name)`        | Set service name for telemetry data | `"mizu-service"` |
| `WithServiceVersion(version)`  | Set service version                 | `"1.0.0"`        |
| `WithEnvironment(env)`         | Set deployment environment          | `"development"`  |
| `WithAttributes(attrs...)`     | Add custom resource attributes      | `none`           |
| `WithResource(resource)`       | Use custom OpenTelemetry resource   | Auto-generated   |
| `WithTracerProvider(provider)` | Use custom tracer provider          | Default provider |
| `WithMeterProvider(provider)`  | Use custom meter provider           | Default provider |
