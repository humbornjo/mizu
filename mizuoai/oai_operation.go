package mizuoai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/humbornjo/mizu/mizuoai/internal"
)

type OperationOption func(*operationConfig)

type operationConfig struct {
	path       string
	method     string
	base       *v3.Operation
	override   *v3.Operation
	components *v3.Components
}

// Ensure initializes the fields that enrichment and rendering write
// into, so later code never sees nil.
func (c *operationConfig) Ensure() {
	if c.override == nil {
		c.override = &v3.Operation{}
	}
}

// WithOperationTags adds tags to the operation, for logical grouping
// of operations.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationTags(tags ...string) OperationOption {
	return func(c *operationConfig) {
		c.override.Tags = append(c.override.Tags, tags...)
	}
}

// WithOperationSummary provides a summary of what the operation does.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationSummary(summary string) OperationOption {
	return func(c *operationConfig) {
		c.override.Summary = summary
	}
}

// WithOperationDescription provides a verbose explanation of the
// operation behavior. CommonMark syntax MAY be used for rich text
// representation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationDescription(description string) OperationOption {
	return func(c *operationConfig) {
		c.override.Description = description
	}
}

// WithOperationExternalDocs provides a reference to an external
// resource for extended documentation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#external-documentation-object
func WithOperationExternalDocs(url string, description string, extensions ...orderedmap.Map[string, any]) OperationOption {
	var firstExtensions orderedmap.Map[string, any]
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *operationConfig) {
		c.override.ExternalDocs = &base.ExternalDoc{
			Description: description,
			URL:         url,
			Extensions:  internal.CanonicalExtensions(firstExtensions),
		}
	}
}

// WithOperationOperationId provides a unique string used to identify
// the operation. Unique string used to identify the operation. The id
// MUST be unique among all operations described in the API. The
// operationId value is case-sensitive.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationOperationId(operationId string) OperationOption {
	return func(c *operationConfig) {
		c.override.OperationId = operationId
	}
}

// WithOperationParameters adds parameters to the operation. A list of
// parameters that are applicable for this operation. If a parameter
// is already defined in the Path Item, the new definition will
// override it but can never remove it. The list MUST NOT include
// duplicated parameters. A unique parameter is defined by a
// combination of a name and location. The list can use the Reference
// Object to link to parameters that are defined in the OpenAPI
// Object’s.
//
// - https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithOperationParameters(parameters ...*v3.Parameter) OperationOption {
	return func(c *operationConfig) {
		c.override.Parameters = append(c.override.Parameters, parameters...)
	}
}

// WithOperation uses a complete OpenAPI operation. Typed reflection
// is skipped, making the supplied operation authoritative.
func WithOperationBase(operation *v3.Operation) OperationOption {
	return func(c *operationConfig) {
		if operation == nil {
			panic("openapi operation is nil")
		}
		c.base = operation
	}
}

// WithOperationPatch merges a raw OpenAPI operation fragment —
// typically the output of mizucue's Schema.Operation — over the
// reflected operation. The fragment carries what reflection cannot
// infer per operation: request body media types, response tables.
// It parses at option construction and panics on malformed input.
// Local refs ("#/components/...") resolve against the final document
// at render time; construction indexes empty placeholders for them.
// Options apply in order: fields set by later options win over the
// patch.
func WithOperationPatch(raw []byte) OperationOption {
	// Parse the standalone fragment by wrapping it in a minimal
	// document, so libopenapi's full pipeline handles the nested
	// structures.
	var fragment map[string]any
	if err := json.Unmarshal(raw, &fragment); err != nil {
		panic(fmt.Sprintf("parse OpenAPI operation patch: %v", err))
	}
	// The fragment's local refs target the final document's
	// components, unknown here: index an empty placeholder per ref so
	// building the shell resolves. Proxies keep their $ref string
	// through merge and render, so placeholders never leak.
	refs := map[string]map[string]any{}
	var collect func(value any)
	collect = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, item := range typed {
				if key == "$ref" {
					if ref, ok := item.(string); ok {
						if rest, ok := strings.CutPrefix(ref, "#/components/"); ok {
							if kind, name, ok := strings.Cut(rest, "/"); ok && !strings.Contains(name, "/") {
								if refs[kind] == nil {
									refs[kind] = map[string]any{}
								}
								refs[kind][name] = map[string]any{}
							}
						}
					}
					continue
				}
				collect(item)
			}
		case []any:
			for _, item := range typed {
				collect(item)
			}
		}
	}
	collect(fragment)
	shell := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "patch", "version": "0"},
		"paths":   map[string]any{"/": map[string]any{"post": fragment}},
	}
	if len(refs) > 0 {
		shell["components"] = refs
	}
	wrapped, err := json.Marshal(shell)
	if err != nil {
		panic(fmt.Sprintf("parse OpenAPI operation patch: %v", err))
	}
	document, err := libopenapi.NewDocument(wrapped)
	if err != nil {
		panic(fmt.Sprintf("parse OpenAPI operation patch: %v", err))
	}
	model, err := document.BuildV3Model()
	if err != nil {
		panic(fmt.Sprintf("build OpenAPI operation patch: %v", err))
	}
	item, _ := model.Model.Paths.PathItems.Get("/")
	if item == nil || item.Post == nil {
		panic("parse OpenAPI operation patch: fragment is not an operation")
	}
	operation := item.Post
	return func(c *operationConfig) {
		c.override = internal.MergeOperation(c.override, operation)
	}
}

// WithOperationRequestBody replaces the reflected request body.
func WithOperationRequestBody(requestBody *v3.RequestBody) OperationOption {
	return func(c *operationConfig) {
		c.override.RequestBody = requestBody
	}
}

// WithOperationResponses replaces the reflected responses object.
func WithOperationResponses(responses *v3.Responses) OperationOption {
	return func(c *operationConfig) {
		c.override.Responses = responses
	}
}

// WithOperationExtensions adds specification extensions to the operation.
func WithOperationExtensions(extensions orderedmap.Map[string, any]) OperationOption {
	return func(c *operationConfig) {
		c.override.Extensions = internal.CanonicalExtensions(extensions)
	}
}

// WithResponse adds or replaces one response by status code.
func WithOperationResponse(code int, response *v3.Response) OperationOption {
	return func(c *operationConfig) {
		if code < 100 || code > 599 {
			panic("invalid HTTP response status " + strconv.Itoa(code))
		}
		if response == nil {
			panic("response for status %d is nil" + strconv.Itoa(code))
		}
		if c.override.Responses == nil {
			c.override.Responses = &v3.Responses{Codes: orderedmap.New[string, *v3.Response]()}
		}
		if c.override.Responses.Codes == nil {
			c.override.Responses.Codes = orderedmap.New[string, *v3.Response]()
		}
		c.override.Responses.Codes.Set(strconv.Itoa(code), response)
	}
}

// WithOperationCallback adds a callback to the operation. A possible
// out-of band callbacks related to the parent operation. The key is
// a unique identifier for the Callback Object. Value is a Callback
// Object that describes a request that may be initiated by the API
// provider and the expected responses.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationCallback(key string, value *v3.Callback) OperationOption {
	return func(c *operationConfig) {
		if c.override.Callbacks == nil {
			c.override.Callbacks = orderedmap.New[string, *v3.Callback]()
		}

		c.override.Callbacks.Set(key, value)
	}
}

// WithOperationDeprecated marks the operation as deprecated.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationDeprecated() OperationOption {
	return func(c *operationConfig) {
		if c.override.Deprecated == nil {
			c.override.Deprecated = new(bool)
		}
		*c.override.Deprecated = true
	}
}

// WithOperationSecurity adds security requirements to the operation.
// Each name MUST correspond to a security scheme which is declared in
// the Security Schemes under the Components Object.
//
// - https://spec.openapis.org/oas/v3.0.4.html#security-requirement-object
func WithOperationSecurity(requirement map[string][]string) OperationOption {
	var containEmpty bool
	for _, v := range requirement {
		if len(v) == 0 {
			containEmpty = true
			break
		}
	}
	return func(c *operationConfig) {
		c.override.Security = append(c.override.Security, &base.SecurityRequirement{
			ContainsEmptyRequirement: containEmpty,
			Requirements:             orderedmap.ToOrderedMap(requirement),
		})
	}
}

// WithOperationServer adds an Server Objects to the operation. An
// alternative servers array to service this operation. If a servers
// array is specified at the Path Item Object or OpenAPI Object level,
// it will be overridden by this value.
//
// - https://spec.openapis.org/oas/v3.0.4.html#server-object
func WithOperationServer(url string, desc string, variables map[string]*v3.ServerVariable,
	extensions ...orderedmap.Map[string, any],
) OperationOption {
	var firstExtensions orderedmap.Map[string, any]
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *operationConfig) {
		c.override.Servers = append(c.override.Servers, &v3.Server{
			URL:         url,
			Description: desc,
			Variables:   orderedmap.ToOrderedMap(variables),
			Extensions:  internal.CanonicalExtensions(firstExtensions),
		})
	}
}
