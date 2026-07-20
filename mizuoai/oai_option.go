package mizuoai

import (
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

const _OPENAPI_VERSION = "3.2.0"

// --------------------------------------------------------------
// OpenAPI Options

type OaiOption func(*oaiConfig)

// oaiConfig holds the configuration and registrations used to build an
// OpenAPI 3.2 document. OpenAPI 3.1 documents can be used as input.
type oaiConfig struct {
	mu             sync.Mutex
	err            error
	version        string
	enableJson     bool
	enableDocument bool
	servePath      string
	baseData       []byte
	self           string
	jsonDialect    string

	info          *base.Info
	tags          []*base.Tag
	servers       []*v3.Server
	externalDocs  *base.ExternalDoc
	security      []*base.SecurityRequirement
	extensions    *orderedmap.Map[string, *yaml.Node]
	paths         *orderedmap.Map[string, *v3.PathItem]
	components    *v3.Components
	reflector     *schemaReflector
	operationIds  map[string]string
	routes        map[string]bool
	rawComponents map[string]*yaml.Node
	webhooks      *orderedmap.Map[string, *v3.PathItem]

	handlers []*operationConfig
}

// WithOaiSelf sets the OpenAPI 3.2 $self URI used as the document base URI.
func WithOaiSelf(uri string) OaiOption {
	return func(c *oaiConfig) {
		c.self = uri
	}
}

// WithOaiJsonSchemaDialect sets the default JSON Schema dialect for Schema
// Objects in the document.
func WithOaiJsonSchemaDialect(uri string) OaiOption {
	return func(c *oaiConfig) {
		c.jsonDialect = uri
	}
}

// WithOaiVersion selects the output OpenAPI version. Versions 3.1.x and 3.2.x
// are supported. The default is 3.2.0.
func WithOaiVersion(version string) OaiOption {
	return func(c *oaiConfig) {
		if err := validateTargetVersion(version); err != nil {
			c.err = err
			return
		}
		c.version = version
	}
}

// WithOaiServePath sets the path to serve openapi.json.
func WithOaiServePath(path string) OaiOption {
	return func(c *oaiConfig) {
		c.servePath = path
	}
}

// WithOaiRenderJson use JSON rendering.
func WithOaiRenderJson() OaiOption {
	return func(c *oaiConfig) {
		c.enableJson = true
	}
}

// WithOaiDocumentation enables documentation generation.
func WithOaiDocumentation() OaiOption {
	return func(c *oaiConfig) {
		c.enableDocument = true
	}
}

// WithOaiPreLoad loads an OpenAPI document from data.
func WithOaiPreLoad(data []byte) OaiOption {
	return func(c *oaiConfig) {
		c.baseData = append([]byte(nil), data...)
	}
}

// WithOaiComponents adds reusable OpenAPI components to the generated
// document. Incompatible components with the same name are rejected.
func WithOaiComponents(components *v3.Components) OaiOption {
	return func(c *oaiConfig) {
		if components == nil {
			return
		}
		if err := mergeComponents(c.components, components); err != nil {
			c.err = err
		}
	}
}

// WithOaiSchemaName overrides the reflected component name for T.
func WithOaiSchemaName[T any](name string) OaiOption {
	return func(c *oaiConfig) {
		if err := c.reflector.setName(reflect.TypeFor[T](), name); err != nil {
			c.err = err
		}
	}
}

// WithOaiDescription provides a verbose description of the API.
// CommonMark syntax MAY be used for rich text representation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#info-object
func WithOaiDescription(description string) OaiOption {
	return func(c *oaiConfig) {
		c.info.Description = description
	}
}

// WithOaiSummary provides the OpenAPI 3.2 API summary.
func WithOaiSummary(summary string) OaiOption {
	return func(c *oaiConfig) {
		c.info.Summary = summary
	}
}

// WithOaiInfoVersion sets the version of the described API.
func WithOaiInfoVersion(version string) OaiOption {
	return func(c *oaiConfig) {
		c.info.Version = version
	}
}

// WithOaiInfo supplies a complete OpenAPI Info Object. Initialize's title is
// retained when info does not specify one.
func WithOaiInfo(info *base.Info) OaiOption {
	return func(c *oaiConfig) {
		if info == nil {
			c.err = fmt.Errorf("OpenAPI info is nil")
			return
		}
		title := c.info.Title
		copied := *info
		c.info = &copied
		if c.info.Title == "" {
			c.info.Title = title
		}
	}
}

// WithOaiTermsOfService provides a URL to the Terms of Service for
// the API. Must be in the form of URI.
//
// - https://spec.openapis.org/oas/v3.0.4.html#info-object
func WithOaiTermsOfService(url string) OaiOption {
	return func(c *oaiConfig) {
		c.info.TermsOfService = url
	}
}

// WithOaiContact provides contact information for the exposed API.
//
// - https://spec.openapis.org/oas/v3.0.4.html#contact-object
func WithOaiContact(name string, url string, email string, extensions ...map[string]any) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}

	return func(c *oaiConfig) {
		c.info.Contact = &base.Contact{
			Name:       name,
			URL:        url,
			Email:      email,
			Extensions: convExtensions(firstExtensions),
		}
	}
}

// WithOaiLicense provides the license information for the exposed API.
//
// - https://spec.openapis.org/oas/v3.0.4.html#license-object
func WithOaiLicense(name string, url string, extensions ...map[string]any) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		if name == "" {
			c.err = fmt.Errorf("OpenAPI license name cannot be empty")
			return
		}
		c.info.License = &base.License{
			Name:       name,
			URL:        url,
			Extensions: convExtensions(firstExtensions),
		}
	}
}

// WithOaiWebhook adds an OpenAPI 3.1+ webhook Path Item.
func WithOaiWebhook(name string, item *v3.PathItem) OaiOption {
	return func(c *oaiConfig) {
		if name == "" || item == nil {
			c.err = fmt.Errorf("OpenAPI webhook name and path item are required")
			return
		}
		if previous, ok := c.webhooks.Get(name); ok {
			equal, err := semanticEqual(previous, item)
			if err != nil || !equal {
				c.err = fmt.Errorf("incompatible OpenAPI webhook collision at %s", name)
			}
			return
		}
		c.webhooks.Set(name, item)
	}
}

// WithOaiSecurityScheme adds a reusable security scheme component.
func WithOaiSecurityScheme(name string, scheme *v3.SecurityScheme) OaiOption {
	return func(c *oaiConfig) {
		if name == "" || scheme == nil {
			c.err = fmt.Errorf("OpenAPI security scheme name and value are required")
			return
		}
		components := newComponents()
		components.SecuritySchemes.Set(name, scheme)
		if err := mergeComponents(c.components, components); err != nil {
			c.err = err
		}
	}
}

// WithOaiServers adds an array of Server Objects, which provide
// connectivity information to a target server. If the servers field
// is not provided, or is an empty array, the default value would be a
// Server Object with a url value of /.
//
// - https://spec.openapis.org/oas/v3.0.4.html#server-object
func WithOaiServer(url string, desc string, variables map[string]*v3.ServerVariable, extensions ...map[string]any,
) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}

	return func(c *oaiConfig) {
		c.servers = append(c.servers, &v3.Server{
			URL:         url,
			Description: desc,
			Variables:   orderedmap.ToOrderedMap(variables),
			Extensions:  convExtensions(firstExtensions),
		})
	}
}

// WithOaiServerObject adds a complete Server Object.
func WithOaiServerObject(server *v3.Server) OaiOption {
	return func(c *oaiConfig) {
		if server == nil {
			c.err = fmt.Errorf("OpenAPI server is nil")
			return
		}
		c.servers = append(c.servers, server)
	}
}

// WithOaiSecurity adds a security requirement to the operation. Each
// name MUST correspond to a security scheme which is declared in the
// Security Schemes under the Components Object.
//
// - https://spec.openapis.org/oas/v3.0.4.html#security-requirement-object
func WithOaiSecurity(requirement map[string][]string) OaiOption {
	var containEmpty bool
	for _, v := range requirement {
		if len(v) == 0 {
			containEmpty = true
			break
		}
	}
	return func(c *oaiConfig) {
		c.security = append(c.security, &base.SecurityRequirement{
			ContainsEmptyRequirement: containEmpty,
			Requirements:             orderedmap.ToOrderedMap(requirement),
		})
	}
}

// WithOaiTags adds tags to the operation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#tag-object
func WithOaiTag(name string, desc string, externalDocs *base.ExternalDoc, extensions ...map[string]any) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		tag := &base.Tag{
			Name:         name,
			Description:  desc,
			ExternalDocs: externalDocs,
			Extensions:   convExtensions(firstExtensions),
		}
		if name == "" {
			c.err = fmt.Errorf("OpenAPI tag name is required")
			return
		}
		if err := mergeTags(&c.tags, []*base.Tag{tag}); err != nil {
			c.err = err
		}
	}
}

// WithOaiTagObject adds a complete Tag Object.
func WithOaiTagObject(tag *base.Tag) OaiOption {
	return func(c *oaiConfig) {
		if tag == nil {
			c.err = fmt.Errorf("OpenAPI tag is nil")
			return
		}
		if tag.Name == "" {
			c.err = fmt.Errorf("OpenAPI tag name is required")
			return
		}
		if err := mergeTags(&c.tags, []*base.Tag{tag}); err != nil {
			c.err = err
		}
	}
}

// WithOaiExternalDocs provides a reference to an external resource
// for extended documentation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#external-documentation-object
func WithOaiExternalDocs(url string, description string, extensions ...map[string]any) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		c.externalDocs = &base.ExternalDoc{
			Description: description,
			URL:         url,
			Extensions:  convExtensions(firstExtensions),
		}
	}
}

// WithOaiExtensions adds extensions to the operation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#openapi-object
func WithOaiExtensions(extensions map[string]any) OaiOption {
	return func(c *oaiConfig) {
		c.extensions = convExtensions(extensions)
	}
}

// --------------------------------------------------------------
// Path Options
type PathOption func(*pathConfig)

type pathConfig struct {
	v3.PathItem
}

// WithPathItem supplies a complete OpenAPI Path Item Object.
func WithPathItem(item *v3.PathItem) PathOption {
	return func(c *pathConfig) {
		if item != nil {
			c.PathItem = *item
		}
	}
}

// WithPathReference sets the Path Item Object reference.
func WithPathReference(reference string) PathOption {
	return func(c *pathConfig) {
		c.Reference = reference
	}
}

// WithPathSummary adds a summary for the path. An optional. An
// optional string summary, intended to apply to all operations in
// this path.
//
// - https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathSummary(summary string) PathOption {
	return func(c *pathConfig) {
		c.Summary = summary
	}
}

// WithPathDescription adds a description for the path. An optional
// string summary, intended to apply to all operations in this path.
//
// - https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathDescription(desc string) PathOption {
	return func(c *pathConfig) {
		c.Description = desc
	}
}

// WithPathServers adds an Server Objects in Path Item Object, which
// provide connectivity information to a target server. If the servers
// field is not provided, or is an empty array, the default value
// would be a Server Object with a url value of /.
//
// - https://spec.openapis.org/oas/v3.0.4.html#server-object
func WithPathServer(url string, desc string, variables map[string]*v3.ServerVariable, extensions ...map[string]any,
) PathOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *pathConfig) {
		c.Servers = append(c.Servers, &v3.Server{
			URL:         url,
			Description: desc,
			Variables:   orderedmap.ToOrderedMap(variables),
			Extensions:  convExtensions(firstExtensions),
		})
	}
}

// WithPathParameters adds parameters to the path. A list of
// parameters that are applicable for all the operations described
// under this path. These parameters can be overridden at the
// operation level, but cannot be removed there. The list MUST NOT
// include duplicated parameters. A unique parameter is defined by a
// combination of a name and location. The list can use the Reference
// Object to link to parameters that are defined in the OpenAPI
// Object’s components.parameters.
//
// - https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathParameters(parameters ...*v3.Parameter) PathOption {
	return func(c *pathConfig) {
		c.Parameters = parameters
	}
}

// WithPathExtensions adds extensions to the operation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathExtensions(extensions ...map[string]any) PathOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *pathConfig) {
		c.Extensions = convExtensions(firstExtensions)
	}
}

// --------------------------------------------------------------
// Operation Options

type OperationOption func(*operationConfig)

type operationConfig struct {
	v3.Operation
	responseCode    *int
	responseLinks   map[string]*v3.Link
	responseHeaders map[string]*v3.Header
	requestBodySet  bool
	responsesSet    bool
	external        *documentOperation
	components      *v3.Components
	pathItem        *v3.PathItem
	documentTags    []*base.Tag
	err             error

	path   string
	method string
}

// WithOperationTags adds tags to the operation, for logical grouping
// of operations.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationTags(tags ...string) OperationOption {
	return func(c *operationConfig) {
		for _, tag := range tags {
			if !slices.Contains(c.Tags, tag) {
				c.Tags = append(c.Tags, tag)
			}
		}
	}
}

// WithOperationSummary provides a summary of what the operation does.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationSummary(summary string) OperationOption {
	return func(c *operationConfig) {
		c.Summary = summary
	}
}

// WithOperationDescription provides a verbose explanation of the
// operation behavior. CommonMark syntax MAY be used for rich text
// representation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationDescription(description string) OperationOption {
	return func(c *operationConfig) {
		c.Description = description
	}
}

// WithOperationExternalDocs provides a reference to an external
// resource for extended documentation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#external-documentation-object
func WithOperationExternalDocs(url string, description string, extensions ...map[string]any) OperationOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *operationConfig) {
		c.ExternalDocs = &base.ExternalDoc{
			Description: description,
			URL:         url,
			Extensions:  convExtensions(firstExtensions),
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
		c.OperationId = operationId
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
		merged, err := mergeParameters(c.Parameters, parameters)
		if err != nil {
			c.err = err
			return
		}
		c.Parameters = merged
	}
}

// WithOperation uses a complete OpenAPI operation. Typed reflection is
// skipped, making the supplied operation authoritative.
func WithOperation(operation *v3.Operation) OperationOption {
	return func(c *operationConfig) {
		if operation == nil {
			c.err = fmt.Errorf("openapi operation is nil")
			return
		}
		if c.external != nil {
			c.err = fmt.Errorf("multiple authoritative OpenAPI operations supplied")
			return
		}
		merged, err := supplementOperation(operation, &c.Operation)
		if err != nil {
			c.err = err
			return
		}
		c.Operation = *merged
		c.external = &documentOperation{operation: operation}
	}
}

// WithOpenApiOperation selects an authoritative operation by operationId from
// a parsed OpenAPI 3.0, 3.1, or 3.2 document.
func WithOpenApiOperation(document *OpenApiDocument, operationId string) OperationOption {
	return func(c *operationConfig) {
		if document == nil {
			c.err = fmt.Errorf("openapi document is nil")
			return
		}
		if c.external != nil {
			c.err = fmt.Errorf("multiple authoritative OpenAPI operations supplied")
			return
		}
		operation, ok := document.operations[operationId]
		if !ok {
			c.err = fmt.Errorf("openapi operation %q not found", operationId)
			return
		}
		merged, err := supplementOperation(operation.operation, &c.Operation)
		if err != nil {
			c.err = err
			return
		}
		c.external = operation
		c.Operation = *merged
		c.components = operation.components
		c.pathItem = operation.pathItem
		c.documentTags = operation.tags
	}
}

// WithOperationRequestBody replaces the reflected request body.
func WithOperationRequestBody(requestBody *v3.RequestBody) OperationOption {
	return func(c *operationConfig) {
		c.RequestBody = requestBody
		c.requestBodySet = true
	}
}

// WithOperationResponses replaces the reflected responses object.
func WithOperationResponses(responses *v3.Responses) OperationOption {
	return func(c *operationConfig) {
		c.Responses = responses
		c.responsesSet = true
	}
}

// WithOperationExtensions adds specification extensions to the operation.
func WithOperationExtensions(extensions map[string]any) OperationOption {
	return func(c *operationConfig) {
		merged, err := mergeNamedValues("extension", c.Extensions, convExtensions(extensions))
		if err != nil {
			c.err = err
			return
		}
		c.Extensions = merged
	}
}

// WithResponse adds or replaces one response by status code.
func WithResponse(code int, response *v3.Response) OperationOption {
	return func(c *operationConfig) {
		if code < 100 || code > 599 {
			c.err = fmt.Errorf("invalid HTTP response status %d", code)
			return
		}
		if response == nil {
			c.err = fmt.Errorf("response for status %d is nil", code)
			return
		}
		if c.Responses == nil {
			c.Responses = &v3.Responses{Codes: orderedmap.New[string, *v3.Response]()}
		}
		if c.Responses.Codes == nil {
			c.Responses.Codes = orderedmap.New[string, *v3.Response]()
		}
		c.Responses.Codes.Set(fmt.Sprintf("%d", code), response)
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
		if c.Callbacks == nil {
			c.Callbacks = orderedmap.New[string, *v3.Callback]()
		}
		if previous, ok := c.Callbacks.Get(key); ok {
			equal, err := semanticEqual(previous, value)
			if err != nil || !equal {
				c.err = fmt.Errorf("conflicting OpenAPI operation callback %s", key)
			}
			return
		}
		c.Callbacks.Set(key, value)
	}
}

// WithOperationDeprecated marks the operation as deprecated.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationDeprecated() OperationOption {
	return func(c *operationConfig) {
		if c.Deprecated == nil {
			c.Deprecated = new(bool)
		}
		*c.Deprecated = true
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
		c.Security = append(c.Security, &base.SecurityRequirement{
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
	extensions ...map[string]any,
) OperationOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *operationConfig) {
		c.Servers = append(c.Servers, &v3.Server{
			URL:         url,
			Description: desc,
			Variables:   orderedmap.ToOrderedMap(variables),
			Extensions:  convExtensions(firstExtensions),
		})
	}
}

// WithResponseOverride overrides the default response for the
// operation. Links and headers can be added if needed.
func WithResponseOverride(code int, links map[string]*v3.Link, headers map[string]*v3.Header) OperationOption {
	return func(c *operationConfig) {
		if code < 100 || code > 599 {
			c.err = fmt.Errorf("invalid HTTP response status %d", code)
			return
		}
		c.responseCode = &code
		c.responseLinks = links
		c.responseHeaders = headers
	}
}
