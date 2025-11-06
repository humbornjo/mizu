# 🔌 mizuconnect - Connect RPC Integration

Streamlined integration with Connect protocol for type-safe RPC services built on protobuffer definitions.

## ✨ Features

- **[Health Checks](https://github.com/connectrpc/grpchealth-go)** - Built-in gRPC health service
- **[Reflection](https://github.com/connectrpc/grpcreflect-go)** - Runtime service discovery via gRPC reflection
- **[Validation](https://github.com/bufbuild/protovalidate)** - Protocol buffer validation support
- **[REST Transcoding](https://github.com/connectrpc/vanguard-go)** - Vanguard integration for HTTP-to-RPC mapping

## 📦 Installation

```bash
go get github.com/humbornjo/mizu/mizuconnect
```

## 🚀 Quick Start

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

## 🔧 Configuration Options

### Connect RPC Options

| Option                               | Description                           | Default  |
| ------------------------------------ | ------------------------------------- | -------- |
| `WithGrpcHealth()`                   | Enable gRPC health check endpoint     | Disabled |
| `WithGrpcReflect(opts...)`           | Enable gRPC reflection for discovery  | Disabled |
| `WithCrpcValidate()`                 | Enable protocol buffer validation     | Disabled |
| `WithCrpcVanguard(pattern, opts...)` | Enable REST transcoding with Vanguard | Disabled |
| `WithCrpcHandlerOptions(opts...)`    | Additional Connect handler options    | `nil`    |

## 🎯 Key Functions

### NewScope

Create a new Connect RPC integration scope:

```go
func NewScope(srv *mizu.Server, opts ...Option) *Scope
```

### Register

Register a Connect RPC service implementation:

```go
func (s *Scope) Register(impl any, newFunc any, opts ...connect.HandlerOption)
```

### Use/Uses

Apply vanguard service options to specific services:

```go
func (s *Scope) Use(svcOpt vanguard.ServiceOption) *relayScope
func (s *Scope) Uses(svcOpts ...vanguard.ServiceOption) *relayScope
```

## 💡 Examples

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

## 🌊 Integration with Mizu

mizuconnect is designed to work seamlessly with the Mizu HTTP framework:

```go
import (
    "github.com/humbornjo/mizu"
    "github.com/humbornjo/mizu/mizuconnect"
)

func main() {
    server := mizu.NewServer("api-service")

    // Set up Connect RPC with features
    scope := mizuconnect.NewScope(server,
        mizuconnect.WithGrpcHealth(),
        mizuconnect.WithGrpcReflect(),
        mizuconnect.WithCrpcVanguard("/"),
    )

    // Register multiple services
    scope.Register(&UserService{}, userv1connect.NewUserServiceHandler)
    scope.Register(&OrderService{}, orderv1connect.NewOrderServiceHandler)

    // Start with graceful shutdown
    server.ServeContext(context.Background(), ":8080")
}
```
