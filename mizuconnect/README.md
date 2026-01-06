# mizuconnect - Connect RPC Integration

Streamlined integration with Connect protocol for type-safe RPC services built on protobuffer definitions.

## Features

- **[Health Checks](https://github.com/connectrpc/grpchealth-go)** - Built-in gRPC health service
- **[Reflection](https://github.com/connectrpc/grpcreflect-go)** - Runtime service discovery via gRPC reflection
- **[Validation](https://github.com/bufbuild/protovalidate)** - Protocol buffer validation support
- **[REST Transcoding](https://github.com/connectrpc/vanguard-go)** - Vanguard integration for HTTP-to-RPC mapping

## Installation

```bash
go get github.com/humbornjo/mizu/mizuconnect
```

## Quick Start

For a more comprehensive example, refer to [examples](../_example/)

### Generate Connect Code

Refer to the official doc for [Buf Build](https://buf.build/docs/)

### Register with Mizu

```go
func main() {
    server := mizu.NewServer("greet-service")

    // Create Connect RPC scope with all features
    scope := mizuconnect.NewScope(server,
        mizuconnect.WithGrpcHealth(),
        mizuconnect.WithGrpcReflect(),
        mizuconnect.WithCrpcVanguard("/"),
        mizuconnect.WithCrpcHandlerOptions(
            connect.WithInterceptors(YourCustomInterceptor()),
        ),
    )

    // Register your service
    greeter := &GreetService{}
    scope.Register(greeter, greetv1connect.NewGreetServiceHandler)

    // Start server
    server.ServeContext(context.Background(), ":8080")
}
```

## Configuration Options

### Connect RPC Options

| Option                               | Description                           | Default  |
| ------------------------------------ | ------------------------------------- | -------- |
| `WithGrpcHealth()`                   | Enable gRPC health check endpoint     | Disabled |
| `WithGrpcReflect(opts...)`           | Enable gRPC reflection for discovery  | Disabled |
| `WithCrpcValidate()`                 | Enable protocol buffer validation     | Disabled |
| `WithCrpcVanguard(pattern, opts...)` | Enable REST transcoding with Vanguard | Disabled |
| `WithCrpcHandlerOptions(opts...)`    | Additional Connect handler options    | `nil`    |

## Examples

### Basic Service Without Extra Features

```go
// Minimal setup with just service registration
scope := mizuconnect.NewScope(server)
greeter := &GreetService{}
scope.Register(greeter, greetv1connect.NewGreetServiceHandler)
```

### Service with Validation Only

```go
scope := mizuconnect.NewScope(server,
    mizuconnect.WithCrpcValidate(),
)
```

### Service with Custom Options

```go
scope := mizuconnect.NewScope(server,
    mizuconnect.WithGrpcHealth(),
    mizuconnect.WithGrpcReflect(),
    mizuconnect.WithCrpcHandlerOptions(
        connect.WithInterceptors(loggingInterceptor()),
    ),
)
```

### Vanguard (REST Transcoding) with Precision

```go
scope := mizuconnect.NewScope(server,
    mizuconnect.WithCrpcVanguard("/api/v1/"),
)
```

## Scrape on RESTFUL toolkits

The `restful` folder contains utility packages that make it super easy to develop RESTful APIs with Connect RPC, especially for common requirements like file handling.

### package `filekit`

**Effortless file upload/download with automatic proto parsing**

The `filekit` package eliminates the complexity of handling multipart form data and file streams. Simply define your request/response types in protobuf, and the package automatically handles:

- **Automatic form parsing** - Non-file fields are automatically mapped to your proto message
- **Built-in validation** - Size limits, checksums (SHA256), and MIME type detection
- **Stream support** - Both unary and streaming file operations

**Just modify your proto definition and go developing with parsing free**

```go
// In your .proto file
message UploadRequest {
  string name = 1;    // Automatically parsed from form field "name"
  int32 category = 2; // Automatically parse form field "category" to int32
  google.api.HttpBody form = 1;
}

// "file" will be the form field name where the file is uploaded
reader, err := filekit.NewFormReader("file", stream, &msg)
```

> All the parsing is constrainted withe a limit-reader to prevent unexpected attacks.

### package `streamkit`

**Iterator utilities for Connect streams**

The `streamkit` package provides simple iterator functions to work with Connect RPC streams without manual EOF checking,
transforming it into iterator.

```go
// Before: Manual EOF checking
count := 0
for {
    if !stream.Receive() {
        if err := stream.Err(); errors.Is(err, io.EOF) {
            break
        }
        return err
    }
    msg := stream.Msg()
    count++
}

// After: Clean iterator
for msg, err := range streamkit.FromClientStream(&stream) {
    if err != nil {
        return err
    }
    // Process msg
}
```
