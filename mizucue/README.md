# mizucue

`mizucue` compiles CUE schemas, validates generated Go models, and turns request-local CUE metadata into OpenAPI 3.1 documents.

It is independent from `mizuoai`: `mizucue` owns CUE compilation and OpenAPI generation, while `mizuoai` owns HTTP registration, document validation, and final rendering. Their integration boundary is ordinary OpenAPI bytes.

## Installation

```bash
go get github.com/humbornjo/mizu/mizucue
```

## Compile a schema

Compile an inline schema when it is already embedded as a string:

```go
schema, err := mizucue.Compile(`
package example

#CreateWidgetRequest: {
	body: name: string & != ""
}
`)
if err != nil {
	return err
}
```

Use `MustCompile` for package initialization where a schema error is unrecoverable:

```go
var SCHEMA = mizucue.MustCompile(_SCHEMA_CUE)
```

For a CUE module containing imports, load the package from an explicit filesystem:

```go
//go:embed cue.mod schema
var schemaFS embed.FS

schema, err := mizucue.LoadFS("schema/v1", schemaFS)
if err != nil {
	return err
}
```

`MustLoadFS` is the initialization-time variant. Loading errors and panics include the requested directory.

## Validate generated Go models

`Validate` dereferences pointers, derives the CUE definition from the concrete Go type name, and requires a concrete result:

```go
type CreateWidgetRequest struct {
	Body struct {
		Name string `json:"name"`
	} `json:"body"`
}

if err := mizucue.Validate(schema, &request); err != nil {
	return fmt.Errorf("invalid request: %w", err)
}
```

Nil values, missing definitions, unsatisfied constraints, and non-concrete results are rejected.

## OpenAPI hints

CUE remains the source of validation. Hidden siblings add OpenAPI metadata without entering generated Go types.

### Property hints

A single-underscore sibling enhances an existing property:

```cue
#UploadForm: {
	name: string
	"package": string @go(-)
	_package: contentMediaType: "application/gzip"
}
```

`_package` is merged into the generated `package` property. A hint cannot manufacture a property, and conflicting generated and hinted values fail. Hints follow referenced schemas recursively.

### Operation hints

Quoted double-underscore fields describe one route-bound request definition:

```cue
#UploadPackageRequest: {
	path: scope: string
	form: #UploadForm
	"__method": "post" @go(-)
	"__path": "/registry/scopes/{scope}/packages" @go(-)
	"__operationId": "uploadPackage" @go(-)
	"__responses": {
		"201": description: "Package uploaded"
		"400": description: "Invalid package"
	} @go(-)
}

#UploadPackageResponse: #Package
```

Operation hints are valid only on definitions ending in `Request`. `__method`, `__path`, `__operationId`, and `__responses` are required. Other `__name` fields map to the exact OpenAPI field `name`; `__components` is merged into document components.

Derived request fields have these meanings:

| CUE field | OpenAPI structure |
| --- | --- |
| required `path` | Required path parameters matching every `{placeholder}` exactly |
| required `form` | Required `multipart/form-data` request body |
| required `body` | Required `application/json` request body |

Query and header parameters are supplied explicitly through `__parameters`. A request cannot contain both `form` and `body`. A form property's `contentMediaType` also becomes multipart encoding metadata.

`XxxRequest` pairs with `XxxResponse`. When the single successful response has no explicit content, `mizucue` adds an `application/json` reference to the response component. Explicit content remains authoritative for binary downloads, SSE, WebSocket upgrades, and other raw transports; HTTP 101 counts as the success for an upgrade operation.

## Generate and register an operation

For a raw binary response, keep the transport contract in CUE:

```cue
#DownloadPackageRequest: {
	"__method": "get" @go(-)
	"__path": "/packages/latest" @go(-)
	"__operationId": "downloadPackage" @go(-)
	"__responses": "200": {
		description: "Compressed package"
		headers: "Content-Disposition": {
			required: true
			schema: type: "string"
		}
		content: "application/gzip": {}
	} @go(-)
}

#DownloadPackageResponse: {}
```

Generate OpenAPI bytes, parse them once, and attach the selected operation to its matching raw route:

```go
var document = mizuoai.MustParseOpenAPI(
	mizucue.MustGenerateOpenAPI(schema, "Example API", "v1", "ExampleV1"),
)

mizuoai.GetRaw(server, "/packages/latest", handleDownload,
	mizuoai.WithOpenApiOperation(document, "downloadPackage"),
	mizuoai.WithOperationTags("packages"),
	mizuoai.WithOperationSummary("Download the latest package"),
	mizuoai.WithOperationDescription("Streams the latest compressed package."),
)
```

`GenerateOpenAPI` returns errors; `MustGenerateOpenAPI` panics for initialization-time use. Generated documents remain OpenAPI 3.1.0. `mizuoai` accepts that document, imports the selected operation and its component closure, and renders it through its configured OpenAPI output version.

## Operation projection

When operation hints are present, `mizucue` projects only each request, its matching response, and recursively referenced definitions into CUE's OpenAPI generator. This prevents unrelated provider-neutral unions or arbitrary JSON definitions from breaking route generation.

The projection restores named aliases and supplemental components that CUE may otherwise deduplicate. Property and operation hints are then applied against the complete original schema. Request operation-source components and every `__` hint are removed from the final document.

## Extract one component

For integrations that need an individual component schema:

```go
component, err := mizucue.ExtractOpenAPI(schema, Widget{})
```

Extraction is cached per `Schema`, safe for concurrent callers, and returns an independently mutable top-level map. `MustExtractOpenAPI` is the panic-on-error variant.
