# mizuoai - OpenAPI Integration

Type-safe OpenAPI documentation and request/response handling for Mizu.

## Philosophy

### Compile-Time Safety for HTTP APIs

Mizuoai shifts serialization type analysis to **compile time** using Go's type system:

```go
// Traditional approach - runtime reflection
func HandleUser(w http.ResponseWriter, r *http.Request) {
    var input CreateUserRequest
    json.NewDecoder(r.Body).Decode(&input) // Runtime errors possible
    // Manual validation required
}

// Mizuoai approach - compile-time type safety
func HandleUser(tx mizuoai.Tx[CreateUserResponse], rx mizuoai.Rx[CreateUserRequest]) {
    input, err := rx.MizuRead() // Type-safe, validated at compile time
    // Handle error, input is guaranteed to match CreateUserRequest
}
```

### How Compile-Time Serde Works

The magic happens in `serde.go` through **generic type instantiation**:

1. **Type Analysis at Initialization**: When you call `mizuoai.Post[I, O]()`, the generic types `I` (input) and `O` (output) are analyzed using reflection

2. **Code Generation via Function Composition**: The `newDecoder[I]()` and `newEncoder[O]()` functions create specialized parsing/encoding functions for your specific types

This approach means **zero type analyzing during request handling** - all type analysis happens at compile-time.

### Tag-Based Parameter Binding

Mizuoai uses struct tags to declaratively define API contracts:

```go
type CreateOrderRequest struct {
    Path struct {
        UserID string `path:"user_id" required:"true"`
    } `mizu:"path"`

    Query struct {
        Currency string `query:"currency" default:"USD"`
    } `mizu:"query"`

    Header struct {
        Auth string `header:"Authorization"`
    } `mizu:"header"`

    Body struct {
        Items []Item `json:"items"`
    } `mizu:"body" required:"true"`
}
```

The tag values drive both **runtime parsing** and **OpenAPI spec generation**.

## Features

- **Compile-time Analysis**: No type analysis during request handling
- **Type-safe handlers** with generic `Tx[O]` and `Rx[I]` types
- **Built-in documentation UI** using Stoplight Elements
- **Support for all parameter locations**: path, query, header, body, form
- **Automatic OpenAPI 3.0 spec generation** from your Go types (and OpenAPI 3.1 compatibility)

## Quick Start

```go
package main

import (
    "log"
    "net/http"
    "github.com/humbornjo/mizu"
    "github.com/humbornjo/mizu/mizuoai"
)

// Define your request/response types
type CreateUserRequest struct {
    Body struct {
        Name  string `json:"name" required:"true"`
        Email string `json:"email" required:"true"`
    } `mizu:"body"`
}

type CreateUserResponse struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

// Handler with type-safe Tx/Rx
createUserHandler := func(tx mizuoai.Tx[CreateUserResponse], rx mizuoai.Rx[CreateUserRequest]) {
    input, err := rx.MizuRead()
    if err != nil {
        tx.WriteHeader(http.StatusBadRequest)
        tx.Write([]byte(err.Error()))
        return
    }

    // input.Body is guaranteed to be populated and validated
    response := CreateUserResponse{
        ID:    "user-123",
        Name:  input.Body.Name,
        Email: input.Body.Email,
    }

    tx.MizuWrite(&response)
}

func main() {
    server := mizu.NewServer("my-api")

    // Initialize OpenAPI with documentation UI
    mizuoai.Initialize(server, "User API",
        mizuoai.WithOaiDocumentation(),
        mizuoai.WithOaiDescription("API for managing users"),
    )

    // Register type-safe handler
    mizuoai.Post(server, "/users", createUserHandler,
        mizuoai.WithOperationSummary("Create a new user"),
        mizuoai.WithOperationTags("users"),
    )

    server.Serve(":8080")
}
```

## Parameter Binding

### Path Parameters

```go
type GetUserRequest struct {
    Path struct {
        UserID string `path:"user_id" required:"true" desc:"User identifier"`
    } `mizu:"path"`
}

mizuoai.Get(server, "/users/{user_id}", getUserHandler)
```

### Query Parameters

```go
type ListUsersRequest struct {
    Query struct {
        Page    int    `query:"page" default:"1"`
        Limit   int    `query:"limit" default:"10"`
        Search  string `query:"search"`
    } `mizu:"query"`
}
```

### Headers

```go
type AuthenticatedRequest struct {
    Header struct {
        Auth     string `header:"Authorization"`
        ClientID string `header:"X-Client-ID"`
    } `mizu:"header"`
}
```

### Request Body (JSON)

```go
type UpdateUserRequest struct {
    Body struct {
        Name     string   `json:"name"`
        Email    string   `json:"email"`
        Tags     []string `json:"tags"`
    } `mizu:"body" required:"true"`
}
```

### Form Data

```go
type UploadRequest struct {
    Form struct {
        Filename string `form:"filename"`
        File     []byte `form:"file"`
    } `mizu:"form"`
}
```

## Type Support

Mizuoai handles various Go types automatically:

### Supported Types

- **Primitives**: `string`, `int`, `int8-64`, `uint`, `uint8-64`, `float32/64`, `bool`
- **Structs**: Nested structs with `json` tags
- **Slices**: Arrays of any supported type
- **Pointers**: Automatically dereferenced

### Response Type Detection

The encoder automatically chooses content type based on the response type:

```go
// JSON response (default for structs)
type UserResponse struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

// Plain text response (for string types)
type MessageResponse string

// Handler will set Content-Type: text/plain
func handleMessage(tx mizuoai.Tx[MessageResponse], rx mizuoai.Rx[any]) {
    tx.MizuWrite(MessageResponse("Hello, World!"))
}
```

## OpenAPI Configuration

### Initialize with Options

```go
mizuoai.Initialize(server, "My API",
    // Enable JSON format (default: YAML)
    mizuoai.WithOaiRenderJson(),

    // Enable documentation UI
    mizuoai.WithOaiDocumentation(),

    // Custom serve path (default: "/")
    mizuoai.WithOaiServePath("/docs"),
)
```

### Operation Configuration

```go
mizuoai.Post(server, "/users", createUserHandler,
    // Operation metadata
    mizuoai.WithOperationSummary("Create user"),
    mizuoai.WithOperationDescription("Creates a new user account"),
    mizuoai.WithOperationOperationId("createUser"),
    mizuoai.WithOperationTags("users", "admin"),

    // Deprecation
    mizuoai.WithOperationDeprecated(),
)
```

### Path Configuration

```go
// Add path-level configuration
mizuoai.Path(server, "/admin/{resource}",
    mizuoai.WithPathDescription("Administrative endpoints"),
    mizuoai.WithPathServer("https://admin.example.com", "Admin server", nil),
)
```

## Performance Considerations

### Benchmark Results

**The benchmark here merely says that `mizuoai` does not has a performance issue compare to `fuego`, not a strict prove that `mizuoai` is much faster than `fuego`**

```
goos: darwin
goarch: arm64
pkg: github.com/humbornjo/mizu/mizuoai
cpu: Apple M2 Pro
BenchmarkMizuOai_Small_Input
BenchmarkMizuOai_Small_Input/Mizu
BenchmarkMizuOai_Small_Input/Mizu-10         	   90024	     13663 ns/op	   29809 B/op	      34 allocs/op
BenchmarkMizuOai_Small_Input/Fuego
BenchmarkMizuOai_Small_Input/Fuego-10        	   56632	     21544 ns/op	   24550 B/op	      22 allocs/op
BenchmarkMizuOai_Large_Input
BenchmarkMizuOai_Large_Input/Mizu
BenchmarkMizuOai_Large_Input/Mizu-10         	     188	   6257025 ns/op	25876087 B/op	     600 allocs/op
BenchmarkMizuOai_Large_Input/Fuego
BenchmarkMizuOai_Large_Input/Fuego-10        	      80	  14643984 ns/op	42898211 B/op	     506 allocs/op
```

### Fieldlet Optimization

Parameter parsing uses sorted slices instead of maps for better performance on small n:

```go
// Map-based approach (typical)
field := fieldMap[paramName]
// O(1) but higher constant factors

// Slice-based approach (mizuoai)
idx, found := slices.BinarySearchFunc(fields, paramName, compare)
// O(log n) per lookup, but better cache locality and
// lower overhead for n < 100 fields

// The implementation chooses slice when struct fields are small (<100),
// as contiguous memory access and avoiding hash computation
// outperforms map lookups in practice.
```

## References

- [OpenAPI 3.0.4 Specification](https://spec.openapis.org/oas/v3.0.4)
- [Stoplight Elements](https://stoplight.io/open-source/elements/)
- [libopenapi](https://github.com/pb33f/libopenapi) - OpenAPI processing
- [Go Generics](https://go.dev/blog/intro-generics) - Type parameters
