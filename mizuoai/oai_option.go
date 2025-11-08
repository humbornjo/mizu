package mizuoai

import (
	"fmt"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// INFO: mizuoai only support OPENAPI v3.0.4 Spec (version is not
// customizable), but still compatible with OpenAPI v3.1.0, which
// means you can load v3.1.0 document.

// --------------------------------------------------------------
// OpenAPI Options

type OaiOption func(*oaiConfig)

// oaiConfig holds the configuration for an OpenAPI Object. It is
// populated by the OaiOption functions and used to generate the
// OpenAPI specification. Version is fixed as 3.0.4.
//
// Each field corresponds to a field in the OpenAPI Object.
// - https://spec.openapis.org/oas/v3.0.4.html#openapi-object
//
// WARN: components are ignored for now.
type oaiConfig struct {
	enableJson     bool
	enableDocument bool
	servePath      string
	baseModel      *v3.Document

	info         *base.Info
	tags         []*base.Tag
	servers      []*v3.Server
	externalDocs *base.ExternalDoc
	security     []*base.SecurityRequirement
	extensions   *orderedmap.Map[string, *yaml.Node]
	paths        v3.Paths

	handlers []*operationConfig
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
	document, err := libopenapi.NewDocument(data)
	if err != nil {
		fmt.Printf("🚨 [ERROR] Failed to load OpenAPI document: %s\n", err)
	}
	v3Model, err := document.BuildV3Model()
	if err != nil {
		fmt.Printf("🚨 [ERROR] Failed to build v3 model: %s\n", err)
	}
	return func(c *oaiConfig) {
		c.baseModel = &v3Model.Model
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

// WithOaiTermsOfService provides a URL to the Terms of Service
// for the API. Must be in the form of URI.
//
// - https://spec.openapis.org/oas/v3.0.4.html#info-object
func WithOaiTermsOfService(url string) OaiOption {
	return func(c *oaiConfig) {
		c.info.TermsOfService = url
	}
}

// WithOaiContact provides contact information for the exposed
// API.
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

// WithOaiLicense provides the license information for the
// exposed API.
//
// - https://spec.openapis.org/oas/v3.0.4.html#license-object
func WithOaiLicense(name string, url string, extensions ...map[string]any) OaiOption {
	if name == "" {
		fmt.Println("🚨 [ERROR] License name cannot be empty")
	}
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		c.info.License = &base.License{
			Name:       name,
			URL:        url,
			Extensions: convExtensions(firstExtensions),
		}
	}
}

// WithOaiServers adds an array of Server Objects, which provide
// connectivity information to a target server. If the servers
// field is not provided, or is an empty array, the default value
// would be a Server Object with a url value of /.
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

// WithOaiSecurity adds a security requirement to the operation.
// Each name MUST correspond to a security scheme which is
// declared in the Security Schemes under the Components Object.
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
		c.tags = append(c.tags, &base.Tag{
			Name:         name,
			Description:  desc,
			ExternalDocs: externalDocs,
			Extensions:   convExtensions(firstExtensions),
		})
	}
}

// WithOaiExternalDocs provides a reference to an external
// resource for extended documentation.
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
//
// WARN: $ref is not supported

type PathOption func(*pathConfig)

type pathConfig struct {
	v3.PathItem
}

// WithPathSummary adds a summary for the path. An optional. An
// optional string summary, intended to apply to all operations
// in this path.
//
// - https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathSummary(summary string) PathOption {
	return func(c *pathConfig) {
		c.Summary = summary
	}
}

// WithPathDescription adds a description for the path. An
// optional string summary, intended to apply to all operations
// in this path.
//
// - https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathDescription(desc string) PathOption {
	return func(c *pathConfig) {
		c.Description = desc
	}
}

// WithPathServers adds an Server Objects in Path Item Object,
// which provide connectivity information to a target server. If
// the servers field is not provided, or is an empty array, the
// default value would be a Server Object with a url value of /.
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
// parameters that are applicable for all the operations
// described under this path. These parameters can be overridden
// at the operation level, but cannot be removed there. The list
// MUST NOT include duplicated parameters. A unique parameter is
// defined by a combination of a name and location. The list can
// use the Reference Object to link to parameters that are
// defined in the OpenAPI Object’s components.parameters.
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

	path   string
	method string
}

// WithOperationTags adds tags to the operation, for logical
// grouping of operations.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationTags(tags ...string) OperationOption {
	return func(c *operationConfig) {
		c.Tags = tags
	}
}

// WithOperationSummary provides a summary of what the operation
// does.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationSummary(summary string) OperationOption {
	return func(c *operationConfig) {
		c.Summary = summary
	}
}

// WithOperationDescription provides a verbose explanation of the
// operation behavior. CommonMark syntax MAY be used for rich
// text representation.
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

// WithOperationOperationId provides a unique string used to
// identify the operation. Unique string used to identify the
// operation. The id MUST be unique among all operations
// described in the API. The operationId value is case-sensitive.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationOperationId(operationId string) OperationOption {
	return func(c *operationConfig) {
		c.OperationId = operationId
	}
}

// WithOperationParameters adds parameters to the operation. A
// list of parameters that are applicable for this operation. If
// a parameter is already defined in the Path Item, the new
// definition will override it but can never remove it. The list
// MUST NOT include duplicated parameters. A unique parameter is
// defined by a combination of a name and location. The list can
// use the Reference Object to link to parameters that are
// defined in the OpenAPI Object’s
//
// - https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithOperationParameters(parameters ...*v3.Parameter) OperationOption {
	return func(c *operationConfig) {
		c.Parameters = parameters
	}
}

// WithOperationCallback adds a callback to the operation. A
// possible out-of band callbacks related to the parent
// operation. The key is a unique identifier for the Callback
// Object. Value is a Callback Object that describes a request
// that may be initiated by the API provider and the expected
// responses.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationCallback(key string, value *v3.Callback) OperationOption {
	return func(c *operationConfig) {
		c.Callbacks.Set(key, value)
	}
}

// WithOperationDeprecated marks the operation as deprecated.
//
// - https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationDeprecated() OperationOption {
	return func(c *operationConfig) {
		*c.Deprecated = true
	}
}

// WithOperationSecurity adds security requirements to the
// operation. Each name MUST correspond to a security scheme
// which is declared in the Security Schemes under the
// Components Object.
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

// WithOperationServer adds an Server Objects to the operation.
// An alternative servers array to service this operation. If a
// servers array is specified at the Path Item Object or OpenAPI
// Object level, it will be overridden by this value.
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
		c.responseCode = &code
		c.responseLinks = links
		c.responseHeaders = headers
	}
}
