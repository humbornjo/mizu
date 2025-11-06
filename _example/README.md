# Mizu Example Application

A comprehensive example demonstrating the Mizu framework for building multi-protocol microservices in Go. This application showcases Connect RPC, RESTful HTTP endpoints, OpenAPI documentation, and OpenTelemetry observability.

## Overview

This example application implements multiple services to demonstrate various protocol capabilities:

- **File Service**: Upload/download with streaming support using Connect RPC
- **Greet Service**: Simple HTTP GET endpoint using Connect RPC
- **Namaste Service**: Bidirectional streaming RPC using Connect RPC
- **HTTP Service**: Basic HTTP middleware for logging
- **OpenAPI Service**: OpenAPI documentation and validation endpoints

## Project Structure

```
_example/
├── main.go                 # Application entry point
├── go.mod                  # Go module definition
├── Makefile                # Build automation
├── buf.yaml                # Buf configuration
├── buf.gen.yaml            # Buf generation config
├── local.yaml              # Local development config
├── config/                 # Configuration management
│   └── config.go           # Dependency injection setup
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

### 2. Generate Protobuf Code

```bash
make all
```

This command:

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

### Namaste Service (Streaming RPC)

Bidirectional streaming (requires HTTP/2):

```bash
 grpcurl -plaintext -d '{"name": "Mizu"}' \
   localhost:18080 fooapp.namaste.v1.NamasteService/Namaste
```

### OpenAPI Service (HTTP/REST)

Order processing endpoint with OpenAPI validation:

```bash
# Process an order with path, query, header, and body parameters
curl -X POST "http://localhost:18080/oai/order/123?timestamp=1699123456" \
  -H "X-Region: US" \
  -H "Content-Type: application/json" \
  -d '{"id": "order-456", "amount": 100, "comment": "Test order"}'

# Scrape endpoint with header validation
curl -X POST "http://localhost:18080/oai/scrape" \
  -H "key: magic-key-123"

```

### HTTP Service (HTTP/REST)

HTTP middleware logging demonstration:

```bash
curl "http://localhost:18080/greet/test"
```
