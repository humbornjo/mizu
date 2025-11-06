# 🛡️ mizumw - HTTP Middleware Collection

**This module steal a lot of code from [chi](https://github.com/go-chi/chi)**

Common HTTP middleware implementations for Mizu, providing essential web server functionality.

## Features

- **Compression** - Response compression with gzip and deflate
- **Recovery** - Panic recovery with proper logging

## Installation

```bash
go get github.com/humbornjo/mizu/mizumw
```

## Quick Start

### Compression Middleware

Response compression middleware that automatically compresses responses based on client `Accept-Encoding` headers.

```go
package main

import (
    "context"
    "github.com/humbornjo/mizu"
    "github.com/humbornjo/mizu/mizumw/compressmw"
)

func main() {
    server := mizu.NewServer("web-app")

    // Add compression middleware to all routes
    server.Use(compressmw.New())

    server.Get("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Large response that will be compressed"))
    })

    server.ServeContext(context.Background(), ":8080")
}
```

### Recovery Middleware

Panic recovery middleware that catches panics, logs detailed stack traces, and responds with appropriate HTTP error codes.

```go
import "github.com/humbornjo/mizu/mizumw/recovermw"

func main() {
    server := mizu.NewServer("api-service")

    // Add recovery middleware at the beginning
    server.Use(recovermw.New())

    server.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
        panic("Something went wrong!")
    })

    server.ServeContext(context.Background(), ":8080")
}
```
