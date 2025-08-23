package mizuoai

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
)

func handle[I any, O any](method string,
	oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	if oai == nil {
		panic("nil oai instance")
	}

	config := &operationConfig{path: pattern, method: method}
	for _, opt := range opts {
		opt(config)
	}
	enrichOperation[I, O](config)

	oai.oaiConfig.handlers = append(oai.oaiConfig.handlers, config)
	switch method {
	case http.MethodGet:
		oai.server.Get(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPost:
		oai.server.Post(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPut:
		oai.server.Put(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodDelete:
		oai.server.Delete(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodPatch:
		oai.server.Patch(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodHead:
		oai.server.Head(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodOptions:
		oai.server.Options(pattern, handler[I, O](oaiHandler).genHandler())
	case http.MethodTrace:
		oai.server.Trace(pattern, handler[I, O](oaiHandler).genHandler())
	}
	return &config.Operation
}

// Get registers a generic handler for GET requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Get[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodGet, oai, pattern, oaiHandler, opts...)
}

// POST registers a generic handler for POST requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Post[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodPost, oai, pattern, oaiHandler, opts...)
}

// Put registers a generic handler for PUT requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Put[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodPut, oai, pattern, oaiHandler, opts...)
}

// Delete registers a generic handler for DELETE requests. It
// uses reflection to parse request data into the input type `I`
// and generate OpenAPI documentation.
func Delete[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodDelete, oai, pattern, oaiHandler, opts...)
}

// Patch registers a generic handler for PATCH requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Patch[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodPatch, oai, pattern, oaiHandler, opts...)
}

// Head registers a generic handler for HEAD requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Head[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodHead, oai, pattern, oaiHandler, opts...)
}

// Options registers a generic handler for OPTIONS requests. It
// uses reflection to parse request data into the input type `I`
// and generate OpenAPI documentation.
func Options[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodOptions, oai, pattern, oaiHandler, opts...)
}

// Trace registers a generic handler for TRACE requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Trace[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption,
) *openapi3.Operation {
	return handle(http.MethodTrace, oai, pattern, oaiHandler, opts...)
}
