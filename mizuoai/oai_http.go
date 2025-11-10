package mizuoai

import (
	"net/http"

	"github.com/humbornjo/mizu"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

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
	oai.handlers = append(oai.handlers, config)
	switch method {
	case http.MethodGet:
		srv.Get(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPost:
		srv.Post(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPut:
		srv.Put(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodDelete:
		srv.Delete(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPatch:
		srv.Patch(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodHead:
		srv.Head(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodOptions:
		srv.Options(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodTrace:
		srv.Trace(pattern, handler[I, O](oaiHandler).genHandler())
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
