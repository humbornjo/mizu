# mizuoai

`mizuoai` combines Mizu route registration with OpenAPI 3.2 document
generation. It supports two handler styles through one document pipeline:

- typed handlers use reflected Go request and response types;
- raw `http.HandlerFunc` handlers attach an explicit OpenAPI operation without
  changing their transport behavior.

OpenAPI 3.2.0 is the default output version. OpenAPI 3.0 and 3.1 documents —
such as `protoc-gen-connect-openapi` output or [`mizucue`](../mizucue/)
generated package documents — can be preloaded as the base document or merged
as patches.

## Installation

```bash
go get github.com/humbornjo/mizu/mizuoai@v0.3.0
```

Requires Go 1.27+ (`encoding/json/v2`).

## Typed handlers

A typed handler takes the response writer and a typed request envelope, and
returns the output and an error:

```go
type GetUserRequest struct {
	Path struct {
		UserId string `json:"user_id"`
	} `json:"path"`
	Query struct {
		Verbose bool `json:"verbose,omitempty"`
	} `json:"query"`
}

type User struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

func handleGetUser(_ http.ResponseWriter, rx mizuoai.Rx[GetUserRequest]) (User, error) {
	input, err := rx.Xread()
	if err != nil {
		return User{}, mizu.NewError(http.StatusBadRequest, err)
	}
	return User{Id: input.Path.UserId, Name: "Mizu"}, nil
}

srv := mizu.NewServer("users")
if err := mizuoai.Initialize(srv, "Users API",
	mizuoai.WithDocumentInfoVersion("v1"),
	mizuoai.WithDocumentRenderHTML(),
); err != nil {
	panic(err)
}

mizuoai.Get(srv, "/users/{user_id}", handleGetUser,
	mizuoai.WithOperationOperationId("getUser"),
	mizuoai.WithOperationTags("users"),
	mizuoai.WithOperationSummary("Get a user"),
)
```

The returned output is written by the transport: JSON-encoded with
`Content-Type: application/json`, or `text/plain` when the output type is a
string. A returned error is translated by `mizu.ResponseError`. A handler that
writes the response body itself takes over — the output write and the error
translation are both skipped.

Request-envelope fields use their JSON name to select a transport location:

- `json:"path"`
- `json:"query"`
- `json:"header"`
- `json:"body"`
- `json:"form"` for `multipart/form-data` or
  `application/x-www-form-urlencoded`

Path, query, and header groups must be structs; inner fields use ordinary JSON
names for both runtime decoding and OpenAPI parameters. Header lookups treat
`_` in a JSON name as `-`. An input may carry a body or a form, never both —
violations fail at handler construction. The form decoder selects the
urlencoded or multipart implementation from the request's actual
`Content-Type`; the reflected document currently describes the urlencoded
media type.

## Reflected schemas and components

Named Go struct types are emitted once under `components.schemas` and
referenced with `$ref`. The component key is the canonical type name —
`<import path>.<TypeName>` with `/` rendered as `.`, the same shape
[`mizucue`](../mizucue/) generates from CUE — so a CUE document patch and a
reflected type of the same logical model converge on one component. Use
`WithDocumentSchemaName` when a type needs a stable public name:

```go
mizuoai.Initialize(srv, "Service API",
	mizuoai.WithDocumentSchemaName[CreateUserRequest]("CreateUserRequest"),
)
```

Reflection supports:

- booleans, strings, signed and unsigned integers, and floating-point values;
- pointers, which union the resolved type with `"null"`;
- arrays, slices, and maps with string keys;
- `time.Time` (`string`, `date-time`), `json.RawMessage` (free-form), and
  `[]byte` (`string` with base64 content encoding);
- embedded structs, which flatten into the parent — flattening through a
  pointer marks every promoted field optional.

Field naming follows `encoding/json`: the `json` tag name wins, `"-"` skips,
untagged fields keep their Go name. A field is required unless it is
`omitempty`, a pointer, or promoted through a pointer-embedded struct; an
explicit `required:"true"` or `required:"false"` tag overrides the default.
Path parameters are always required.

Recursive named structs are not yet supported — the cycle guard is a known
gap. Reflection supplies a sound default request and success-response schema;
it cannot infer error responses, security policy, content negotiation,
examples, callbacks, or protocol semantics. Use operation options or an
operation patch for those.

## Operation options

Native `libopenapi` models provide fields that reflection cannot infer:

```go
mizuoai.Post(srv, "/users", handleCreateUser,
	mizuoai.WithOperationRequestBody(requestBody),
	mizuoai.WithOperationResponse(http.StatusCreated, createdResponse),
	mizuoai.WithOperationResponse(http.StatusBadRequest, badRequestResponse),
)
```

Options apply in order and later options win. Scalar fields replace; lists
such as tags replace wholesale; parameters merge by `(name, in)`; the request
body replaces wholesale; responses merge per status code; extensions merge
key-wise.

`WithOperationBase` supplies a complete `*v3.Operation` as the merge base —
on a raw registration it is the operation; on a typed registration it
replaces the reflected one. `WithOperationRequestBody` and
`WithOperationResponses` replace the reflected request-body and response
defaults; `WithOperationResponse` adds or replaces one status code.

`WithOperationPatch` merges a raw JSON operation fragment over the operation.
The fragment parses eagerly at option construction and panics on malformed
input. Fragment-local `$ref`s (`#/components/schemas/...`) resolve against
the final assembled document at render time. The typical fragment source is a
CUE schema through [`mizucue`](../mizucue/)'s `Schema.Operation`:

```go
mizuoai.GetRaw(srv, "/package", handlePackage,
	mizuoai.WithOperationPatch(
		schema.MustOperation(reflect.TypeFor[DownloadPackageOperation]())),
)
```

## Raw handlers

Raw registration is intended for multipart uploads, binary downloads, SSE,
WebSocket upgrades, proxies, and other handlers where the typed transport is
not the implementation:

```go
mizuoai.PostRaw(srv, "/upload", handleUpload,
	mizuoai.WithOperationBase(uploadOperation),
)
```

Raw registration passes the `http.ResponseWriter` and `*http.Request` received
after Mizu middleware directly to the handler. It does not read or replace the
body, wrap the writer, infer content, delay headers, or interfere with
flushing and upgrades.

Helpers are available for every HTTP method currently supported by Mizu:
`GetRaw`, `PostRaw`, `PutRaw`, `DeleteRaw`, `PatchRaw`, `HeadRaw`,
`OptionsRaw`, `TraceRaw`, and `ConnectRaw` — the last rendered through
OpenAPI 3.2 `additionalOperations`.

## Document assembly

`Initialize` is called once per server and serves the rendered document —
YAML at `/openapi.yaml` by default:

```go
mizuoai.Initialize(srv, "Service API",
	mizuoai.WithDocumentVersion("3.2.0"),
	mizuoai.WithDocumentInfoVersion("v1"),
	mizuoai.WithDocumentSummary("Service API"),
	mizuoai.WithDocumentDescription("HTTP API for the service"),
	mizuoai.WithDocumentSelf("https://example.com/openapi.yaml"),
	mizuoai.WithDocumentJsonSchemaDialect("https://json-schema.org/draft/2020-12/schema"),
	mizuoai.WithDocumentRoute("/docs"),
	mizuoai.WithDocumentJSON(),
	mizuoai.WithDocumentRenderHTML(),
)
```

`WithDocumentJSON` switches the endpoint to `/openapi.json`;
`WithDocumentRoute` prefixes both paths; `WithDocumentRenderHTML` serves a
Stoplight Elements UI at `/openapi` under the same prefix.

`WithDocumentBase` preloads a complete OpenAPI 3.0, 3.1, or 3.2 document as
the base that registrations accumulate onto — the compatibility path for
generated documents such as `protoc-gen-connect-openapi` output. Output is
normalized to the configured target version, defaulting to 3.2.0; 3.1.x
targets are available through `WithDocumentVersion`.

`WithDocumentPatch` merges the component schemas of a raw document —
typically one `mizucue` package's generated output — into the assembled
document. Canonical naming makes the name the schema's identity: a name
repeated across patches dedupes silently, first occurrence wins, and at
render a patch schema shadows the reflected schema of the same name (logged
at debug level). Assembling a whole CUE module is a loop:

```go
var options []mizuoai.DocumentOption
for importPath, doc := range module.MustOpenAPIs(nil) {
	options = append(options, mizuoai.WithDocumentPatch(importPath, doc))
}
mizuoai.Initialize(srv, "Service API", options...)
```

The remaining document options cover the OpenAPI surface directly:
`WithDocumentInfo`, `WithDocumentTermsOfService`, `WithDocumentContact`,
`WithDocumentLicense`, `WithDocumentServer(Object)`,
`WithDocumentTag(Object)`, `WithDocumentSecurity(Scheme)`,
`WithDocumentComponents`, `WithDocumentWebhook`, `WithDocumentExternalDocs`,
and `WithDocumentExtensions`.

## Validation behavior

The rendered document is validated against the official OpenAPI schema
through `libopenapi-validator` — at `Initialize`, so an invalid document
fails before any route exists, and again when the document is served.
Registration rejects:

- duplicate method/path pairs;
- duplicate non-empty operation IDs.

The document builder merges methods into one Path Item, so GET and POST can
safely share a route path. Rendering starts from the accumulated base
document overlaid with the document options and is deterministic for an
identical registration sequence.

## Migration notes

- Typed handlers are now `func(http.ResponseWriter, Rx[I]) (O, error)`: the
  returned output and error drive the transport. `MizuRead`/`MizuWrite` are
  now `Xread`/`Xwrite`.
- `WithOperation` is now `WithOperationBase`; `WithResponse` is now
  `WithOperationResponse`.
- `WithDocumentDocumentation` is now `WithDocumentRenderHTML`,
  `WithDocumentPreLoad` is now `WithDocumentBase`, `WithDocumentServePath` is
  now `WithDocumentRoute`, and `WithDocumentRenderJson` is now
  `WithDocumentJSON`.
- `ParseOpenAPI`, `WithOpenApiOperation`, and `WithOpenApiOperationPatch` are
  removed. Preload the document with `WithDocumentBase` and attach operations
  with `WithOperationBase`, or merge a fragment with `WithOperationPatch`.
- Reflection reads `json` (with `omitempty`) and `required` tags only. The
  former schema and parameter tags (`desc`, `enum`, `format`, `pattern`,
  `minLength`, `style`, `explode`, and friends) and the legacy `mizu` tags
  are no longer read — state those constraints in CUE through
  [`mizucue`](../mizucue/) or with explicit operation options.
- Component names are now canonical import-path-qualified names
  (`example.com.mizu.service.oaisvc.CreateOrderResponse`). Use
  `WithDocumentSchemaName[T]` to pin a stable public name.
- The generated version remains OpenAPI 3.2.0. Use
  `WithDocumentVersion("3.1.x")` only when a downstream tool still requires
  3.1.

## References

- [OpenAPI 3.2 specification](https://spec.openapis.org/oas/v3.2.0)
- [Sequential media types](https://spec.openapis.org/oas/v3.2.0#sequential-media-types)
- [Server-Sent Events](https://spec.openapis.org/oas/v3.2.0#special-considerations-for-server-sent-events)
- [libopenapi](https://github.com/pb33f/libopenapi)
- [libopenapi-validator](https://github.com/pb33f/libopenapi-validator)
