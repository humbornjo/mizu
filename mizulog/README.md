# 📝 mizulog - Structured Logging for Go

Enhanced structured logging with context-aware attribute injection built on Go's standard `log/slog` package.

## ✨ Features

- **Context-Aware** - Automatic attribute injection from context
- **Configurable Levels** - Set minimum log levels for filtering
- **Seamless Integration** - Works with existing slog code

## 📦 Installation

```bash
go get github.com/humbornjo/mizu/mizulog
```

## 🚀 Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "log/slog"
    "github.com/humbornjo/mizu/mizulog"
)

func main() {
    // Initialize mizulog with default settings
    mizulog.Initialize(nil)

    // Use standard slog functions
    slog.Info("Application started")
    slog.Error("Something went wrong", slog.String("error", "timeout"))
}
```

### Context-Aware Logging

```go
func handleRequest(ctx context.Context, requestID string) {
    // Inject attributes into context
    ctx = mizulog.InjectContextAttrs(ctx,
        slog.String("request_id", requestID),
        slog.String("user_id", "123"),
    )

    // Attributes are automatically included in logs
    slog.InfoContext(ctx, "Processing request") // includes request_id and user_id
}
```

### Custom Configuration

```go
import "log/slog"

// Configure with options
mizulog.Initialize(nil,
    mizulog.WithLogLevel("debug"),
    mizulog.WithAttributes([]slog.Attr{
        slog.String("service", "my-api"),
        slog.String("version", "1.0.0"),
    }),
)
```

## 🔧 Configuration Options

### Initialize Options

| Option                  | Description                                 | Default          |
| ----------------------- | ------------------------------------------- | ---------------- |
| `WithLogLevel(level)`   | Minimum log level (string or int)           | `slog.LevelInfo` |
| `WithAttributes(attrs)` | Default attributes added to all log records | `nil`            |

## 🎯 Key Functions

### Initialize

Set up mizulog as the default slog handler:

```go
func Initialize(h slog.Handler, opts ...Option)
```

### New

Create a new mizulog handler:

```go
func New(h slog.Handler, opts ...Option) slog.Handler
```

### InjectContextAttrs

Add attributes to context for automatic inclusion:

```go
func InjectContextAttrs(ctx context.Context, attrs ...slog.Attr) context.Context
```

### Context-Aware Logging

Use context-aware slog functions for automatic attribute inclusion:

```go
slog.InfoContext(ctx, "message")   // Includes context attributes
slog.ErrorContext(ctx, "error")    // Includes context attributes
slog.DebugContext(ctx, "debug")    // Includes context attributes
slog.WarnContext(ctx, "warning")   // Includes context attributes
```

## 💡 Examples

### HTTP Server Integration

```go
func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Extract user info from request
        userID := extractUserID(r)

        // Add to context for all subsequent logs
        ctx := mizulog.InjectContextAttrs(r.Context(),
            slog.String("user_id", userID),
            slog.String("path", r.URL.Path),
        )

        // Create new request with enhanced context
        r = r.WithContext(ctx)
        next.ServeHTTP(w, r)
    })
}

func handleUser(w http.ResponseWriter, r *http.Request) {
    // Automatically includes user_id and path from context
    slog.InfoContext(r.Context(), "User request processed")
}
```

### Error Tracking

```go
func processOrder(ctx context.Context, orderID string) error {
    ctx = mizulog.InjectContextAttrs(ctx,
        slog.String("order_id", orderID),
    )

    order, err := getOrder(orderID)
    if err != nil {
        slog.ErrorContext(ctx, "Failed to get order")
        return err
    }

    slog.InfoContext(ctx, "Order processed successfully")
    return nil
}
```

## 🌊 Integration with Mizu

mizulog is designed to work seamlessly with the main Mizu HTTP framework:

```go
import (
    "github.com/humbornjo/mizu"
    "github.com/humbornjo/mizu/mizulog"
)

func main() {
    // Set up mizulog
    mizulog.Initialize(nil, mizulog.WithLogLevel("info"))

    // Create Mizu server
    server := mizu.NewServer("my-service")

    // Your middleware can use context-aware logging
    server.Use(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx := mizulog.InjectContextAttrs(r.Context(),
                slog.String("method", r.Method),
                slog.String("path", r.URL.Path),
            )
            slog.InfoContext(ctx, "Request received")
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    })
}
```
