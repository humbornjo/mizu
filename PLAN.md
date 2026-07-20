# `mizuoai` OpenAPI Refinement Plan

## Objective

Refactor `mizuoai` in two ordered steps:

1. Make reflected OpenAPI generation correct, component-aware, deterministic,
   and extensible while preserving the existing typed handler APIs.
2. Add raw `http.HandlerFunc` registration that uses the same route and
   OpenAPI assembly pipeline without changing request or response transport
   behavior.

The first step is a prerequisite for the second. Raw registration must not be
bolted onto the current renderer or introduce a second document-building path.

## Design principles

- Runtime handling and OpenAPI description are separate concerns.
- Typed and raw routes share one operation and component registry.
- Reflection provides a sound default, not a claim that every OpenAPI feature
  can be inferred from Go types.
- Explicit operation options or imported OpenAPI contracts remain the source
  for status codes, errors, security, examples, polymorphism, uncommon media
  types, and other behavior that Go types cannot express.
- Invalid paths, duplicate operations, unresolved references, and incompatible
  component collisions fail before traffic is served.
- Rendering does not mutate the configured base document and produces stable
  YAML and JSON for identical registrations.
- Existing typed `Get`, `Post`, `Put`, `Delete`, `Patch`, `Head`, `Options`, and
  `Trace` handlers retain their runtime behavior.

## Current gaps

The current implementation needs structural work before it can support raw
operations safely:

- Named Go types are expanded inline instead of being placed under
  `components.schemas` and referenced with `$ref`.
- Schema reflection covers only a small subset of Go types and is not
  cycle-aware.
- Required fields, JSON tag options, maps, aliases, common standard-library
  types, nullable values, and recursive models are not handled consistently.
- Request decoding and parameter documentation do not use one canonical tag
  interpretation.
- Form decoding and the documented form media type disagree.
- Reflected responses lack several required OpenAPI details and attach
  `encoding` where it is not valid for response content.
- Operations are currently keyed as `"METHOD /path"` inside the OpenAPI
  `paths` map. OpenAPI instead requires one path key with methods stored on its
  `PathItem`.
- Rendering mutates the preloaded model and reports some initialization errors
  by printing instead of returning or raising them.
- Version checking excludes newer patch releases and OpenAPI 3.2 despite the
  installed `libopenapi` version supporting it.
- Existing tests mostly exercise request and response serialization rather
  than the complete rendered document.

## Step 1: Refine and refactor reflected generation

### Milestone 1: Document assembly core

Introduce one internal document builder responsible for paths, operations,
components, validation, and rendering.

Suggested internal boundary:

```go
builder.AddOperation(method, resolvedPath, operation, components)
```

Deliverables:

- Store paths by their resolved OpenAPI path.
- Merge different HTTP methods into the same `PathItem`.
- Reject duplicate `(method, path)` registrations.
- Track and reject duplicate non-empty operation IDs.
- Apply group prefixes through the same `mizu.Server.Pattern` behavior used by
  runtime registration.
- Build a fresh output document for every render instead of mutating the base
  document.
- Replace print-and-continue initialization failures with returned errors or
  startup panics for programmer configuration errors.
- Validate the assembled document through `libopenapi` before exposing it.
- Keep YAML and JSON rendering structurally equivalent and deterministic.

Tests and exit gate:

- Register GET and POST on the same path and confirm both methods survive.
- Register nested groups and confirm the fully resolved path.
- Reject duplicate method/path and operation ID registrations.
- Render repeatedly and confirm byte-stable output for each format.
- Parse the rendered YAML and JSON back through `libopenapi` without errors.

### Milestone 2: Schema reflector and component registry

Replace the standalone recursive `createSchema` function with a cycle-aware
schema reflector owned by the document builder.

Deliverables:

- Put reusable named Go types in `components.schemas`.
- Return `$ref` proxies for named component types.
- Inline anonymous structs where no stable component identity exists.
- Track reflection by `reflect.Type` to handle recursion and reuse.
- Define deterministic component naming and detect different Go types that
  resolve to the same component name.
- Provide an explicit naming override for genuine collisions.
- Support at least:
  - booleans, strings, signed and unsigned integers, and floating-point types;
  - pointers and aliases;
  - arrays and slices;
  - maps through `additionalProperties`;
  - embedded and ordinary structs;
  - `time.Time` as `string` with `date-time` format;
  - `json.RawMessage` as unconstrained JSON;
  - `[]byte` according to the selected OpenAPI version and media context.
- Respect exported fields, `json:"-"`, JSON field names, and `omitempty`.
- Define and document how explicit `required` metadata overrides inferred
  optionality.
- Merge equal component definitions and reject incompatible definitions with
  the same name.

Tests and exit gate:

- Shared nested types produce one component and multiple `$ref` usages.
- Recursive and mutually recursive types render without infinite recursion.
- Maps, aliases, pointers, arrays, `time.Time`, `json.RawMessage`, and `[]byte`
  have structural golden tests.
- Component output order is deterministic.
- Equal-name/equal-definition reuse succeeds; incompatible collisions fail.

### Milestone 3: Operation reflection

Rebuild operation reflection on top of the component-aware schema reflector.

Deliverables:

- Establish one canonical request-envelope convention for `path`, `query`,
  `header`, `body`, and `form` fields.
- Use JSON field names consistently for runtime decoding and documentation.
- Require every OpenAPI path parameter and verify that documented path
  parameters match router placeholders.
- Support parameter descriptions, deprecation, defaults, examples, and the
  relevant style/explode metadata when supplied explicitly.
- Generate valid request bodies for JSON, text, URL-encoded, multipart, and
  explicitly selected media types.
- Correct the existing form media-type/runtime mismatch.
- Generate valid response descriptions and content without response-side
  Encoding Objects.
- Replace the single response override with composable response definitions
  that can describe multiple statuses, media types, headers, links, and empty
  responses.
- Preserve explicit operation metadata such as tags, summary, description,
  deprecation, security, callbacks, servers, extensions, and operation ID.
- Reject contradictory options instead of silently selecting a winner.

Reflection boundaries:

- Go types may provide the default schema for a request or success response.
- HTTP status codes beyond the default success case are explicit.
- Error responses, security, examples, content negotiation, callbacks,
  discriminators, and protocol semantics are explicit or externally supplied.

Tests and exit gate:

- Cover path, query, header, JSON body, text body, URL-encoded form, and
  multipart metadata.
- Confirm required and optional fields match the documented tag rules.
- Cover multiple success and error responses, response headers, and empty
  bodies.
- Compare complete operations structurally rather than checking fragments.
- Confirm all existing typed handlers behave unchanged at runtime.

### Milestone 4: Version policy and validation

Make OpenAPI version behavior explicit and testable.

Deliverables:

- Add an OpenAPI version option.
- Accept `3.0.x`, `3.1.x`, and `3.2.x` input documents rather than matching
  only one patch release.
- Generate `3.2.0` by default, with optional `3.1.x` output for older
  downstream tooling. OpenAPI 3.0 is an input compatibility format, not an
  output target.
- Support OpenAPI 3.2 sequential media types and SSE `itemSchema`.
- Render reflected binary schemas using OpenAPI 3.1/3.2 media types and JSON
  Schema `contentMediaType`/`contentEncoding` semantics where applicable.
- Validate input and rendered artifacts against the official embedded OpenAPI
  3.0, 3.1, or 3.2 schema, in addition to building the libopenapi model.
- Preserve raw 3.2 fields that libopenapi's high-level model does not yet
  expose, while keeping explicit configured overlays authoritative.
- Reject fields that are unavailable in the selected version.
- Pin the Stoplight Elements asset version and add a documentation UI smoke
  test so UI behavior does not drift independently of `mizuoai`.

Tests and exit gate:

- Maintain structural fixtures for 3.1 and 3.2 output plus 3.0 input.
- Parse and validate every fixture using `libopenapi`.
- Confirm 3.2 `itemSchema` is rejected for 3.1 output.
- Confirm the pinned documentation UI loads a generated document.

### Milestone 5: Compatibility and release preparation

Deliverables:

- Keep existing typed registration signatures and serde behavior.
- Update the README to match the actual canonical tags and media behavior.
- If legacy tag forms are still used, support them for one deprecation window
  with explicit conflict detection.
- Add migration notes for changes that correct previously invalid OpenAPI.
- Run tests for the root Mizu module and the `mizuoai` module.
- Treat semantic tag or schema changes as a deliberate pre-1.0 compatibility
  release rather than an undocumented patch.

Step 1 is complete only when the full rendered documents are valid and tested;
passing request-decoder tests alone is insufficient.

## Step 2: Support raw registration

Build raw registration as another operation source feeding the same document
builder from Step 1.

### Public API direction

Provide one internal generic registrar and convenient helpers for every HTTP
method already supported by `mizuoai`:

```go
mizuoai.PostRaw(
	group,
	"/scopes/{scope}/skills",
	svc.HandleRegisterSkill,
	mizuoai.WithOpenApiOperation(doc, "registerSkill"),
)
```

A parsed reusable document is preferable to parsing the same embedded bytes at
each registration:

```go
doc, err := mizuoai.ParseOpenAPI(registryv1.OPENAPI)
```

Deliverables:

- Add a raw registrar that accepts `http.HandlerFunc`.
- Pass the request and response writer to the handler exactly as received after
  Mizu middleware.
- Do not parse, read, replace, or buffer the request body.
- Do not wrap the response writer, infer a response body, delay headers, or
  interfere with flushing, hijacking, upgrades, or streaming.
- Validate and attach the OpenAPI operation before registering the runtime
  route so a documentation failure cannot leave an undocumented live route.
- Accept either a fully constructed operation or one selected from a parsed
  OpenAPI document by operation ID.
- For imported operations:
  - reject missing and duplicate operation IDs;
  - verify documented method/path against the fully resolved runtime route;
  - preserve the operation without reflection;
  - include inherited path-item parameters where applicable;
  - resolve and import the transitive closure of referenced schemas,
    responses, parameters, examples, request bodies, headers, security
    schemes, links, callbacks, path items, and media types;
  - merge identical components and reject incompatible name collisions.
- When an external operation is supplied, treat it as authoritative. Do not
  silently merge reflected fields into it.

Tests and exit gate:

- Register typed and raw methods on the same fully prefixed path.
- Confirm a multipart request reaches the raw handler byte-for-byte.
- Confirm a streamed response can flush incrementally without a `mizuoai`
  wrapper.
- Confirm middleware and route-group behavior matches typed registration.
- Round-trip multipart encoding, binary responses, response headers, security,
  errors, extensions, and referenced components structurally.
- Reject missing/duplicate operation IDs, path or method mismatches, unresolved
  references, duplicate routes, and incompatible component collisions before
  serving.
- Confirm existing typed registrations still use reflection when no external
  operation is supplied.

## Official transport descriptions

OpenAPI does not define special operation kinds for upload, download, or SSE.
They are ordinary operations described by request and response media types.

### Multipart upload

OpenAPI 3.0 describes uploaded files using `type: string` and
`format: binary` inside a `multipart/form-data` request body:

```yaml
requestBody:
  required: true
  content:
    multipart/form-data:
      schema:
        type: object
        required: [name, version, package]
        properties:
          name:
            type: string
          version:
            type: string
          package:
            type: string
            format: binary
      encoding:
        package:
          contentType: application/gzip
```

OpenAPI 3.1 and 3.2 model raw binary primarily through the media type and JSON
Schema `contentMediaType`/`contentEncoding` semantics. Version-specific output
must not assume that `format: binary` controls encoding outside OpenAPI 3.0.

### Binary download

```yaml
responses:
  "200":
    description: Compressed skill package
    headers:
      Content-Disposition:
        required: true
        schema:
          type: string
        example: attachment; filename="skill.tar.gz"
    content:
      application/gzip:
        schema:
          type: string
          format: binary
```

In OpenAPI 3.1+, a raw binary media type may omit the schema. Streaming the
response is a runtime behavior and does not require a separate OpenAPI flag.
Range support, if implemented, should explicitly document `206` and its
associated request and response headers.

### Server-Sent Events

OpenAPI 3.0 and 3.1 can describe the complete response as
`text/event-stream`, but cannot standardly describe each event:

```yaml
responses:
  "200":
    description: Event stream
    content:
      text/event-stream:
        schema:
          type: string
```

OpenAPI 3.2 adds standard sequential-media support through `itemSchema`:

```yaml
responses:
  "200":
    description: Event stream
    content:
      text/event-stream:
        itemSchema:
          $ref: "#/components/schemas/Event"

components:
  schemas:
    Event:
      type: object
      required: [data]
      properties:
        event:
          type: string
        data:
          type: string
        id:
          type: string
        retry:
          type: integer
          minimum: 0
```

If an SSE `data` field contains JSON, OpenAPI 3.2 can additionally describe it
with `contentMediaType: application/json` and `contentSchema`.

## Bob follow-up

After both Mizu steps are released:

1. Generate complete OpenAPI path operations from Bob's service-owned CUE
   contract instead of extracting only component schemas.
2. Parse the generated document once during service initialization.
3. Replace the three raw registry route registrations with `mizuoai` raw
   registration.
4. Document both upload `404` responses in addition to `400`, `401`, `409`,
   `413`, and `500` because missing scopes or skills currently map to `404`.
5. Add Bob integration tests covering the rendered registry operations and the
   actual multipart/download runtime behavior.

## Recommended delivery sequence

Keep the work reviewable rather than combining it into one large change:

1. Document builder and path correctness.
2. Schema components and recursive reflection.
3. Operation reflection and response model.
4. Version policy, validation, and documentation UI stability.
5. Compatibility documentation and release.
6. Raw registration and external operation import.
7. Bob CUE operation generation and registry migration.
