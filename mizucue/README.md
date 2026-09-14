# mizucue

`mizucue` compiles CUE schemas, validates generated Go models, and renders raw CUE OpenAPI documents.

It is independent from `mizuoai`: `mizucue` owns CUE compilation, model validation, and per-package OpenAPI generation, while `mizuoai` owns assembling and serving the merged document. Their integration boundary is ordinary OpenAPI bytes.

## Installation

```bash
go get github.com/humbornjo/mizu/mizucue@v0.3.0
```

## Load an inline schema

Load a schema that is already embedded as a string:

```go
schema, err := mizucue.LoadSchema(`
package example

#CreateWidgetRequest: {
	name: string & != ""
}
`)
if err != nil {
	return err
}
```

`MustLoadSchema` is the initialization-time variant, panicking on failure:

```go
var SCHEMA = mizucue.MustLoadSchema(_SCHEMA_CUE)
```

## Load every package as one module

`LoadModule` compiles every CUE package of a module into one shared `Module`. The filesystem must hold the module's `cue.mod/module.cue`; imports resolve through the declared module path:

```go
//go:embed all:package all:service cue.mod
var schemaFS embed.FS

module, err := mizucue.LoadModule(schemaFS)
if err != nil {
	return err
}
```

Cross-package imports, stdlib packages, and build constraints are the CUE loader's standard semantics.

`Module.Extract` builds a `Schema` from the first package whose package name matches. Package names are not unique across a module's directories; selection follows loader order:

```go
schema, err := module.Extract("agent")
if err != nil {
	return err
}
```

`MustExtract` is the initialization-time variant.

`Module.Instances` exposes the underlying CUE loader instances in loader order, for consumers that need more than the schema and OpenAPI views — import paths, package names, files, dependencies:

```go
for instance := range module.Instances() {
	// instance is the loader's *build.Instance; its ImportPath
	// carries the loader's canonical "@version" suffix.
}
```

## Validate generated Go models

`Validate` dereferences pointers, derives the CUE definition from the concrete Go type name, and requires a concrete result:

```go
type CreateWidgetRequest struct {
	Name string `json:"name"`
}

if err := schema.Validate(&request); err != nil {
	return fmt.Errorf("invalid request: %w", err)
}
```

Nil values, missing definitions, unsatisfied constraints, and non-concrete results are rejected.

## Render operation fragments

`Operation` renders the CUE member named after a Go type — mirroring `Validate`'s naming — as JSON bytes. The fragment carries what `mizuoai`'s reflection cannot infer per operation (request body media types, response tables) and merges over the reflected operation via `mizuoai.WithOperationPatch`. The member must be concrete:

```cue
#DownloadPackageOperation: {
	operationId: "downloadPackage"
	responses: "200": content: "application/gzip": schema: type: "string"
} @go(-)
```

```go
fragment, err := schema.Operation(reflect.TypeFor[DownloadPackageOperation]())
```

`MustOperation` is the initialization-time variant. The Go type only needs to exist for `reflect.TypeFor` — `cue exp gengotypes` emits a near-empty struct when the member carries `@go(-)`. See the [example application](../_example/service/oaisvc/) for the full integration.

## Generate OpenAPI documents

`Schema.OpenAPI` renders the schema's raw CUE OpenAPI document as JSON bytes. It is sugar over CUE's `openapi.Generate`: the config passes through untouched, nothing is post-processed, and every call regenerates from scratch:

```go
raw, err := schema.OpenAPI(&openapi.Config{
	Info: map[string]any{"title": "Example API", "version": "v1"},
})
```

`Module.OpenAPIs` does the same for every package in the module and returns the documents as an import-path-ordered sequence:

```go
documents, err := module.OpenAPIs(nil)
if err != nil {
	return err
}
for importPath, raw := range documents {
	// importPath is the declared module path plus the package's
	// relative dir; raw is the package's OpenAPI JSON document.
}
```

Component names are qualified k8s-style with the definition's source package import path — `example.com.mizucue.test.app.TestModel`, the same shape as `mizuoai`'s `CanonicalTypeName` — so same-named definitions from different packages coexist in one assembled document and cross-package `$ref`s carry their provenance. A config `NameFunc` replaces the default naming; every other config field passes through untouched, and the caller's config is never mutated. `Schema.OpenAPI`, by contrast, stays verbatim.

A package that fails generation aborts the whole call with an error naming the package. Constructs the CUE generator rejects (`!=` string bounds, for one) fail as-is; `mizucue` does not sanitize. `MustOpenAPI` and `MustOpenAPIs` are the initialization-time variants; `mizuoai` assembles the per-package documents, with each package's import path passed along as the sequence key.

## Bake openapi.yaml from the command line

`mizucuegen` writes every package's generated document to `openapi.yaml` next to the package's CUE files. Run it from anywhere inside the CUE module — it walks up from the argument directory (default `.`) until a `cue.mod/module.cue`, and errors when none is found:

```bash
go tool mizucuegen ./schemas
```

Track the tool in your module (Go 1.24+):

```bash
go get -tool github.com/humbornjo/mizu/mizucue/cmd/mizucuegen
```

or install it as a plain binary:

```bash
go install github.com/humbornjo/mizu/mizucue/cmd/mizucuegen@latest
```

It also hangs off `go:generate`:

```go
//go:generate go run github.com/humbornjo/mizu/mizucue/cmd/mizucuegen ./schemas
```

The baked files are the documents `Module.OpenAPIs` returns, as YAML — pass them to `mizuoai` for assembly.
