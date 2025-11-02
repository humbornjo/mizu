package mizuoai

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/humbornjo/mizu"
)

func handle[I any, O any](
	method string, mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	if mux == nil {
		panic("nil oai instance")
	}

	config := &operationConfig{path: pattern, method: method}
	for _, opt := range opts {
		opt(config)
	}
	enrichOperation[I, O](config)

	srv, ok := mux.(*mizu.Server)
	if !ok {
		panic("oai can only be used with mizu.Mux from mizu.Server")
	}
	oai := mizu.Hook[ctxkey, oaiConfig](srv, _CTXKEY_OAI, nil)
	oai.handlers = append(oai.handlers, config)
	switch method {
	case http.MethodGet:
		mux.Get(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPost:
		mux.Post(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPut:
		mux.Put(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodDelete:
		mux.Delete(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPatch:
		mux.Patch(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodHead:
		mux.Head(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodOptions:
		mux.Options(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodTrace:
		mux.Trace(pattern, handler[I, O](oaiHandler).genHandler())
	}
	return &config.Operation
}

// Get registers a generic handler for GET requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Get[I any, O any](mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodGet, mux, pattern, oaiHandler, opts...)
}

// POST registers a generic handler for POST requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Post[I any, O any](mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodPost, mux, pattern, oaiHandler, opts...)
}

// Put registers a generic handler for PUT requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Put[I any, O any](mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodPut, mux, pattern, oaiHandler, opts...)
}

// Delete registers a generic handler for DELETE requests. It
// uses reflection to parse request data into the input type `I`
// and generate OpenAPI documentation.
func Delete[I any, O any](mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodDelete, mux, pattern, oaiHandler, opts...)
}

// Patch registers a generic handler for PATCH requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Patch[I any, O any](mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodPatch, mux, pattern, oaiHandler, opts...)
}

// Head registers a generic handler for HEAD requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Head[I any, O any](mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodHead, mux, pattern, oaiHandler, opts...)
}

// Options registers a generic handler for OPTIONS requests. It
// uses reflection to parse request data into the input type `I`
// and generate OpenAPI documentation.
func Options[I any, O any](mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodOptions, mux, pattern, oaiHandler, opts...)
}

// Trace registers a generic handler for TRACE requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Trace[I any, O any](mux mizu.Mux, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodTrace, mux, pattern, oaiHandler, opts...)
}
