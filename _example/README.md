# Mizu Example Application

A comprehensive example demonstrating the Mizu framework for building multi-protocol services in Go: Connect RPC, RESTful HTTP endpoints, CUE-driven contracts, reflected and assembled OpenAPI documentation, and OpenTelemetry observability.

## The CUE Pipeline

The heart of this example is how one CUE module feeds everything:

```
schema.cue ──┬── cue exp gengotypes ──→ Go request/response types (cue_types_gen.go)
             ├── mizucue.LoadModule  ──→ runtime validation of config AND requests
             └── Module.OpenAPIs    ──→ per-package OpenAPI components
                                               │
main.go embeds the CUE tree ───────────────────┤
                                               ▼
              mizuoai.Initialize(…, WithDocumentPatch(importPath, doc)…)
                                               ▼
                            one served document at /openapi.yaml
```

- **`cue.mod/module.cue` declares the same module path as `go.mod`** (`example.com/mizu`). That is what makes component names converge: mizucue renders `example.com.mizu.service.oaisvc.CreateOrderResponse` from the CUE import path, mizuoai renders the identical name from the Go package path, and the CUE component silently shadows the reflected one — same name, richer contract (constraints, doc comments).
- **Requests are validated by the schema that generated their types.** Handlers decode with `rx.Xread()` and reject with `s.schema.Validate(input)` — try posting a negative `amount`.
- **The config is validated the same way** (`config/config.cue` → `Config`), at startup, before any route exists.
- **What reflection cannot infer, CUE states outright**: `#DownloadPackageOperation` is an operation fragment merged over the raw download route via `mizuoai.WithOperationPatch(schema.MustOperation(...))`.

## Services

- **File Service**: Upload/download with streaming support using Connect RPC
- **Greet Service**: Simple HTTP GET endpoint using Connect RPC
- **Namaste Service**: Server-streaming RPC using Connect RPC
- **HTTP Service**: Basic HTTP middleware for logging
- **OpenAPI Service**: Reflected, hand-built, and CUE-patched OpenAPI operations

## Project Structure

```
_example/
├── main.go                 # Entry point: embeds the CUE tree, loads the module
├── go.mod                  # module example.com/mizu (+ cue tool directive)
├── cue.mod/module.cue      # Same module path — the convergence prerequisite
├── Makefile                # Build automation
├── buf.yaml                # Buf configuration
├── buf.gen.yaml            # Buf generation config
├── local.yaml              # Local development config
├── config/                 # Configuration management
│   ├── config.cue          #   #Config: shape, defaults, constraints
│   ├── cue_types_gen.go    #   generated from config.cue
│   └── config.go           # DI setup, config validation, OpenAPI assembly
├── proto/                  # Protocol buffer definitions
│   ├── fooapp/namaste/v1/namaste.proto
│   ├── barapp/greet/v1/greet.proto
│   └── barapp/file/v1/file.proto
├── service/                # Business logic implementations
│   ├── filesvc/            # File upload/download service
│   ├── greetsvc/           # Greeting service
│   ├── httpsvc/            # HTTP middleware service
│   ├── namastesvc/         # Namaste streaming service
│   └── oaisvc/             # OpenAPI service
│       ├── schema.cue      #   the single source of truth
│       └── cue_types_gen.go
├── package/                # Shared packages
│   ├── storage/            # In-memory file storage
│   └── debug/              # Debug utilities
└── protogen/               # Generated protobuf code
```

## Quick Start

### 1. Install Dependencies

```bash
go mod tidy
```

### 2. Generate Code

```bash
make all
```

This command:

- Generates Go types from CUE schemas (`go tool cue exp gengotypes`)
- Compiles protobuf definitions using Buf
- Generates Connect RPC and Go code
- Creates OpenAPI specifications
- Embeds OpenAPI documentation

### 3. Run the Application

```bash
make run
```

The server will start on the configured port (default: 18080) with all services available.

## API Documentation

OpenAPI documentation is automatically generated and available at `http://localhost:18080/openapi`

## CURL Examples

### Greet Service (HTTP/REST)

Simple HTTP GET endpoint:

```bash
# Basic greeting
curl "http://localhost:18080/greet/mizu"

# Response: {"message":"Nihao, mizu"}

# Greeting with custom name
curl "http://localhost:18080/greet/Alice"

# Response: {"message":"Nihao, Alice"}
```

### File Service (Connect RPC over HTTP/2)

File upload and download using Connect RPC protocol:

```bash
# Upload a file
curl --location 'http://localhost:18080/file' --form 'file=@"{FILE_PATH}"'

# Response: {"id": {FILE_ID}, "url": {FILE_URL}}

# Download a file
curl --location 'http://localhost:18080/file/{FILE_ID}'
```

### Namaste Service (Server-Streaming RPC)

```bash
 grpcurl -plaintext -d '{"name": "Mizu"}' \
   localhost:18080 fooapp.namaste.v1.NamasteService/Namaste

 curl -XPOST "http://localhost:18080/namaste" -d '{"name":"I"}'
```

### OpenAPI Service (HTTP/REST)

OpenAPI documentation:

```bash
open "http://localhost:18080/openapi"
```

Server-sent events with immediate flushing:

```bash
curl -N "http://localhost:18080/oai/events"
```

CUE-documented binary download:

```bash
curl -D - "http://localhost:18080/oai/package" \
  --output mizu-example.tar.gz
```

The package route is an ordinary raw HTTP handler. Its `application/gzip`
response and `Content-Disposition` header come from the CUE operation fragment
`#DownloadPackageOperation` in `service/oaisvc/schema.cue`, merged over the
route by `mizuoai.WithOperationPatch`.

Scrape endpoint with header validation — drop the header and CUE rejects the
request with a 400:

```bash
curl -X GET "http://localhost:18080/oai/scrape" \
  -H "key: magic-key-123"

# Response: {"message":"Hello, magic-key-123"}
```

Order processing endpoint with path, query, header, and body parameters — note
the CUE constraint `amount: int & >0`:

```bash
# Valid order
curl -X POST "http://localhost:18080/oai/user/123/order?timestamp=1699123456" \
  -H "X-Region: US" \
  -H "Content-Type: application/json" \
  -d '{"id": "order-456", "amount": 100, "comment": "Test order"}'

# Response: {"amount":1}

# Rejected by the schema, not by hand-written code
curl -X POST "http://localhost:18080/oai/user/123/order" \
  -H "Content-Type: application/json" \
  -d '{"id": "order-456", "amount": -1}'

# Response: 400 Bad Request
```

### HTTP Service (HTTP/REST)

HTTP middleware logging demonstration:

```bash
curl "http://localhost:18080/scrape"
```
