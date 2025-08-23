package mizuoai

import (
	"github.com/getkin/kin-openapi/openapi3"
)

// INFO: mizuoai only support OPENAPI v3.0.4 (version is not
// customizable), but still compatible with OpenAPI v3.0.x

// --------------------------------------------------------------
// OpenAPI Options

type OaiOption func(*oaiConfig)

// oaiConfig holds the configuration for an OpenAPI Object. It is
// populated by the OaiOption functions and used to generate the
// OpenAPI specification. Version is fixed as 3.0.4.
//
// Each field corresponds to a field in the OpenAPI Object.
// See: https://spec.openapis.org/oas/v3.0.4.html#openapi-object
//
// WARN: components are ignored for now.
type oaiConfig struct {
	enableDoc bool
	pathDoc   string
	preLoaded *openapi3.T

	paths        openapi3.Paths
	info         openapi3.Info
	servers      []*openapi3.Server
	security     []openapi3.SecurityRequirement
	tags         []*openapi3.Tag
	externalDocs *openapi3.ExternalDocs
	extensions   map[string]any

	handlers []*operationConfig
}

// WithOaiDocumentation enables to serve HTML OpenAPI
// documentation. It will be served at the same path as
// openapi.json
func WithOaiDocumentation() OaiOption {
	return func(c *oaiConfig) {
		c.enableDoc = true
	}
}

// WithOaiServePath sets the path to serve openapi.json.
func WithOaiServePath(path string) OaiOption {
	return func(c *oaiConfig) {
		c.pathDoc = path
	}
}

func WithOaiPreLoadDoc(doc *openapi3.T) OaiOption {
	return func(c *oaiConfig) {
		c.preLoaded = doc
	}
}

// WithOaiDescription provides a verbose description of the API.
// CommonMark syntax MAY be used for rich text representation.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#info-object
func WithOaiDescription(description string) OaiOption {
	return func(c *oaiConfig) {
		c.info.Description = description
	}
}

// WithOaiTermsOfService provides a URL to the Terms of Service
// for the API. Must be in the form of URI.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#info-object
func WithOaiTermsOfService(url string) OaiOption {
	return func(c *oaiConfig) {
		c.info.TermsOfService = url
	}
}

// WithOaiContact provides contact information for the exposed
// API.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#contact-object
func WithOaiContact(name string, url string, email string, extensions ...map[string]any) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		c.info.Contact = &openapi3.Contact{Name: name, URL: url, Email: email, Extensions: firstExtensions}
	}
}

// WithOaiLicense provides the license information for the
// exposed API.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#license-object
func WithOaiLicense(name string, url string, extensions ...map[string]any) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		if name == "" {
			panic("name is required")
		}
		c.info.License = &openapi3.License{URL: url, Name: name, Extensions: firstExtensions}
	}
}

// WithOaiServers adds an array of Server Objects, which provide
// connectivity information to a target server. If the servers
// field is not provided, or is an empty array, the default value
// would be a Server Object with a url value of /.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#server-object
func WithOaiServer(url string, desc string, variables map[string]*openapi3.ServerVariable, extensions ...map[string]any,
) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		c.servers = append(c.servers, &openapi3.Server{
			URL:         url,
			Description: desc,
			Variables:   variables,
			Extensions:  firstExtensions,
		})
	}
}

// WithOaiSecurity adds a security requirement to the operation.
// Each name MUST correspond to a security scheme which is
// declared in the Security Schemes under the Components Object.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#security-requirement-object
func WithOaiSecurity(requirement map[string][]string) OaiOption {
	securityRequirement := openapi3.SecurityRequirement(requirement)
	return func(c *oaiConfig) {
		c.security = append(c.security, securityRequirement)
	}
}

// WithOaiTags adds tags to the operation.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#tag-object
func WithOaiTag(name string, desc string, externalDocs *openapi3.ExternalDocs, extensions ...map[string]any) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		c.tags = append(c.tags, &openapi3.Tag{
			Name:         name,
			Description:  desc,
			ExternalDocs: externalDocs,
			Extensions:   firstExtensions,
		})
	}
}

// WithOaiExternalDocs provides a reference to an external
// resource for extended documentation.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#external-documentation-object
func WithOaiExternalDocs(url string, description string, extensions ...map[string]any) OaiOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *oaiConfig) {
		c.externalDocs = &openapi3.ExternalDocs{URL: url, Description: description, Extensions: firstExtensions}
	}
}

// WithOaiExtensions adds extensions to the operation.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#openapi-object
func WithOaiExtensions(extensions map[string]any) OaiOption {
	return func(c *oaiConfig) {
		c.extensions = extensions
	}
}

// --------------------------------------------------------------
// Path Options
//
// WARN: $ref is not supported

type PathOption func(*pathConfig)

type pathConfig struct {
	openapi3.PathItem
}

// WithPathSummary adds a summary for the path. An optional. An
// optional string summary, intended to apply to all operations
// in this path.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathSummary(summary string) PathOption {
	return func(c *pathConfig) {
		c.Summary = summary
	}
}

// WithPathDescription adds a description for the path. An
// optional string summary, intended to apply to all operations
// in this path.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#path-item-object
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
// See: https://spec.openapis.org/oas/v3.0.4.html#server-object
func WithPathServer(url string, desc string, variables map[string]*openapi3.ServerVariable, extensions ...map[string]any,
) PathOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *pathConfig) {
		c.Servers = append(c.Servers, &openapi3.Server{
			URL:         url,
			Description: desc,
			Variables:   variables,
			Extensions:  firstExtensions,
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
// See: https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathParameters(parameters ...*openapi3.ParameterRef) PathOption {
	return func(c *pathConfig) {
		c.Parameters = parameters
	}
}

// WithPathExtensions adds extensions to the operation.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithPathExtensions(extensions ...map[string]any) PathOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *pathConfig) {
		c.Extensions = firstExtensions
	}
}

// --------------------------------------------------------------
// Operation Options

type OperationOption func(*operationConfig)

type operationConfig struct {
	openapi3.Operation
	responseCode    *int
	responseLinks   openapi3.Links
	responseHeaders openapi3.Headers

	path   string
	method string
}

// WithOperationTags adds tags to the operation, for logical
// grouping of operations.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationTags(tags ...string) OperationOption {
	return func(c *operationConfig) {
		c.Tags = tags
	}
}

// WithOperationSummary provides a summary of what the operation
// does.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationSummary(summary string) OperationOption {
	return func(c *operationConfig) {
		c.Summary = summary
	}
}

// WithOperationDescription provides a verbose explanation of the
// operation behavior. CommonMark syntax MAY be used for rich
// text representation.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationDescription(description string) OperationOption {
	return func(c *operationConfig) {
		c.Description = description
	}
}

// WithOperationExternalDocs provides a reference to an external
// resource for extended documentation.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#external-documentation-object
func WithOperationExternalDocs(url string, description string, extensions ...map[string]any) OperationOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *operationConfig) {
		c.ExternalDocs = &openapi3.ExternalDocs{URL: url, Description: description, Extensions: firstExtensions}
	}
}

// WithOperationOperationId provides a unique string used to
// identify the operation. Unique string used to identify the
// operation. The id MUST be unique among all operations
// described in the API. The operationId value is case-sensitive.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationOperationId(operationId string) OperationOption {
	return func(c *operationConfig) {
		c.OperationID = operationId
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
// See: https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func WithOperationParameters(parameters ...*openapi3.ParameterRef) OperationOption {
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
// See: https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationCallback(key string, value *openapi3.CallbackRef) OperationOption {
	return func(c *operationConfig) {
		c.Callbacks[key] = value
	}
}

// WithOperationDeprecated marks the operation as deprecated.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#operation-object
func WithOperationDeprecated() OperationOption {
	return func(c *operationConfig) {
		c.Deprecated = true
	}
}

// WithOperationSecurity adds security requirements to the
// operation. Each name MUST correspond to a security scheme
// which is declared in the Security Schemes under the
// Components Object.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#security-requirement-object
func WithOperationSecurity(requirement map[string][]string) OperationOption {
	securityRequirement := openapi3.SecurityRequirement(requirement)
	return func(c *operationConfig) {
		if c.Security == nil {
			c.Security = &openapi3.SecurityRequirements{securityRequirement}
			return
		}
		*c.Security = append(*c.Security, securityRequirement)
	}
}

// WithOperationServer adds an Server Objects to the operation.
// An alternative servers array to service this operation. If a
// servers array is specified at the Path Item Object or OpenAPI
// Object level, it will be overridden by this value.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#server-object
func WithOperationServer(url string, desc string, variables map[string]*openapi3.ServerVariable,
	extensions ...map[string]any,
) OperationOption {
	var firstExtensions map[string]any
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *operationConfig) {
		if c.Servers == nil {
			c.Servers = &openapi3.Servers{{
				URL:         url,
				Description: desc,
				Variables:   variables,
				Extensions:  firstExtensions,
			}}
			return
		}
		*c.Servers = append(*c.Servers, &openapi3.Server{
			URL:         url,
			Description: desc,
			Variables:   variables,
			Extensions:  firstExtensions,
		})
	}
}

// WithResponseOverride overrides the default response for the
// operation. Links and headers can be added if needed.
func WithResponseOverride(code int, links openapi3.Links, headers openapi3.Headers) OperationOption {
	return func(c *operationConfig) {
		c.responseCode = &code
		c.responseLinks = links
		c.responseHeaders = headers
	}
}
