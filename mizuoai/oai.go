package mizuoai

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path"
	"strings"
	"sync"
	"text/template"

	"github.com/humbornjo/mizu"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

var (
	//go:embed tmpl_stoplight.html
	_STOPLIGHT_UI_TEMPLATE_CONTENT string
	_STOPLIGHT_UI_TEMPLATE         = template.Must(template.New("oai_ui").Parse(_STOPLIGHT_UI_TEMPLATE_CONTENT))
)

type ctxkey int

const (
	_CTXKEY_OAI ctxkey = iota
)

// Initialize inject OpenAPI config into mizu.Server with the given
// path and options. openapi.json will be served at /{path}/openapi.json.
// HTML will be served at /{path}/openapi if enabled. {path} can be
// set using WithOaiServePath
func Initialize(srv *mizu.Server, title string, opts ...OaiOption) error {
	if title == "" {
		return errors.New("openapi spec title is required")
	}
	alreadyInitialized := false
	mizu.Immediate(srv, _CTXKEY_OAI, func(existing *oaiConfig) {
		alreadyInitialized = existing != nil
	})
	if alreadyInitialized {
		return errors.New("openapi already initialized")
	}

	config := &oaiConfig{
		version:       _OPENAPI_VERSION,
		info:          new(base.Info),
		paths:         orderedmap.New[string, *v3.PathItem](),
		components:    newComponents(),
		operationIds:  make(map[string]string),
		routes:        make(map[string]bool),
		rawComponents: make(map[string]*yaml.Node),
		webhooks:      orderedmap.New[string, *v3.PathItem](),
	}
	config.reflector = newSchemaReflector(config.components.Schemas)
	config.info.Title = title
	for _, opt := range opts {
		opt(config)
	}
	if config.err != nil {
		return config.err
	}
	if len(config.baseData) > 0 {
		document, err := parseOpenApiDocument(config.baseData, false)
		if err != nil {
			return err
		}
		for operationId, operation := range document.operations {
			config.operationIds[operationId] = operation.method + " " + operation.path
		}
		rawOperations, err := rawDocumentOperations(document.raw)
		if err != nil {
			return err
		}
		for _, operation := range rawOperations {
			config.routes[operation.method+" "+operation.path] = true
		}
		if document.model.Paths != nil && document.model.Paths.PathItems != nil {
			for routePath, item := range document.model.Paths.PathItems.FromOldest() {
				if item == nil || item.GetOperations() == nil {
					continue
				}
				for method := range item.GetOperations().KeysFromOldest() {
					config.routes[strings.ToUpper(method)+" "+routePath] = true
				}
			}
		}

		maps.Copy(config.rawComponents, document.components)
	}
	if _, err := config.render(false); err != nil {
		return fmt.Errorf("initialize OpenAPI document: %w", err)
	}

	// Serve openapi.json
	fileName := "/openapi.yaml"
	contentType := "text/yaml"
	if config.enableJson {
		fileName = "/openapi.json"
		contentType = "application/json"
	}

	once := sync.Once{}
	mizu.Hook(srv, _CTXKEY_OAI, config, mizu.WithHookHandler(func(srv *mizu.Server) {
		once.Do(func() {
			content, err := config.render(config.enableJson)
			if err != nil {
				panic(fmt.Errorf("generate OpenAPI document: %w", err))
			}
			srv.Get(path.Join(config.servePath, fileName), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType)
				_, _ = w.Write(content)
			})

			if !config.enableDocument {
				return
			}
			encoded, _ := json.Marshal(string(content))
			srv.Get(path.Join(config.servePath, "/openapi"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_ = _STOPLIGHT_UI_TEMPLATE.Execute(w, map[string]string{"Document": string(encoded)})
			})
		})
	}))

	return nil
}

// Path registers a new path in the OpenAPI spec. It can be used to
// set the path field that can't be accessed via Get, Post, etc.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func Path(server *mizu.Server, pattern string, opts ...PathOption) {
	config := &pathConfig{}
	for _, opt := range opts {
		opt(config)
	}

	oai := mizu.Hook[ctxkey, oaiConfig](server, _CTXKEY_OAI, nil)
	if oai == nil {
		panic("oai not initialized, call Initialize first")
	}

	resolved := server.Pattern(pattern)
	oai.mu.Lock()
	defer oai.mu.Unlock()
	previous, existed := oai.paths.Get(resolved)
	candidate := clonePathItem(previous)
	if err := mergePathItem(candidate, &config.PathItem); err != nil {
		panic(fmt.Errorf("register OpenAPI path %s: %w", resolved, err))
	}
	operations := config.GetOperations()
	locations := make([]string, 0)
	operationIds := make(map[string]string)
	if operations != nil {
		for method, operation := range operations.FromOldest() {
			if operation == nil {
				continue
			}
			location := strings.ToUpper(method) + " " + resolved
			if oai.routes[location] {
				panic(fmt.Errorf("register OpenAPI path %s: duplicate OpenAPI operation %s", resolved, location))
			}
			if err := validateResponses(operation.Responses, location); err != nil {
				panic(fmt.Errorf("register OpenAPI path %s: %w", resolved, err))
			}
			if operation.OperationId != "" {
				if previous, ok := oai.operationIds[operation.OperationId]; ok {
					panic(fmt.Errorf(
						"register OpenAPI path %s: duplicate OpenAPI operationId %q at %s and %s",
						resolved, operation.OperationId, previous, location,
					))
				}
				operationIds[operation.OperationId] = location
			}
			if err := validateParameterDuplicates(candidate, operation.Parameters, oai.components); err != nil {
				panic(fmt.Errorf("register OpenAPI path %s: %w", resolved, err))
			}
			operationConfig := &operationConfig{
				Operation: *operation, method: strings.ToUpper(method), path: resolved,
				pathItem: candidate, components: oai.components,
			}
			if err := validatePathParameters(operationConfig); err != nil {
				panic(fmt.Errorf("register OpenAPI path %s: %w", resolved, err))
			}
			if _, err := collectOperationComponents(
				&v3.Document{Components: oai.components},
				&documentOperation{operation: operation, pathItem: candidate},
			); err != nil {
				panic(fmt.Errorf("register OpenAPI path %s: %w", resolved, err))
			}
			locations = append(locations, location)
		}
	}
	oai.paths.Set(resolved, candidate)
	if _, err := oai.renderUnlocked(false); err != nil {
		if existed {
			oai.paths.Set(resolved, previous)
		} else {
			oai.paths.Delete(resolved)
		}
		panic(fmt.Errorf("register OpenAPI path %s: %w", resolved, err))
	}
	for _, location := range locations {
		oai.routes[location] = true
	}

	maps.Copy(oai.operationIds, operationIds)
}

// Rx represents the request side of an API endpoint. It provides
// access to the parsed request data and the original request context.
type Rx[T any] struct {
	*http.Request
	read func(*http.Request) (T, error)
}

// Read returns the parsed input from the request. The parsing logic
// is generated based on the struct tags of the input type.
func (rx Rx[T]) MizuRead() (T, error) {
	return rx.read(rx.Request)
}

// Tx represents the response side of an API endpoint. It provides
// methods to write the response.
type Tx[T any] struct {
	http.ResponseWriter
	write func(*T) error
}

// Write writes the JSON-encoded output to the response writer. It
// also sets the Content-Type header to "application/json".
func (tx Tx[T]) MizuWrite(data *T) error {
	return tx.write(data)
}

// mizutag represents the source of request data (e.g., path, body).
type mizutag string

func (t mizutag) String() string {
	return string(t)
}

const (
	_STRUCT_TAG_PATH   mizutag = "path"
	_STRUCT_TAG_QUERY  mizutag = "query"
	_STRUCT_TAG_HEADER mizutag = "header"
	_STRUCT_TAG_BODY   mizutag = "body"
	_STRUCT_TAG_FORM   mizutag = "form"
)

// handler is a generic type for user-provided API logic.
type handler[I any, O any] func(Tx[O], Rx[I])

// newHandler wraps the user-provided handler with request parsing
// logic.
func (h handler[I, O]) newHandler() http.HandlerFunc {
	encoder := newEncoder[O]()
	decoder := newDecoder[I]()
	return func(w http.ResponseWriter, r *http.Request) {
		tx := Tx[O]{w, func(val *O) error {
			return encoder.encode(w, val)
		}}
		rx := Rx[I]{r, func(r *http.Request) (input I, err error) {
			return input, decoder.decode(r, &input)
		}}
		h(tx, rx)
	}
}

func handle[I any, O any](
	method string, srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	config := &operationConfig{
		method: method,
		path:   srv.Pattern(pattern),
	}
	for _, opt := range opts {
		opt(config)
	}

	oai := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	if oai == nil {
		panic("oai not initialized, call Initialize first")
	}

	if config.external == nil {
		components := newComponents()
		enrichOperation[I, O](config, oai.reflector.withSchemas(components.Schemas))
		config.components = components
	}
	if err := oai.addOperation(config); err != nil {
		panic(fmt.Errorf("register OpenAPI operation: %w", err))
	}
	registerHandler(method, srv, pattern, handler[I, O](oaiHandler).newHandler())
	return &config.Operation
}

func registerHandler(method string, srv *mizu.Server, pattern string, handler http.HandlerFunc) {
	switch method {
	case http.MethodGet:
		srv.Get(pattern, handler)
	case http.MethodPost:
		srv.Post(pattern, handler)
	case http.MethodPut:
		srv.Put(pattern, handler)
	case http.MethodDelete:
		srv.Delete(pattern, handler)
	case http.MethodPatch:
		srv.Patch(pattern, handler)
	case http.MethodHead:
		srv.Head(pattern, handler)
	case http.MethodOptions:
		srv.Options(pattern, handler)
	case http.MethodTrace:
		srv.Trace(pattern, handler)
	case http.MethodConnect:
		srv.Connect(pattern, handler)
	default:
		panic("unsupported HTTP method: " + method)
	}
}

func handleRaw(
	method string, srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption,
) *v3.Operation {
	config := &operationConfig{method: method, path: srv.Pattern(pattern)}
	for _, opt := range opts {
		opt(config)
	}
	if config.external == nil {
		ensureOperation(config)
	}
	oai := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	if oai == nil {
		panic("oai not initialized, call Initialize first")
	}
	if err := oai.addOperation(config); err != nil {
		panic(fmt.Errorf("register raw OpenAPI operation: %w", err))
	}
	registerHandler(method, srv, pattern, rawHandler)
	return &config.Operation
}

// Get registers a generic handler for GET requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Get[I any, O any](srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodGet, srv, pattern, oaiHandler, opts...)
}

// POST registers a generic handler for POST requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Post[I any, O any](srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodPost, srv, pattern, oaiHandler, opts...)
}

// Put registers a generic handler for PUT requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Put[I any, O any](srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodPut, srv, pattern, oaiHandler, opts...)
}

// Delete registers a generic handler for DELETE requests. It
// uses reflection to parse request data into the input type `I`
// and generate OpenAPI documentation.
func Delete[I any, O any](srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodDelete, srv, pattern, oaiHandler, opts...)
}

// Patch registers a generic handler for PATCH requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Patch[I any, O any](srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodPatch, srv, pattern, oaiHandler, opts...)
}

// Head registers a generic handler for HEAD requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Head[I any, O any](srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodHead, srv, pattern, oaiHandler, opts...)
}

// Options registers a generic handler for OPTIONS requests. It
// uses reflection to parse request data into the input type `I`
// and generate OpenAPI documentation.
func Options[I any, O any](srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodOptions, srv, pattern, oaiHandler, opts...)
}

// Trace registers a generic handler for TRACE requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Trace[I any, O any](srv *mizu.Server, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodTrace, srv, pattern, oaiHandler, opts...)
}

// GetRaw registers a raw GET handler with an explicit OpenAPI operation.
func GetRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodGet, srv, pattern, rawHandler, opts...)
}

// PostRaw registers a raw POST handler with an explicit OpenAPI operation.
func PostRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodPost, srv, pattern, rawHandler, opts...)
}

// PutRaw registers a raw PUT handler with an explicit OpenAPI operation.
func PutRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodPut, srv, pattern, rawHandler, opts...)
}

// DeleteRaw registers a raw DELETE handler with an explicit OpenAPI operation.
func DeleteRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodDelete, srv, pattern, rawHandler, opts...)
}

// PatchRaw registers a raw PATCH handler with an explicit OpenAPI operation.
func PatchRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodPatch, srv, pattern, rawHandler, opts...)
}

// HeadRaw registers a raw HEAD handler with an explicit OpenAPI operation.
func HeadRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodHead, srv, pattern, rawHandler, opts...)
}

// OptionsRaw registers a raw OPTIONS handler with an explicit OpenAPI operation.
func OptionsRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodOptions, srv, pattern, rawHandler, opts...)
}

// TraceRaw registers a raw TRACE handler with an explicit OpenAPI operation.
func TraceRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodTrace, srv, pattern, rawHandler, opts...)
}

// ConnectRaw registers a raw CONNECT handler using OpenAPI 3.2 additionalOperations.
func ConnectRaw(srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption) *v3.Operation {
	return handleRaw(http.MethodConnect, srv, pattern, rawHandler, opts...)
}
