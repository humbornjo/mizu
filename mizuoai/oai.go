package mizuoai

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"reflect"
	"sync"
	"text/template"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuoai/internal"
	"github.com/humbornjo/mizu/mizuoai/internal/serde"
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
// set using WithDocumentServePath
func Initialize(srv *mizu.Server, title string, opts ...DocumentOption) error {
	if title == "" {
		return errors.New("openapi spec title is required")
	}
	alreadyInitialized := false
	mizu.Immediate(srv, _CTXKEY_OAI, func(existing *documentConfig) {
		alreadyInitialized = existing != nil
	})
	if alreadyInitialized {
		return errors.New("openapi already initialized")
	}

	config := &documentConfig{
		operationIds:    make(map[string]string),
		operationRoutes: make(map[string]bool),
		overridedNames:  make(map[string]string),
		patchedSchemas:  make(map[string]string),
		renderFormat:    _RENDER_FORMAT_YAML,
		base: &v3.Document{
			Paths:      &v3.Paths{PathItems: orderedmap.New[string, *v3.PathItem]()},
			Components: internal.NewComponents(),
			Webhooks:   orderedmap.New[string, *v3.PathItem](),
		},
		override: &v3.Document{
			Version:    _OPENAPI_VERSION,
			Info:       new(base.Info),
			Components: internal.NewComponents(),
		},
	}

	config.override.Info.Title = title
	for _, opt := range opts {
		opt(config)
	}

	if _, err := config.Render(_RENDER_FORMAT_YAML); err != nil {
		return fmt.Errorf("initialize OpenAPI document: %w", err)
	}

	var fileName, contentType string
	switch config.renderFormat {
	case _RENDER_FORMAT_JSON:
		fileName, contentType = "/openapi.json", "application/json"
	case _RENDER_FORMAT_YAML:
		fileName, contentType = "/openapi.yaml", "text/yaml"
	}

	once := sync.Once{}
	mizu.Hook(srv, _CTXKEY_OAI, config, mizu.WithHookHandler(func(srv *mizu.Server) {
		once.Do(func() {
			content, err := config.Render(_RENDER_FORMAT_AUTO)
			if err != nil {
				panic(fmt.Errorf("generate OpenAPI document: %w", err))
			}
			srv.Get(path.Join(config.route, fileName), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType)
				_, _ = w.Write(content)
			})

			if !config.renderHTML {
				return
			}
			encoded, _ := json.Marshal(string(content))
			srv.Get(path.Join(config.route, "/openapi"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_ = _STOPLIGHT_UI_TEMPLATE.Execute(w, map[string]string{"Document": string(encoded)})
			})
		})
	}))

	return nil
}

type Tx[T any] = serde.Tx[T]

type Rx[T any] = serde.Rx[T]

type Handler[I, O any] func(http.ResponseWriter, Rx[I]) (O, error)

// NewHandler wraps the user-provided handler with request parsing
// logic.
func NewHandler[I, O any](handler Handler[I, O]) http.HandlerFunc {
	codec, err := serde.NewCodec[I, O]()
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		tx, rx := codec.Split(w, r)
		mw := internal.WrapResponseWriter(w)

		out, err := handler(mw, rx)
		if err != nil {
			if !mw.Takeover() {
				_ = mizu.ResponseError(w, err)
			}
			return
		}
		if mw.Takeover() {
			return
		}
		_ = tx.Xwrite(&out)
	}
}

func handle[I any, O any](
	method string, srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	oai := mizu.Hook[ctxkey, documentConfig](srv, _CTXKEY_OAI, nil)
	if oai == nil {
		panic("oai not initialized, call Initialize first")
	}
	op := oai.ExtractOperation(reflect.TypeFor[I](), reflect.TypeFor[O]())
	baseOpt := []OperationOption{WithOperationBase(op)}
	return handleRaw(method, srv, pattern, NewHandler(handler), append(baseOpt, opts...)...)
}

func handleRaw(method string, srv *mizu.Server, pattern string, rawHandler http.HandlerFunc, opts ...OperationOption,
) *v3.Operation {
	config := &operationConfig{method: method, path: srv.Pattern(pattern)}
	config.Ensure()
	for _, opt := range opts {
		opt(config)
	}
	oai := mizu.Hook[ctxkey, documentConfig](srv, _CTXKEY_OAI, nil)
	if oai == nil {
		panic("oai not initialized, call Initialize first")
	}
	if err := oai.RegisterOperation(config); err != nil {
		panic(fmt.Errorf("register raw OpenAPI operation: %w", err))
	}
	switch method {
	case http.MethodGet:
		srv.Get(pattern, rawHandler)
	case http.MethodPost:
		srv.Post(pattern, rawHandler)
	case http.MethodPut:
		srv.Put(pattern, rawHandler)
	case http.MethodDelete:
		srv.Delete(pattern, rawHandler)
	case http.MethodPatch:
		srv.Patch(pattern, rawHandler)
	case http.MethodHead:
		srv.Head(pattern, rawHandler)
	case http.MethodOptions:
		srv.Options(pattern, rawHandler)
	case http.MethodTrace:
		srv.Trace(pattern, rawHandler)
	case http.MethodConnect:
		srv.Connect(pattern, rawHandler)
	default:
		panic("unsupported HTTP method: " + method)
	}
	return config.override
}

// Get registers a generic handler for GET requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Get[I any, O any](srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodGet, srv, pattern, handler, opts...)
}

// POST registers a generic handler for POST requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Post[I any, O any](srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodPost, srv, pattern, handler, opts...)
}

// Put registers a generic handler for PUT requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Put[I any, O any](srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodPut, srv, pattern, handler, opts...)
}

// Delete registers a generic handler for DELETE requests. It
// uses reflection to parse request data into the input type `I`
// and generate OpenAPI documentation.
func Delete[I any, O any](srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodDelete, srv, pattern, handler, opts...)
}

// Patch registers a generic handler for PATCH requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Patch[I any, O any](srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodPatch, srv, pattern, handler, opts...)
}

// Head registers a generic handler for HEAD requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Head[I any, O any](srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodHead, srv, pattern, handler, opts...)
}

// Options registers a generic handler for OPTIONS requests. It
// uses reflection to parse request data into the input type `I`
// and generate OpenAPI documentation.
func Options[I any, O any](srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodOptions, srv, pattern, handler, opts...)
}

// Trace registers a generic handler for TRACE requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Trace[I any, O any](srv *mizu.Server, pattern string, handler Handler[I, O], opts ...OperationOption,
) *v3.Operation {
	return handle(http.MethodTrace, srv, pattern, handler, opts...)
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
