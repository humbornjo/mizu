# 📝 mizulog - Structured Logging for Go

Enhanced structured logging with context-aware attribute injection built on Go's standard `log/slog` package.

## Features

- **Context-Aware** - Automatic attribute injection from context
- **Configurable Levels** - Set minimum log levels for filtering
- **Seamless Integration** - Works with existing slog code

## Installation

```bash
go get github.com/humbornjo/mizu/mizulog
```

## Quick Start

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

## Configuration Options

### Initialize Options

| Option                  | Description                                 | Default          |
| ----------------------- | ------------------------------------------- | ---------------- |
| `WithLogLevel(level)`   | Minimum log level (string or int)           | `slog.LevelInfo` |
| `WithAttributes(attrs)` | Default attributes added to all log records | `nil`            |

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
