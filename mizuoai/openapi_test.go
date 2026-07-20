package mizuoai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/humbornjo/mizu"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

type reflectedNode struct {
	Name     string           `json:"name" minLength:"1"`
	Children []*reflectedNode `json:"children,omitempty"`
}

type reflectedRequest struct {
	Path struct {
		Id string `json:"id" desc:"Node identifier"`
	} `json:"path"`
	Query struct {
		Limit int `json:"limit,omitempty" minimum:"1"`
	} `json:"query"`
}

type reflectedResponse struct {
	Node reflectedNode `json:"node"`
}

type reflectedLimit int

type reflectedParameterRequest struct {
	Query struct {
		Limit reflectedLimit `json:"limit,omitempty" minimum:"1"`
	} `json:"query"`
}

type schemaOverrideResponse struct {
	Value string `json:"value"`
}

type urlEncodedRequest struct {
	Form struct {
		Name  string   `json:"name"`
		Count int      `json:"count"`
		Tags  []string `json:"tags"`
	} `json:"form" contentType:"application/x-www-form-urlencoded"`
}

type parameterMetadataRequest struct {
	Query struct {
		Limit int `json:"limit" required:"true" deprecated:"true" default:"10" example:"5" style:"form" explode:"false" allowReserved:"true"`
	} `json:"query"`
}

type explicitBodyRequest struct {
	Body struct {
		Value string `json:"value"`
	} `json:"body"`
}

type multipartRequest struct {
	Form struct {
		Name    string `json:"name"`
		Package []byte `json:"package" contentType:"application/gzip"`
	} `json:"form"`
}

type binaryBodyRequest struct {
	Body []byte `json:"body"`
}

type textBodyRequest struct {
	Header struct {
		RequestId string `json:"X-Request-Id"`
	} `json:"header"`
	Body string `json:"body"`
}

type pointerQueryRequest struct {
	Query *struct {
		Limit  *int  `json:"limit,omitempty"`
		Values []int `json:"values,omitempty"`
	} `json:"query"`
}

type capabilityWriter struct {
	header   http.Header
	flushed  bool
	hijacked bool
}

func (w *capabilityWriter) Header() http.Header {
	return w.header
}

func (w *capabilityWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *capabilityWriter) WriteHeader(int) {}

func (w *capabilityWriter) Flush() {
	w.flushed = true
}

func (w *capabilityWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	return nil, nil, nil
}

type componentCollision struct {
	Value string `json:"value"`
}

type scalarAlias string

type mutualA struct {
	B *mutualB `json:"b,omitempty"`
}

type mutualB struct {
	A *mutualA `json:"a,omitempty"`
}

type EmbeddedFields struct {
	Embedded string `json:"embedded"`
}

type OptionalEmbeddedFields struct {
	OptionalEmbedded string `json:"optionalEmbedded"`
}

type reflectionCoverage struct {
	EmbeddedFields
	*OptionalEmbeddedFields
	Bool           bool            `json:"bool"`
	Int8           int8            `json:"int8"`
	Uint64         uint64          `json:"uint64"`
	Float32        float32         `json:"float32"`
	Alias          scalarAlias     `json:"alias"`
	Pointer        *string         `json:"pointer"`
	Array          [2]int          `json:"array"`
	Slice          []string        `json:"slice"`
	Map            map[string]int  `json:"map"`
	Time           time.Time       `json:"time"`
	Raw            json.RawMessage `json:"raw"`
	Bytes          []byte          `json:"bytes"`
	Mutual         mutualA         `json:"mutual"`
	Optional       string          `json:"optional,omitempty"`
	ForcedOptional string          `json:"forcedOptional" required:"false"`
	ForcedRequired *string         `json:"forcedRequired,omitempty" required:"true"`
	Ignored        string          `json:"-"`
}

const fullOpenApi32Document = `openapi: 3.2.0
$self: https://example.com/openapi.yaml
info:
  title: full
  version: v1
servers:
  - name: production
    url: https://api.example.com
tags:
  - name: registry
    summary: Registry operations
    kind: nav
  - name: events
    parent: registry
    kind: badge
paths:
  /search:
    query:
      responses:
        "200": {description: search result}
  /events:
    get:
      operationId: events
      tags: [events]
      security:
        - deviceAuth: []
      responses:
        "200":
          summary: Event stream
          description: streamed events
          content:
            text/event-stream:
              description: Event sequence
              itemSchema:
                $ref: "#/components/schemas/Event"
              prefixEncoding:
                - contentType: application/json
              itemEncoding:
                contentType: application/json
                prefixEncoding:
                  - contentType: text/plain
                itemEncoding:
                  contentType: application/json
                headers:
                  X-Event-Id:
                    $ref: "#/components/headers/EventId"
components:
  schemas:
    Event:
      type: object
      discriminator:
        propertyName: kind
        defaultMapping: "#/components/schemas/Event"
      xml:
        nodeType: element
      properties:
        kind: {type: string}
        data: {type: string}
  headers:
    EventId:
      schema: {type: string}
  examples:
    Data:
      dataValue: {kind: update, data: ready}
    Wire:
      serializedValue: '{"kind":"update","data":"ready"}'
  mediaTypes:
    EventStream:
      description: Reusable event stream
      itemSchema:
        $ref: "#/components/schemas/Event"
  securitySchemes:
    deviceAuth:
      type: oauth2
      deprecated: true
      oauth2MetadataUrl: https://auth.example.com/.well-known/oauth-authorization-server
      flows:
        deviceAuthorization:
          deviceAuthorizationUrl: https://auth.example.com/device
          tokenUrl: https://auth.example.com/token
          scopes: {}
`

const transitiveOpenApi32Document = `openapi: 3.2.0
info: {title: transitive, version: v1}
paths:
  /all:
    get:
      operationId: allComponents
      parameters:
        - $ref: "#/components/parameters/Filter"
      requestBody:
        $ref: "#/components/requestBodies/Input"
      callbacks:
        changed:
          $ref: "#/components/callbacks/Changed"
      security:
        - bearerAuth: []
      responses:
        "200":
          $ref: "#/components/responses/Success"
components:
  schemas:
    Root:
      type: object
      properties:
        child: {$ref: "#/components/schemas/Child"}
    Child: {type: string}
    Unused: {type: boolean}
  responses:
    Success:
      description: success
      headers:
        X-Trace:
          $ref: "#/components/headers/Trace"
      content:
        application/json:
          $ref: "#/components/mediaTypes/Json"
      links:
        next:
          $ref: "#/components/links/Next"
  parameters:
    Filter:
      name: filter
      in: query
      schema: {$ref: "#/components/schemas/Child"}
      examples:
        sample:
          $ref: "#/components/examples/Sample"
  examples:
    Sample:
      dataValue: filtered
  requestBodies:
    Input:
      content:
        application/json:
          schema: {$ref: "#/components/schemas/Root"}
  headers:
    Trace:
      schema: {type: string}
  securitySchemes:
    bearerAuth: {type: http, scheme: bearer}
  links:
    Next:
      operationId: allComponents
      server:
        name: linked
        url: https://api.example.com
  callbacks:
    Changed:
      '{$request.body#/callbackUrl}':
        $ref: "#/components/pathItems/ChangedPath"
  pathItems:
    ChangedPath:
      post:
        requestBody:
          $ref: "#/components/requestBodies/Input"
        responses:
          "204": {description: accepted}
  mediaTypes:
    Json:
      description: Reusable JSON
      schema: {$ref: "#/components/schemas/Root"}
`

func TestMizuoai_RenderOpenApi32WithComponents(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiInfoVersion("2026.7")))
	group := srv.Group("/api")
	Get(group, "/nodes/{id}", func(Tx[reflectedResponse], Rx[reflectedRequest]) {},
		WithOperationOperationId("getNode"))
	Post(group, "/nodes/{id}", func(Tx[reflectedResponse], Rx[reflectedRequest]) {},
		WithOperationOperationId("updateNode"))

	model := retrieveDocument(t, srv)
	require.Equal(t, "3.2.0", model.Version)
	item, ok := model.Paths.PathItems.Get("/api/nodes/{id}")
	require.True(t, ok)
	require.NotNil(t, item.Get)
	require.NotNil(t, item.Post)
	require.Len(t, item.Get.Parameters, 2)
	require.Equal(t, "id", item.Get.Parameters[0].Name)
	require.True(t, *item.Get.Parameters[0].Required)
	_, ok = model.Components.Schemas.Get("reflectedNode")
	require.True(t, ok)
	_, ok = model.Components.Schemas.Get("reflectedResponse")
	require.True(t, ok)
	node, _ := model.Components.Schemas.Get("reflectedNode")
	children, _ := node.Schema().Properties.Get("children")
	child := children.Schema().Items.A.Schema().AnyOf[0]
	require.Equal(t, "#/components/schemas/reflectedNode", child.GetReference())
}

func TestMizuoai_NestedGroupsAndMixedHandlers(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	group := srv.Group("/api").Group("/v1")
	Get(group, "/items", func(Tx[string], Rx[struct{}]) {})
	PostRaw(group, "/items", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}, WithOperation(&v3.Operation{
		Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"201": {Description: "created"},
		})},
	}))

	model := retrieveDocument(t, srv)
	item, ok := model.Paths.PathItems.Get("/api/v1/items")
	require.True(t, ok)
	require.NotNil(t, item.Get)
	require.NotNil(t, item.Post)
}

func TestMizuoai_RawHandlerUsesMizuMiddleware(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	group := srv.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Mizu-Middleware", "applied")
			next.ServeHTTP(w, r)
		})
	}).Group("/api")
	GetRaw(group, "/raw", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}, WithOperation(&v3.Operation{
		Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"204": {Description: "done"},
		})},
	}))

	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/raw", nil))
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, "applied", recorder.Header().Get("X-Mizu-Middleware"))
}

func TestMizuoai_RawHandlerPreservesWriterCapabilities(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	GetRaw(srv, "/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		_, _, _ = w.(http.Hijacker).Hijack()
	}, WithOperation(&v3.Operation{
		Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"101": {Description: "switching protocols"},
		})},
	}))

	writer := &capabilityWriter{header: make(http.Header)}
	srv.Handler().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	require.True(t, writer.flushed)
	require.True(t, writer.hijacked)
}

func TestMizuoai_RawMethodHelpers(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	operation := func() *v3.Operation {
		return &v3.Operation{Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"204": {Description: "done"},
		})}}
	}
	handler := func(http.ResponseWriter, *http.Request) {}
	GetRaw(srv, "/methods", handler, WithOperation(operation()))
	PostRaw(srv, "/methods", handler, WithOperation(operation()))
	PutRaw(srv, "/methods", handler, WithOperation(operation()))
	DeleteRaw(srv, "/methods", handler, WithOperation(operation()))
	PatchRaw(srv, "/methods", handler, WithOperation(operation()))
	HeadRaw(srv, "/methods", handler, WithOperation(operation()))
	OptionsRaw(srv, "/methods", handler, WithOperation(operation()))
	TraceRaw(srv, "/methods", handler, WithOperation(operation()))
	ConnectRaw(srv, "/methods", handler, WithOperation(operation()))

	model := retrieveDocument(t, srv)
	item, ok := model.Paths.PathItems.Get("/methods")
	require.True(t, ok)
	require.NotNil(t, item.Get)
	require.NotNil(t, item.Post)
	require.NotNil(t, item.Put)
	require.NotNil(t, item.Delete)
	require.NotNil(t, item.Patch)
	require.NotNil(t, item.Head)
	require.NotNil(t, item.Options)
	require.NotNil(t, item.Trace)
	config := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	rendered, err := config.render(false)
	require.NoError(t, err)
	require.Contains(t, string(rendered), "additionalOperations:")
	require.Contains(t, string(rendered), "connect:")
}

func TestMizuoai_SelectsOpenApi32AdditionalOperation(t *testing.T) {
	document := MustParseOpenAPI([]byte(`openapi: 3.2.0
info: {title: connect, version: v1}
paths:
  /tunnel:
    additionalOperations:
      connect:
        operationId: tunnel
        responses:
          "200": {description: connected}
`))
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	ConnectRaw(srv, "/tunnel", func(http.ResponseWriter, *http.Request) {},
		WithOpenApiOperation(document, "tunnel"))
	config := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	rendered, err := config.render(false)
	require.NoError(t, err)
	require.Contains(t, string(rendered), "additionalOperations:")
	require.Contains(t, string(rendered), "operationId: tunnel")
}

func TestMizuoai_SelectsReferencedPathItemOperation(t *testing.T) {
	document := MustParseOpenAPI([]byte(`openapi: 3.1.0
info: {title: referenced path, version: v1}
paths:
  /items/{id}:
    $ref: "#/components/pathItems/ItemPath"
components:
  parameters:
    ItemId:
      name: id
      in: path
      required: true
      schema: {type: string}
  pathItems:
    ItemPath:
      parameters:
        - $ref: "#/components/parameters/ItemId"
      get:
        operationId: getReferencedItem
        responses:
          "200": {description: item}
`))
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	GetRaw(srv, "/items/{id}", func(http.ResponseWriter, *http.Request) {},
		WithOpenApiOperation(document, "getReferencedItem"))

	model := retrieveDocument(t, srv)
	item, ok := model.Paths.PathItems.Get("/items/{id}")
	require.True(t, ok)
	require.NotNil(t, item.Get)
	require.Equal(t, "getReferencedItem", item.Get.OperationId)
	require.Len(t, item.Parameters, 1)
	require.True(t, hasMapValue(model.Components.PathItems, "ItemPath"))
	require.True(t, hasMapValue(model.Components.Parameters, "ItemId"))
}

func TestMizuoai_RejectsDuplicateRoutesBeforeRuntimeRegistration(t *testing.T) {
	operation := func(description string) *v3.Operation {
		return &v3.Operation{Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"200": {Description: description},
		})}}
	}

	t.Run("registered operation", func(t *testing.T) {
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		GetRaw(srv, "/duplicate", func(http.ResponseWriter, *http.Request) {}, WithOperation(operation("first")))
		require.PanicsWithError(t,
			"register raw OpenAPI operation: duplicate OpenAPI operation GET /duplicate",
			func() {
				GetRaw(srv, "/duplicate", func(http.ResponseWriter, *http.Request) {}, WithOperation(operation("second")))
			},
		)
	})

	t.Run("preloaded operation", func(t *testing.T) {
		baseDocument := []byte(`openapi: 3.1.0
info: {title: base, version: v1}
paths:
  /base:
    get:
      operationId: base
      responses:
        "200": {description: base}
`)
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test", WithOaiPreLoad(baseDocument)))
		require.PanicsWithError(t,
			"register raw OpenAPI operation: duplicate OpenAPI operation GET /base",
			func() {
				GetRaw(srv, "/base", func(http.ResponseWriter, *http.Request) {}, WithOperation(operation("runtime")))
			},
		)
		recorder := httptest.NewRecorder()
		srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/base", nil))
		require.Equal(t, http.StatusNotFound, recorder.Code)
	})
}

func TestMizuoai_FailedTypedRegistrationDoesNotLeakComponents(t *testing.T) {
	type rejectedResponse struct {
		Value string `json:"value"`
	}

	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	Get(srv, "/value", func(Tx[string], Rx[struct{}]) {})
	require.Panics(t, func() {
		Get(srv, "/value", func(Tx[rejectedResponse], Rx[struct{}]) {})
	})

	model := retrieveDocument(t, srv)
	_, ok := model.Components.Schemas.Get("rejectedResponse")
	require.False(t, ok)
}

func TestMizuoai_PathItemRegistration(t *testing.T) {
	operation := func(operationId string) *v3.Operation {
		return &v3.Operation{
			OperationId: operationId,
			Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
				"200": {Description: "ok"},
			})},
		}
	}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	Path(srv, "/documented", WithPathItem(&v3.PathItem{Get: operation("documentedGet")}))
	Path(srv, "/documented", WithPathItem(&v3.PathItem{Post: operation("documentedPost")}))
	model := retrieveDocument(t, srv)
	item, ok := model.Paths.PathItems.Get("/documented")
	require.True(t, ok)
	require.NotNil(t, item.Get)
	require.NotNil(t, item.Post)

	require.PanicsWithError(t,
		"register raw OpenAPI operation: duplicate OpenAPI operation GET /documented",
		func() {
			GetRaw(srv, "/documented", func(http.ResponseWriter, *http.Request) {},
				WithOperation(operation("runtime")))
		},
	)
}

func TestMizuoai_FullOpenApi32InputPreservation(t *testing.T) {
	document, err := ParseOpenAPI([]byte(fullOpenApi32Document))
	require.NoError(t, err)
	require.Equal(t, "3.2.0", document.Version())

	t.Run("preload", func(t *testing.T) {
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test", WithOaiPreLoad([]byte(fullOpenApi32Document))))
		config := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
		rendered, err := config.render(false)
		require.NoError(t, err)
		for _, field := range []string{
			"$self:", "description: Event sequence", "prefixEncoding:",
			"itemEncoding:", "deviceAuthorization:", "deviceAuthorizationUrl:",
			"dataValue:", "serializedValue:", "defaultMapping:", "nodeType:",
			"mediaTypes:",
		} {
			require.Contains(t, string(rendered), field)
		}
	})

	t.Run("selected operation", func(t *testing.T) {
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		GetRaw(srv, "/events", func(http.ResponseWriter, *http.Request) {},
			WithOpenApiOperation(document, "events"))
		config := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
		rendered, err := config.render(false)
		require.NoError(t, err)
		for _, field := range []string{
			"description: Event sequence", "prefixEncoding:", "itemEncoding:",
			"deviceAuthorization:", "deviceAuthorizationUrl:", "defaultMapping:",
			"nodeType:",
		} {
			require.Contains(t, string(rendered), field)
		}
		require.NotContains(t, string(rendered), "$self:")
	})
}

func TestMizuoai_OpenApi32DocumentOptions(t *testing.T) {
	components := newComponents()
	components.MediaTypes.Set("Sequence", &v3.MediaType{
		ItemSchema: base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}),
	})
	webhook := &v3.PathItem{Post: &v3.Operation{
		Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"204": {Description: "accepted"},
		})},
	}}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test",
		WithOaiSelf("https://example.com/openapi.yaml"),
		WithOaiJsonSchemaDialect("https://json-schema.org/draft/2020-12/schema"),
		WithOaiSummary("Complete document options"),
		WithOaiInfoVersion("v1"),
		WithOaiServerObject(&v3.Server{Name: "production", URL: "https://api.example.com"}),
		WithOaiTagObject(&base.Tag{Name: "root", Summary: "Root", Kind: "nav"}),
		WithOaiTagObject(&base.Tag{Name: "child", Parent: "root", Kind: "badge"}),
		WithOaiSecurityScheme("bearerAuth", &v3.SecurityScheme{Type: "http", Scheme: "bearer"}),
		WithOaiSecurity(map[string][]string{"bearerAuth": {}}),
		WithOaiWebhook("changed", webhook),
		WithOaiComponents(components),
	))

	model := retrieveDocument(t, srv)
	require.Equal(t, "https://example.com/openapi.yaml", model.Self)
	require.Equal(t, "https://json-schema.org/draft/2020-12/schema", model.JsonSchemaDialect)
	require.Equal(t, "Complete document options", model.Info.Summary)
	require.Equal(t, "production", model.Servers[0].Name)
	require.Len(t, model.Tags, 2)
	require.True(t, hasMapValue(model.Components.MediaTypes, "Sequence"))
	require.True(t, hasMapValue(model.Webhooks, "changed"))
}

func TestMizuoai_ImportsTransitiveComponentClosure(t *testing.T) {
	document, err := ParseOpenAPI([]byte(transitiveOpenApi32Document))
	require.NoError(t, err)
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	GetRaw(srv, "/all", func(http.ResponseWriter, *http.Request) {},
		WithOpenApiOperation(document, "allComponents"))

	model := retrieveDocument(t, srv)
	components := model.Components
	for _, check := range []struct {
		name string
		has  bool
	}{
		{"schemas.Root", hasMapValue(components.Schemas, "Root")},
		{"schemas.Child", hasMapValue(components.Schemas, "Child")},
		{"responses.Success", hasMapValue(components.Responses, "Success")},
		{"parameters.Filter", hasMapValue(components.Parameters, "Filter")},
		{"examples.Sample", hasMapValue(components.Examples, "Sample")},
		{"requestBodies.Input", hasMapValue(components.RequestBodies, "Input")},
		{"headers.Trace", hasMapValue(components.Headers, "Trace")},
		{"securitySchemes.bearerAuth", hasMapValue(components.SecuritySchemes, "bearerAuth")},
		{"links.Next", hasMapValue(components.Links, "Next")},
		{"callbacks.Changed", hasMapValue(components.Callbacks, "Changed")},
		{"pathItems.ChangedPath", hasMapValue(components.PathItems, "ChangedPath")},
		{"mediaTypes.Json", hasMapValue(components.MediaTypes, "Json")},
	} {
		require.True(t, check.has, check.name)
	}
	require.False(t, hasMapValue(components.Schemas, "Unused"))
	config := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	rendered, err := config.render(false)
	require.NoError(t, err)
	require.Contains(t, string(rendered), "description: Reusable JSON")
}

func hasMapValue[T any](values *orderedmap.Map[string, T], name string) bool {
	if values == nil {
		return false
	}
	_, ok := values.Get(name)
	return ok
}

func TestMizuoai_RejectsRawOpenApi32ComponentCollisions(t *testing.T) {
	document := func(path, operationId, description string) *OpenApiDocument {
		return MustParseOpenAPI([]byte(fmt.Sprintf(`openapi: 3.2.0
info: {title: collision, version: v1}
paths:
  %s:
    get:
      operationId: %s
      responses:
        "200":
          description: ok
          content:
            text/event-stream:
              $ref: "#/components/mediaTypes/Stream"
components:
  mediaTypes:
    Stream:
      description: %s
      itemSchema: {type: string}
`, path, operationId, description)))
	}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	GetRaw(srv, "/one", func(http.ResponseWriter, *http.Request) {},
		WithOpenApiOperation(document("/one", "one", "first"), "one"))
	require.PanicsWithError(t,
		"register raw OpenAPI operation: incompatible OpenAPI component collision at mediaTypes/Stream",
		func() {
			GetRaw(srv, "/two", func(http.ResponseWriter, *http.Request) {},
				WithOpenApiOperation(document("/two", "two", "second"), "two"))
		},
	)
}

func TestMizuoai_OpenApi31InputCompatibility(t *testing.T) {
	input := []byte(`openapi: 3.1.0
info:
  title: generated
  version: v1
paths:
  /legacy:
    get:
      operationId: legacy
      responses:
        "200":
          description: legacy response
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Legacy"
components:
  schemas:
    Legacy:
      type: object
      required: [value]
      properties:
        value:
          type: string
`)
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiPreLoad(input)))
	model := retrieveDocument(t, srv)
	require.Equal(t, "3.2.0", model.Version)
	item, ok := model.Paths.PathItems.Get("/legacy")
	require.True(t, ok)
	require.Equal(t, "legacy", item.Get.OperationId)
	_, ok = model.Components.Schemas.Get("Legacy")
	require.True(t, ok)
}

func TestMizuoai_OpenApi302InputCompatibility(t *testing.T) {
	input := []byte(`openapi: 3.0.2
info:
  title: generated
  version: v1
paths:
  /legacy:
    get:
      operationId: legacy302
      responses:
        "200":
          description: legacy response
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Legacy302"
components:
  schemas:
    Legacy302:
      type: object
      required: [value]
      properties:
        value:
          type: string
          nullable: true
        count:
          type: integer
          minimum: 1
          exclusiveMinimum: true
`)
	document, err := ParseOpenAPI(input)
	require.NoError(t, err)
	require.Equal(t, "3.0.2", document.Version())

	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiPreLoad(input)))
	model := retrieveDocument(t, srv)
	require.Equal(t, "3.2.0", model.Version)
	item, ok := model.Paths.PathItems.Get("/legacy")
	require.True(t, ok)
	require.Equal(t, "legacy302", item.Get.OperationId)
	_, ok = model.Components.Schemas.Get("Legacy302")
	require.True(t, ok)
	legacy, _ := model.Components.Schemas.Get("Legacy302")
	value, _ := legacy.Schema().Properties.Get("value")
	require.Contains(t, value.Schema().Type, "null")
	require.Nil(t, value.Schema().Nullable)
	count, _ := legacy.Schema().Properties.Get("count")
	require.NotNil(t, count.Schema().ExclusiveMinimum)
	require.True(t, count.Schema().ExclusiveMinimum.IsB())
	require.Equal(t, float64(1), count.Schema().ExclusiveMinimum.B)
}

func TestMizuoai_RawOpenApi31Operation(t *testing.T) {
	source := []byte(`openapi: 3.1.0
info:
  title: registry
  version: v1
tags:
  - name: registry
    description: Registry operations
  - name: skill
    description: Skill operations
paths:
  /api/scopes/{scope}/skills:
    parameters:
      - $ref: "#/components/parameters/Scope"
    post:
      operationId: registerSkill
      tags: [registry, skill]
      summary: Register a skill
      description: Upload a compressed skill package.
      deprecated: false
      security:
        - bearerAuth: []
      x-registry-operation: true
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              $ref: "#/components/schemas/Upload"
      responses:
        "201":
          description: registered skill
          headers:
            Location:
              required: true
              schema:
                type: string
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Skill"
        "400":
          description: invalid upload
components:
  parameters:
    Scope:
      name: scope
      in: path
      required: true
      schema:
        type: string
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
  schemas:
    Upload:
      type: object
      required: [name, package]
      properties:
        name:
          type: string
        package:
          contentMediaType: application/gzip
    Skill:
      type: object
      required: [name]
      properties:
        name:
          type: string
`)
	document, err := ParseOpenAPI(source)
	require.NoError(t, err)
	require.Equal(t, "3.1.0", document.Version())

	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	var received []byte
	PostRaw(srv.Group("/api"), "/scopes/{scope}/skills", func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	},
		WithResponse(http.StatusUnprocessableEntity, &v3.Response{Description: "unprocessable upload"}),
		WithOpenApiOperation(document, "registerSkill"),
	)

	handler := srv.Handler()
	payload := []byte("raw multipart bytes\x00\xff")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/scopes/main/skills", bytes.NewReader(payload))
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Equal(t, payload, received)

	model := retrieveDocumentFromHandler(t, handler)
	item, ok := model.Paths.PathItems.Get("/api/scopes/{scope}/skills")
	require.True(t, ok)
	require.Equal(t, "registerSkill", item.Post.OperationId)
	require.Equal(t, []string{"registry", "skill"}, item.Post.Tags)
	require.Equal(t, "Register a skill", item.Post.Summary)
	require.Len(t, item.Post.Security, 1)
	_, ok = item.Post.Responses.Codes.Get("422")
	require.True(t, ok)
	require.NotNil(t, item.Post.Extensions)
	require.Len(t, item.Parameters, 1)
	_, ok = model.Components.Schemas.Get("Upload")
	require.True(t, ok)
	_, ok = model.Components.Schemas.Get("Skill")
	require.True(t, ok)
	_, ok = model.Components.SecuritySchemes.Get("bearerAuth")
	require.True(t, ok)
	_, ok = model.Components.Parameters.Get("Scope")
	require.True(t, ok)
	require.Len(t, model.Tags, 2)
	require.Equal(t, "Registry operations", model.Tags[0].Description)
}

func TestMizuoai_RawSsePreservesFlusher(t *testing.T) {
	components := newComponents()
	components.Schemas.Set("Event", base.CreateSchemaProxy(&base.Schema{
		Type:     []string{"object"},
		Required: []string{"data"},
		Properties: orderedmap.ToOrderedMap(map[string]*base.SchemaProxy{
			"data": base.CreateSchemaProxy(&base.Schema{
				Type:          []string{"string"},
				ContentSchema: base.CreateSchemaProxy(&base.Schema{Type: []string{"object"}}),
			}),
		}),
	}))
	operation := &v3.Operation{
		OperationId: "events",
		Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"200": {
				Description: "event stream",
				Content: orderedmap.ToOrderedMap(map[string]*v3.MediaType{
					"text/event-stream": {
						ItemSchema: base.CreateSchemaProxyRef("#/components/schemas/Event"),
					},
				}),
			},
		})},
	}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiComponents(components)))
	GetRaw(srv, "/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: first\n\n"))
		w.(http.Flusher).Flush()
	}, WithOperation(operation))

	handler := srv.Handler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/events", nil))
	require.True(t, recorder.Flushed)
	require.Equal(t, "data: first\n\n", recorder.Body.String())

	model := retrieveDocumentFromHandler(t, handler)
	item, _ := model.Paths.PathItems.Get("/events")
	mediaType, _ := item.Get.Responses.Codes.Get("200")
	stream, _ := mediaType.Content.Get("text/event-stream")
	require.Equal(t, "#/components/schemas/Event", stream.ItemSchema.GetReference())
	event, _ := model.Components.Schemas.Get("Event")
	data, _ := event.Schema().Properties.Get("data")
	require.NotNil(t, data.Schema().ContentSchema)
}

func TestMizuoai_RawBinaryDownload(t *testing.T) {
	operation := &v3.Operation{
		OperationId: "download",
		Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"200": {
				Description: "compressed package",
				Headers: orderedmap.ToOrderedMap(map[string]*v3.Header{
					"Content-Disposition": {
						Required: true,
						Schema:   base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}),
					},
				}),
				Content: orderedmap.ToOrderedMap(map[string]*v3.MediaType{
					"application/gzip": {},
				}),
			},
		})},
	}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	GetRaw(srv, "/package", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="package.tar.gz"`)
		_, _ = w.Write([]byte{0x1f, 0x8b, 0x08})
	}, WithOperation(operation))

	handler := srv.Handler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/package", nil))
	require.Equal(t, "application/gzip", recorder.Header().Get("Content-Type"))
	require.Equal(t, []byte{0x1f, 0x8b, 0x08}, recorder.Body.Bytes())

	model := retrieveDocumentFromHandler(t, handler)
	item, _ := model.Paths.PathItems.Get("/package")
	response, _ := item.Get.Responses.Codes.Get("200")
	_, ok := response.Content.Get("application/gzip")
	require.True(t, ok)
	_, ok = response.Headers.Get("Content-Disposition")
	require.True(t, ok)
}

func TestMizuoai_DeterministicRenderingAndVersionSelection(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiVersion("3.1.1")))
	Get(srv, "/nodes/{id}", func(Tx[reflectedResponse], Rx[reflectedRequest]) {})
	config := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	first, err := config.render(false)
	require.NoError(t, err)
	second, err := config.render(false)
	require.NoError(t, err)
	require.Equal(t, first, second)
	document, err := ParseOpenAPI(first)
	require.NoError(t, err)
	require.Equal(t, "3.1.1", document.Version())
}

func TestMizuoai_DeterministicYamlJsonEquivalence(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	Get(srv, "/nodes/{id}", func(Tx[reflectedResponse], Rx[reflectedRequest]) {})
	config := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	yamlFirst, err := config.render(false)
	require.NoError(t, err)
	yamlSecond, err := config.render(false)
	require.NoError(t, err)
	jsonFirst, err := config.render(true)
	require.NoError(t, err)
	jsonSecond, err := config.render(true)
	require.NoError(t, err)
	require.Equal(t, yamlFirst, yamlSecond)
	require.Equal(t, jsonFirst, jsonSecond)
	var yamlValue, jsonValue any
	require.NoError(t, yaml.Unmarshal(yamlFirst, &yamlValue))
	require.NoError(t, yaml.Unmarshal(jsonFirst, &jsonValue))
	require.True(t, reflect.DeepEqual(yamlValue, jsonValue))
}

func TestMizuoai_DocumentationUiSmoke(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiDocumentation()))
	Get(srv, "/value", func(Tx[string], Rx[struct{}]) {})
	handler := srv.Handler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "@stoplight/elements@9.0.24")
	require.Contains(t, recorder.Body.String(), "<elements-api")
	require.Contains(t, recorder.Body.String(), "openapi: 3.2.0")

	jsonServer := mizu.NewServer("json")
	require.NoError(t, Initialize(jsonServer, "test", WithOaiRenderJson()))
	Get(jsonServer, "/value", func(Tx[string], Rx[struct{}]) {})
	jsonRecorder := httptest.NewRecorder()
	jsonServer.Handler().ServeHTTP(jsonRecorder, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	require.Equal(t, http.StatusOK, jsonRecorder.Code)
	require.Equal(t, "application/json", jsonRecorder.Header().Get("Content-Type"))
	require.Equal(t, "3.2.0", retrieveVersion(t, jsonRecorder.Body.Bytes()))
}

func retrieveVersion(t *testing.T, data []byte) string {
	t.Helper()
	var document struct {
		OpenApi string `json:"openapi"`
	}
	require.NoError(t, json.Unmarshal(data, &document))
	return document.OpenApi
}

func TestMizuoai_OpenApi31RejectsOpenApi32Fields(t *testing.T) {
	operation := &v3.Operation{
		Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"200": {
				Description: "events",
				Content: orderedmap.ToOrderedMap(map[string]*v3.MediaType{
					"text/event-stream": {
						ItemSchema: base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}),
					},
				}),
			},
		})},
	}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiVersion("3.1.1")))
	var panicValue any
	func() {
		defer func() { panicValue = recover() }()
		GetRaw(srv, "/events", func(http.ResponseWriter, *http.Request) {}, WithOperation(operation))
	}()
	err, ok := panicValue.(error)
	require.True(t, ok)
	require.ErrorContains(t, err, "itemSchema")
	require.ErrorContains(t, err, "3.1.1 specification")

	_, err = ParseOpenAPI([]byte(`openapi: 3.1.0
info: {title: invalid, version: v1}
paths:
  /events:
    get:
      responses:
        "200":
          description: events
          content:
            text/event-stream:
              itemSchema: {type: string}
`))
	require.ErrorContains(t, err, "itemSchema")
	require.ErrorContains(t, err, "3.1.0 specification")
}

func TestMizuoai_InitializationValidation(t *testing.T) {
	t.Run("preload info completed by initialization", func(t *testing.T) {
		srv := mizu.NewServer("test")
		err := Initialize(srv, "completed title", WithOaiPreLoad([]byte(`openapi: 3.1.0
info: {}
paths: {}
`)))
		require.NoError(t, err)
		model := retrieveDocument(t, srv)
		require.Equal(t, "completed title", model.Info.Title)
		require.Equal(t, "1.0.0", model.Info.Version)
	})

	t.Run("3.2 field in 3.1 output", func(t *testing.T) {
		srv := mizu.NewServer("test")
		err := Initialize(srv, "test", WithOaiVersion("3.1.1"), WithOaiSelf("https://example.com/openapi.yaml"))
		require.ErrorContains(t, err, "$self")
		require.ErrorContains(t, err, "3.1.1 specification")
	})

	t.Run("undefined global security scheme", func(t *testing.T) {
		srv := mizu.NewServer("test")
		err := Initialize(srv, "test", WithOaiSecurity(map[string][]string{"missing": {}}))
		require.EqualError(t, err,
			`initialize OpenAPI document: OpenAPI security requirement "missing" on document has no security scheme`)
	})

	t.Run("undefined tag parent", func(t *testing.T) {
		srv := mizu.NewServer("test")
		err := Initialize(srv, "test", WithOaiTagObject(&base.Tag{Name: "child", Parent: "missing"}))
		require.EqualError(t, err,
			`initialize OpenAPI document: OpenAPI tag "child" has undefined parent "missing"`)
	})

	t.Run("conflicting duplicate tag", func(t *testing.T) {
		srv := mizu.NewServer("test")
		err := Initialize(srv, "test",
			WithOaiTag("duplicate", "first", nil),
			WithOaiTag("duplicate", "second", nil),
		)
		require.EqualError(t, err, "incompatible OpenAPI tag collision at duplicate")
	})
}

func TestMizuoai_SchemaNameAndParameterDecoration(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test",
		WithOaiSchemaName[schemaOverrideResponse]("RenamedResponse"),
	))
	Get(srv, "/values", func(Tx[schemaOverrideResponse], Rx[reflectedParameterRequest]) {})

	model := retrieveDocument(t, srv)
	_, ok := model.Components.Schemas.Get("RenamedResponse")
	require.True(t, ok)
	_, ok = model.Components.Schemas.Get("schemaOverrideResponse")
	require.False(t, ok)
	limit, ok := model.Components.Schemas.Get("reflectedLimit")
	require.True(t, ok)
	require.Nil(t, limit.Schema().Minimum)

	item, _ := model.Paths.PathItems.Get("/values")
	require.Len(t, item.Get.Parameters, 1)
	parameterSchema := item.Get.Parameters[0].Schema.Schema()
	require.Equal(t, float64(1), *parameterSchema.Minimum)
	require.Len(t, parameterSchema.AllOf, 1)
	require.Equal(t, "#/components/schemas/reflectedLimit", parameterSchema.AllOf[0].GetReference())
	response, _ := item.Get.Responses.Codes.Get("200")
	content, _ := response.Content.Get("application/json")
	require.Equal(t, "#/components/schemas/RenamedResponse", content.Schema.GetReference())
}

func TestMizuoai_ParameterMetadata(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	Get(srv, "/metadata", func(Tx[string], Rx[parameterMetadataRequest]) {})

	model := retrieveDocument(t, srv)
	item, _ := model.Paths.PathItems.Get("/metadata")
	require.Len(t, item.Get.Parameters, 1)
	parameter := item.Get.Parameters[0]
	require.Equal(t, "form", parameter.Style)
	require.NotNil(t, parameter.Explode)
	require.False(t, *parameter.Explode)
	require.True(t, parameter.AllowReserved)
	require.True(t, parameter.Deprecated)
	require.True(t, *parameter.Required)
	var example int
	require.NoError(t, parameter.Example.Decode(&example))
	require.Equal(t, 5, example)
	var defaultValue int
	require.NoError(t, parameter.Schema.Schema().Default.Decode(&defaultValue))
	require.Equal(t, 10, defaultValue)
}

func TestMizuoai_UrlEncodedForm(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	var input urlEncodedRequest
	Post(srv, "/form", func(tx Tx[string], rx Rx[urlEncodedRequest]) {
		input, _ = rx.MizuRead()
	}, WithOperationResponses(&v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
		"204": {Description: "accepted"},
	})}))

	values := url.Values{
		"name":  {"Mizu"},
		"count": {"2"},
		"tags":  {"one", "two"},
	}
	request := httptest.NewRequest(http.MethodPost, "/form", bytes.NewBufferString(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	require.Equal(t, "Mizu", input.Form.Name)
	require.Equal(t, 2, input.Form.Count)
	require.Equal(t, []string{"one", "two"}, input.Form.Tags)

	model := retrieveDocumentFromHandler(t, srv.Handler())
	item, _ := model.Paths.PathItems.Get("/form")
	_, ok := item.Post.RequestBody.Content.Get("application/x-www-form-urlencoded")
	require.True(t, ok)
	_, hasDefault := item.Post.Responses.Codes.Get("200")
	require.False(t, hasDefault)
	_, hasExplicit := item.Post.Responses.Codes.Get("204")
	require.True(t, hasExplicit)
}

func TestMizuoai_ExplicitRequestAndResponseReplacement(t *testing.T) {
	requestBody := &v3.RequestBody{
		Content: orderedmap.ToOrderedMap(map[string]*v3.MediaType{
			"text/plain": {Schema: base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}})},
		}),
	}
	responses := &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
		"204": {Description: "done"},
	})}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	Post(srv, "/explicit", func(Tx[string], Rx[explicitBodyRequest]) {},
		WithOperationRequestBody(requestBody),
		WithOperationResponses(responses),
	)

	model := retrieveDocument(t, srv)
	item, _ := model.Paths.PathItems.Get("/explicit")
	_, ok := item.Post.RequestBody.Content.Get("text/plain")
	require.True(t, ok)
	_, ok = item.Post.RequestBody.Content.Get("application/json")
	require.False(t, ok)
	_, ok = item.Post.Responses.Codes.Get("204")
	require.True(t, ok)
	_, ok = item.Post.Responses.Codes.Get("200")
	require.False(t, ok)
}

func TestMizuoai_ComposableOperationOptions(t *testing.T) {
	callback := &v3.Callback{Expression: orderedmap.ToOrderedMap(map[string]*v3.PathItem{
		"{$request.query.callback}": {
			Post: &v3.Operation{Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
				"204": {Description: "callback accepted"},
			})}},
		},
	})}
	links := map[string]*v3.Link{"self": {OperationId: "optionOperation"}}
	headers := map[string]*v3.Header{
		"Location": {Required: true, Schema: base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}})},
	}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test",
		WithOaiSecurityScheme("bearerAuth", &v3.SecurityScheme{Type: "http", Scheme: "bearer"}),
	))
	Get(srv, "/options", func(Tx[string], Rx[struct{}]) {},
		WithOperationTags("options"),
		WithOperationSummary("Operation options"),
		WithOperationDescription("Complete option coverage"),
		WithOperationExternalDocs("https://example.com/docs", "More details"),
		WithOperationOperationId("optionOperation"),
		WithOperationCallback("changed", callback),
		WithOperationDeprecated(),
		WithOperationSecurity(map[string][]string{"bearerAuth": {}}),
		WithOperationServer("https://api.example.com", "production", nil),
		WithOperationExtensions(map[string]any{"x-option": true}),
		WithResponseOverride(http.StatusCreated, links, headers),
	)

	model := retrieveDocument(t, srv)
	item, _ := model.Paths.PathItems.Get("/options")
	operation := item.Get
	require.Equal(t, []string{"options"}, operation.Tags)
	require.Equal(t, "Operation options", operation.Summary)
	require.Equal(t, "Complete option coverage", operation.Description)
	require.Equal(t, "optionOperation", operation.OperationId)
	require.True(t, *operation.Deprecated)
	require.Len(t, operation.Security, 1)
	require.Len(t, operation.Servers, 1)
	require.True(t, hasMapValue(operation.Callbacks, "changed"))
	response, ok := operation.Responses.Codes.Get("201")
	require.True(t, ok)
	require.True(t, hasMapValue(response.Headers, "Location"))
	require.True(t, hasMapValue(response.Links, "self"))
	require.NotNil(t, operation.Extensions)
}

func TestMizuoai_BinaryRequestSchemasMatchTransport(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	Post(srv, "/multipart", func(Tx[string], Rx[multipartRequest]) {})
	Post(srv, "/binary", func(Tx[string], Rx[binaryBodyRequest]) {})

	model := retrieveDocument(t, srv)
	multipartItem, _ := model.Paths.PathItems.Get("/multipart")
	multipartMedia, _ := multipartItem.Post.RequestBody.Content.Get("multipart/form-data")
	packageSchema, _ := multipartMedia.Schema.Schema().Properties.Get("package")
	require.Equal(t, "application/gzip", packageSchema.Schema().ContentMediaType)
	require.Empty(t, packageSchema.Schema().ContentEncoding)
	binaryItem, _ := model.Paths.PathItems.Get("/binary")
	binaryMedia, ok := binaryItem.Post.RequestBody.Content.Get("application/octet-stream")
	require.True(t, ok)
	require.Nil(t, binaryMedia.Schema)
}

func TestMizuoai_HeaderAndTextBody(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	var input textBodyRequest
	Post(srv, "/text", func(_ Tx[string], rx Rx[textBodyRequest]) {
		input, _ = rx.MizuRead()
	}, WithOperationResponses(&v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
		"204": {Description: "accepted"},
	})}))

	request := httptest.NewRequest(http.MethodPost, "/text", bytes.NewBufferString("hello"))
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("X-Request-Id", "request-1")
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	require.Equal(t, "request-1", input.Header.RequestId)
	require.Equal(t, "hello", input.Body)

	model := retrieveDocumentFromHandler(t, srv.Handler())
	item, _ := model.Paths.PathItems.Get("/text")
	require.Len(t, item.Post.Parameters, 1)
	require.Equal(t, "header", item.Post.Parameters[0].In)
	require.Equal(t, "X-Request-Id", item.Post.Parameters[0].Name)
	_, ok := item.Post.RequestBody.Content.Get("text/plain")
	require.True(t, ok)
}

func TestMizuoai_PointerAndRepeatedQueryDecoding(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	var input pointerQueryRequest
	Get(srv, "/query", func(_ Tx[string], rx Rx[pointerQueryRequest]) {
		input, _ = rx.MizuRead()
	})
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/query?values=1&values=2", nil))
	require.NotNil(t, input.Query)
	require.Nil(t, input.Query.Limit)
	require.Equal(t, []int{1, 2}, input.Query.Values)
}

func TestMizuoai_RejectsConflictingLegacyTags(t *testing.T) {
	t.Run("request location", func(t *testing.T) {
		type request struct {
			Query struct{} `json:"query" mizu:"path"`
		}
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		require.PanicsWithError(t,
			`register OpenAPI operation: conflicting request location tags on Query: json="query" mizu="path"`,
			func() { Get(srv, "/value", func(Tx[string], Rx[request]) {}) },
		)
	})

	t.Run("parameter name", func(t *testing.T) {
		type request struct {
			Query struct {
				Value string `json:"value" query:"other"`
			} `json:"query"`
		}
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		require.PanicsWithError(t,
			`register OpenAPI operation: conflicting query parameter tags on Value: json="value" query="other"`,
			func() { Get(srv, "/value", func(Tx[string], Rx[request]) {}) },
		)
	})
}

func TestMizuoai_ReflectedComponentCollisions(t *testing.T) {
	property := orderedmap.New[string, *base.SchemaProxy]()
	property.Set("value", base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}))
	equal := newComponents()
	equal.Schemas.Set("componentCollision", base.CreateSchemaProxy(&base.Schema{
		Type:       []string{"object"},
		Properties: property,
		Required:   []string{"value"},
	}))
	srv := mizu.NewServer("equal")
	require.NoError(t, Initialize(srv, "test", WithOaiComponents(equal)))
	Get(srv, "/equal", func(Tx[componentCollision], Rx[struct{}]) {})

	different := newComponents()
	different.Schemas.Set("componentCollision", base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}))
	conflict := mizu.NewServer("conflict")
	require.NoError(t, Initialize(conflict, "test", WithOaiComponents(different)))
	require.PanicsWithError(t,
		"register OpenAPI operation: incompatible OpenAPI component collision at schemas.componentCollision",
		func() { Get(conflict, "/conflict", func(Tx[componentCollision], Rx[struct{}]) {}) },
	)
}

func TestMizuoai_ReflectedSchemaCoverage(t *testing.T) {
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	Get(srv, "/coverage", func(Tx[reflectionCoverage], Rx[struct{}]) {})

	model := retrieveDocument(t, srv)
	coverage, ok := model.Components.Schemas.Get("reflectionCoverage")
	require.True(t, ok)
	schema := coverage.Schema()
	require.Contains(t, schema.Required, "embedded")
	require.NotContains(t, schema.Required, "optionalEmbedded")
	require.NotContains(t, schema.Required, "optional")
	require.NotContains(t, schema.Required, "forcedOptional")
	require.Contains(t, schema.Required, "forcedRequired")
	_, ok = schema.Properties.Get("Ignored")
	require.False(t, ok)
	_, ok = schema.Properties.Get("ignored")
	require.False(t, ok)

	boolean, _ := schema.Properties.Get("bool")
	require.Equal(t, []string{"boolean"}, boolean.Schema().Type)
	int8Schema, _ := schema.Properties.Get("int8")
	require.Equal(t, "int32", int8Schema.Schema().Format)
	uint64Schema, _ := schema.Properties.Get("uint64")
	require.Equal(t, float64(0), *uint64Schema.Schema().Minimum)
	floatSchema, _ := schema.Properties.Get("float32")
	require.Equal(t, "float", floatSchema.Schema().Format)
	alias, _ := schema.Properties.Get("alias")
	require.Equal(t, "#/components/schemas/scalarAlias", alias.GetReference())
	pointer, _ := schema.Properties.Get("pointer")
	require.Len(t, pointer.Schema().AnyOf, 2)
	array, _ := schema.Properties.Get("array")
	require.Equal(t, []string{"integer"}, array.Schema().Items.A.Schema().Type)
	slice, _ := schema.Properties.Get("slice")
	require.Equal(t, []string{"string"}, slice.Schema().Items.A.Schema().Type)
	mapSchema, _ := schema.Properties.Get("map")
	require.Equal(t, []string{"integer"}, mapSchema.Schema().AdditionalProperties.A.Schema().Type)
	timeSchema, _ := schema.Properties.Get("time")
	require.Equal(t, "date-time", timeSchema.Schema().Format)
	raw, _ := schema.Properties.Get("raw")
	require.Empty(t, raw.Schema().Type)
	bytesSchema, _ := schema.Properties.Get("bytes")
	require.Equal(t, "base64", bytesSchema.Schema().ContentEncoding)
	require.Equal(t, "application/octet-stream", bytesSchema.Schema().ContentMediaType)

	mutual, _ := model.Components.Schemas.Get("mutualA")
	b, _ := mutual.Schema().Properties.Get("b")
	require.Equal(t, "#/components/schemas/mutualB", b.Schema().AnyOf[0].GetReference())
	mutualBComponent, _ := model.Components.Schemas.Get("mutualB")
	a, _ := mutualBComponent.Schema().Properties.Get("a")
	require.Equal(t, "#/components/schemas/mutualA", a.Schema().AnyOf[0].GetReference())
	require.True(t, slices.Contains(schema.Required, "mutual"))
}

func TestMizuoai_ResponseDefaultsAndReferences(t *testing.T) {
	t.Run("default response", func(t *testing.T) {
		operation := &v3.Operation{
			Responses: &v3.Responses{Default: &v3.Response{Description: "fallback"}},
		}
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		GetRaw(srv, "/default", func(http.ResponseWriter, *http.Request) {}, WithOperation(operation))
		model := retrieveDocument(t, srv)
		item, _ := model.Paths.PathItems.Get("/default")
		require.Equal(t, "fallback", item.Get.Responses.Default.Description)
	})

	t.Run("referenced response", func(t *testing.T) {
		components := newComponents()
		components.Responses.Set("Success", &v3.Response{Description: "success"})
		operation := &v3.Operation{
			Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
				"200": v3.CreateResponseRef("#/components/responses/Success"),
			})},
		}
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test", WithOaiComponents(components)))
		GetRaw(srv, "/reference", func(http.ResponseWriter, *http.Request) {}, WithOperation(operation))
		config := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
		rendered, err := config.render(false)
		require.NoError(t, err)
		require.Contains(t, string(rendered), "#/components/responses/Success")
	})
}

func TestMizuoai_ImportedDocumentInheritance(t *testing.T) {
	document := MustParseOpenAPI([]byte(`openapi: 3.1.0
info: {title: test, version: v1}
servers:
  - url: https://api.example.com
security:
  - bearerAuth: []
paths:
  /secured:
    get:
      operationId: secured
      responses:
        "200": {description: ok}
components:
  securitySchemes:
    bearerAuth: {type: http, scheme: bearer}
`))
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	GetRaw(srv, "/secured", func(http.ResponseWriter, *http.Request) {},
		WithOpenApiOperation(document, "secured"))

	model := retrieveDocument(t, srv)
	item, _ := model.Paths.PathItems.Get("/secured")
	require.Len(t, item.Get.Security, 1)
	require.Len(t, item.Get.Servers, 1)
	require.Equal(t, "https://api.example.com", item.Get.Servers[0].URL)
	_, ok := model.Components.SecuritySchemes.Get("bearerAuth")
	require.True(t, ok)
}

func TestMizuoai_AuthoritativeOperationSupplements(t *testing.T) {
	operation := &v3.Operation{
		OperationId: "original",
		Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"200": {Description: "ok"},
		})},
	}
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test"))
	GetRaw(srv, "/value", func(http.ResponseWriter, *http.Request) {},
		WithOperation(operation),
		WithOperationSummary("Get a value"),
		WithResponse(http.StatusCreated, &v3.Response{Description: "created"}),
	)
	_, mutated := operation.Responses.Codes.Get("201")
	require.False(t, mutated)
	model := retrieveDocument(t, srv)
	item, _ := model.Paths.PathItems.Get("/value")
	require.Equal(t, "Get a value", item.Get.Summary)
	require.NotNil(t, item.Get.Responses.FindResponseByCode(http.StatusOK))
	require.NotNil(t, item.Get.Responses.FindResponseByCode(http.StatusCreated))

	conflict := mizu.NewServer("conflict")
	require.NoError(t, Initialize(conflict, "test"))
	require.PanicsWithError(t,
		`register raw OpenAPI operation: authoritative OpenAPI operation "original": conflicting OpenAPI operation response 200`,
		func() {
			GetRaw(conflict, "/value", func(http.ResponseWriter, *http.Request) {},
				WithOperation(operation),
				WithResponse(http.StatusOK, &v3.Response{Description: "different"}),
			)
		},
	)
}

func TestMizuoai_RawRegistrationValidation(t *testing.T) {
	document := MustParseOpenAPI([]byte(`openapi: 3.1.0
info: {title: test, version: v1}
paths:
  /expected:
    post:
      operationId: expected
      responses:
        "204": {description: done}
`))
	t.Run("missing operation", func(t *testing.T) {
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		require.Panics(t, func() {
			PostRaw(srv, "/expected", func(http.ResponseWriter, *http.Request) {},
				WithOpenApiOperation(document, "missing"))
		})
	})
	t.Run("path mismatch", func(t *testing.T) {
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		require.Panics(t, func() {
			PostRaw(srv, "/different", func(http.ResponseWriter, *http.Request) {},
				WithOpenApiOperation(document, "expected"))
		})
	})
	t.Run("extra path parameter", func(t *testing.T) {
		document := MustParseOpenAPI([]byte(`openapi: 3.1.0
info: {title: test, version: v1}
paths:
  /expected:
    get:
      operationId: extra
      parameters:
        - {name: extra, in: path, required: true, schema: {type: string}}
      responses:
        "200": {description: ok}
`))
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		require.PanicsWithError(t,
			"register raw OpenAPI operation: OpenAPI path parameter \"extra\" is not present in path /expected",
			func() {
				GetRaw(srv, "/expected", func(http.ResponseWriter, *http.Request) {},
					WithOpenApiOperation(document, "extra"))
			},
		)
	})

	t.Run("unresolved native reference", func(t *testing.T) {
		operation := &v3.Operation{Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"200": {
				Description: "ok",
				Content: orderedmap.ToOrderedMap(map[string]*v3.MediaType{
					"application/json": {Schema: base.CreateSchemaProxyRef("#/components/schemas/Missing")},
				}),
			},
		})}}
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		require.PanicsWithError(t,
			"register raw OpenAPI operation: unresolved OpenAPI component reference schemas.Missing",
			func() { GetRaw(srv, "/missing", func(http.ResponseWriter, *http.Request) {}, WithOperation(operation)) },
		)
		recorder := httptest.NewRecorder()
		srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing", nil))
		require.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("duplicate parameters", func(t *testing.T) {
		required := true
		parameter := func() *v3.Parameter {
			return &v3.Parameter{
				Name: "id", In: "path", Required: &required,
				Schema: base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}),
			}
		}
		operation := &v3.Operation{
			Parameters: []*v3.Parameter{parameter(), parameter()},
			Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
				"200": {Description: "ok"},
			})},
		}
		srv := mizu.NewServer("test")
		require.NoError(t, Initialize(srv, "test"))
		require.PanicsWithError(t,
			"register raw OpenAPI operation: duplicate OpenAPI operation parameter path/id",
			func() {
				GetRaw(srv, "/items/{id}", func(http.ResponseWriter, *http.Request) {}, WithOperation(operation))
			},
		)
	})
}

func TestMizuoai_RejectsDuplicateOperationIdsAndComponentCollisions(t *testing.T) {
	_, err := ParseOpenAPI([]byte(`openapi: 3.1.0
info: {title: test, version: v1}
paths:
  /one:
    get:
      operationId: duplicate
      responses: {"200": {description: ok}}
  /two:
    get:
      operationId: duplicate
      responses: {"200": {description: ok}}
`))
	require.ErrorContains(t, err, "duplicate openapi operationId")

	document := MustParseOpenAPI([]byte(`openapi: 3.1.0
info: {title: test, version: v1}
paths:
  /value:
    get:
      operationId: value
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Value"}
components:
  schemas:
    Value: {type: string}
`))
	components := newComponents()
	components.Schemas.Set("Value", base.CreateSchemaProxy(&base.Schema{Type: []string{"object"}}))
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiComponents(components)))
	require.Panics(t, func() {
		GetRaw(srv, "/value", func(http.ResponseWriter, *http.Request) {},
			WithOpenApiOperation(document, "value"))
	})
}

func TestMizuoai_ImportsOnlyReferencedComponents(t *testing.T) {
	document := MustParseOpenAPI([]byte(`openapi: 3.1.0
info: {title: test, version: v1}
paths:
  /value:
    get:
      operationId: value
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Used"}
components:
  schemas:
    Used: {type: string}
    Unused: {type: string}
`))
	components := newComponents()
	components.Schemas.Set("Unused", base.CreateSchemaProxy(&base.Schema{Type: []string{"object"}}))
	srv := mizu.NewServer("test")
	require.NoError(t, Initialize(srv, "test", WithOaiComponents(components)))
	GetRaw(srv, "/value", func(http.ResponseWriter, *http.Request) {},
		WithOpenApiOperation(document, "value"))
	model := retrieveDocument(t, srv)
	used, ok := model.Components.Schemas.Get("Used")
	require.True(t, ok)
	require.Equal(t, []string{"string"}, used.Schema().Type)
	unused, ok := model.Components.Schemas.Get("Unused")
	require.True(t, ok)
	require.Equal(t, []string{"object"}, unused.Schema().Type)
}

func retrieveDocument(t *testing.T, srv *mizu.Server) *v3.Document {
	t.Helper()
	return retrieveDocumentFromHandler(t, srv.Handler())
}

func retrieveDocumentFromHandler(t *testing.T, handler http.Handler) *v3.Document {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	document, err := libopenapi.NewDocument(recorder.Body.Bytes())
	require.NoError(t, err)
	model, err := document.BuildV3Model()
	require.NoError(t, err)
	return &model.Model
}
