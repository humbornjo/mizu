package mizuoai

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sync"
	"text/template"

	"github.com/humbornjo/mizu"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
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

// Initialize inject OpenAPI config into mizu.Server with the
// given path and options. openapi.json will be served at
// /{path}/openapi.json. HTML will be served at /{path}/openapi
// if enabled. {path} can be set using WithOaiServePath
func Initialize(srv *mizu.Server, title string, opts ...OaiOption) error {
	if title == "" {
		return errors.New("openapi spec title is required")
	}

	config := &oaiConfig{
		info: new(base.Info),
	}
	config.info.Title = title
	for _, opt := range opts {
		opt(config)
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
				fmt.Printf("🚨 [ERROR] Failed to generate openapi.json: %s\n", err)
				return
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

// Path registers a new path in the OpenAPI spec. It can be used
// to set the path field that can't be accessed via Get, Post,
// etc.
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

	oai.paths.PathItems.Set(pattern, &config.PathItem)
}

// Rx represents the request side of an API endpoint. It provides
// access to the parsed request data and the original request
// context.
type Rx[T any] struct {
	*http.Request
	read func(*http.Request) (T, error)
}

// Read returns the parsed input from the request. The parsing
// logic is generated based on the struct tags of the input type.
func (rx Rx[T]) MizuRead() (T, error) {
	return rx.read(rx.Request)
}

// Tx represents the response side of an API endpoint. It
// provides methods to write the response.
type Tx[T any] struct {
	http.ResponseWriter
	write func(*T) error
}

// Write writes the JSON-encoded output to the response writer.
// It also sets the Content-Type header to "application/json".
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

// newHandler wraps the user-provided handler with request
// parsing logic.
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
		path:   pattern,
		method: method,
		Operation: v3.Operation{
			Deprecated: new(bool),
			Callbacks:  orderedmap.New[string, *v3.Callback](),
			Responses: &v3.Responses{
				Codes: orderedmap.New[string, *v3.Response](),
			},
		},
	}
	for _, opt := range opts {
		opt(config)
	}
	enrichOperation[I, O](config)

	oai := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	if oai == nil {
		panic("oai not initialized, call Initialize first")
	}

	oai.handlers = append(oai.handlers, config)
	switch method {
	case http.MethodGet:
		srv.Get(pattern, handler[I, O](oaiHandler).newHandler())
	case http.MethodPost:
		srv.Post(pattern, handler[I, O](oaiHandler).newHandler())
	case http.MethodPut:
		srv.Put(pattern, handler[I, O](oaiHandler).newHandler())
	case http.MethodDelete:
		srv.Delete(pattern, handler[I, O](oaiHandler).newHandler())
	case http.MethodPatch:
		srv.Patch(pattern, handler[I, O](oaiHandler).newHandler())
	case http.MethodHead:
		srv.Head(pattern, handler[I, O](oaiHandler).newHandler())
	case http.MethodOptions:
		srv.Options(pattern, handler[I, O](oaiHandler).newHandler())
	case http.MethodTrace:
		srv.Trace(pattern, handler[I, O](oaiHandler).newHandler())
	}
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
