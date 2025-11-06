# 🛡️ mizumw - HTTP Middleware Collection

**This module steal a lot of code from [chi](https://github.com/go-chi/chi)**

Common HTTP middleware implementations for Mizu, providing essential web server functionality.

## ✨ Features

- **Compression** - Response compression with gzip and deflate
- **Recovery** - Panic recovery with proper logging

## 📦 Installation

```bash
go get github.com/humbornjo/mizu/mizumw
```

## 🚀 Quick Start

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

## 🔧 Middleware Options

### Compression Middleware (`compressmw`)

Compression middleware supports both gzip and deflate compression with automatic content-type filtering.

| Option                       | Description                                    | Default          |
| ---------------------------- | ---------------------------------------------- | ---------------- |
| `WithContentTypes(types...)` | Override allowed content types for compression | Default types    |
| `WithOverrideGzip(enc)`      | Custom gzip encoder configuration              | Standard gzip    |
| `WithOverrideDeflate(enc)`   | Custom deflate encoder configuration           | Standard deflate |

#### Compression Usage Examples

**Basic Compression:**

```go
server.Use(compressmw.New())
```

**Custom Content Types:**

```go
server.Use(compressmw.New(
    compressmw.WithContentTypes("application/json", "text/css", "text/html"),
))
```

**Custom Gzip Configuration:**

```go
server.Use(compressmw.New(
    compressmw.WithOverrideGzip(&compressmw.EncoderGzip{
        Level: compressmw.GZIP_COMPRESSION_LEVEL_BEST,
    }),
))
```

**Disable Gzip (Deflate Only):**

```go
server.Use(compressmw.New(
    compressmw.WithOverrideGzip(nil), // Disable gzip
))
```

### Recovery Middleware (`recovermw`)

Recovery middleware catches panics and logs stack traces to help diagnose runtime issues.

| Option                | Description                         | Default     |
| --------------------- | ----------------------------------- | ----------- |
| `WithMaxBytes(bytes)` | Maximum bytes of stack trace to log | No limit    |
| `WithWriteCloser(w)`  | Custom output for stack traces      | `os.Stderr` |

#### Recovery Usage Examples

**Basic Recovery:**

```go
server.Use(recovermw.New())
```

**Custom Stack Trace Output:**

```go
server.Use(recovermw.New(
    recovermw.WithWriteCloser(myCustomWriter),
    recovermw.WithMaxBytes(4096), // Limit to 4KB
))
```
