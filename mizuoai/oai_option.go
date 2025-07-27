package mizuoai

import (
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

type OaiOption func(*oaiConfig)

// oaiConfig holds the configuration for an OpenAPI Object. It is
// populated by the OaiOption functions and used to generate the
// OpenAPI specification. Version is fixed as 3.1.0.
//
// Each field corresponds to a field in the OpenAPI Object.
// See: https://swagger.io/specification/#operation-object
//
// WARN: webhooks, jsonSchemaDialect and components are ignored
// for now.
type oaiConfig struct {
	enableDoc    bool
	tags         []base.Tag
	servers      []*v3.Server
	info         *base.Info
	security     *base.SecurityRequirement
	externalDocs *base.ExternalDoc
}

// WithOaiDocumentation enables to serve HTML OpenAPI
// documentation. It will be served at the same path as
// openapi.json
func WithOaiDocumentation() OaiOption {
	return func(c *oaiConfig) {
		c.enableDoc = true
	}
}

// WithOaiServers adds an array of Server Objects, which provide
// connectivity information to a target server. If the servers
// field is not provided, or is an empty array, the default value
// would be a Server Object with a url value of /.
//
// See: https://swagger.io/specification/#operation-object
func WithOaiServers(servers ...*v3.Server) OaiOption {
	return func(c *oaiConfig) {
		c.servers = append(c.servers, servers...)
	}
}

// WithOaiTitle sets the title of the operation. Title is
// required to describe API.
//
// See: https://swagger.io/specification/#info-object
func WithOaiTitle(title string) OaiOption {
	return func(c *oaiConfig) {
		if c.info == nil {
			c.info = &base.Info{}
		}
		c.info.Title = title
	}
}

// WithOaiSummary provides a short summary of about API.
//
// See: https://swagger.io/specification/#info-object
func WithOaiSummary(summary string) OaiOption {
	return func(c *oaiConfig) {
		if c.info == nil {
			c.info = &base.Info{}
		}
		c.info.Summary = summary
	}
}

// WithOaiDescription provides a verbose description of the API.
// CommonMark syntax MAY be used for rich text representation.
//
// See: https://swagger.io/specification/#info-object
func WithOaiDescription(description string) OaiOption {
	return func(c *oaiConfig) {
		if c.info == nil {
			c.info = &base.Info{}
		}
		c.info.Description = description
	}
}

// WithOaiTermsOfService provides a URL to the Terms of Service
// for the API. Must be in the form of URI.
//
// See: https://swagger.io/specification/#info-object
func WithOaiTermsOfService(url string) OaiOption {
	return func(c *oaiConfig) {
		if c.info == nil {
			c.info = &base.Info{}
		}
		c.info.TermsOfService = url
	}
}

// WithOaiContact provides contact information for the exposed
// API.
//
// See: https://swagger.io/specification/#info-object
func WithOaiContact(name, email, url string) OaiOption {
	return func(c *oaiConfig) {
		if c.info == nil {
			c.info = &base.Info{}
		}
		c.info.Contact = &base.Contact{
			Name:  name,
			Email: email,
			URL:   url,
		}
	}
}

// WithOaiLicense provides the license information for the
// exposed API.
//
// See: https://swagger.io/specification/#license-object
func WithOaiLicense(name string, identifier string, url string) OaiOption {
	return func(c *oaiConfig) {
		if name == "" {
			panic("name is required")
		}
		if c.info == nil {
			c.info = &base.Info{}
		}
		c.info.License = &base.License{
			URL:        url,
			Name:       name,
			Identifier: identifier,
		}
	}
}

// TODO: DELETE?
// WithOaiVersion sets the version of the API. It is required. The
// version of the OpenAPI Document (which is distinct from the
// OpenAPI Specification version or the version of the API being
// described or the version of the OpenAPI Description).
//
// See: https://swagger.io/specification/#info-object
func WithOaiVersion(version string) OaiOption {
	return func(c *oaiConfig) {
		if c.info == nil {
			c.info = &base.Info{}
		}
		c.info.Version = version
	}
}

// WithOaiSecurity adds a security requirement to the operation.
//
// See: https://swagger.io/specification/#security-requirement-object
func WithOaiSecurity(requirement map[string][]string) OaiOption {
	return func(c *oaiConfig) {
		if c.security == nil {
			c.security = &base.SecurityRequirement{Requirements: orderedmap.New[string, []string]()}
		}
		for k, v := range requirement {
			c.security.Requirements.Set(k, v)
		}
	}
}

// WithOaiExternalDocs provides a reference to an external
// resource for extended documentation.
//
// See: https://swagger.io/specification/#external-documentation-object
func WithOaiExternalDocs(url string, description string) OaiOption {
	return func(c *oaiConfig) {
		c.externalDocs = &base.ExternalDoc{
			URL:         url,
			Description: description,
		}
	}
}

// WithOaiTags adds tags to the operation.
//
// See: https://swagger.io/specification/#tag-object
func WithOaiTags(tags ...base.Tag) OaiOption {
	return func(c *oaiConfig) {
		c.tags = tags
	}
}

// --------------------------------------------------------------

// WARN: path-wise option is not supported, need mizu to impl
// GROUP

type HandlerOption func(*handlerConfig)

// WARN: Callback is not supported, parameters, request body and
// responses will be generated automatically.
type handlerConfig v3.Operation

// WithHandlerSummary provides a summary of what the operation
// does.
//
// See: https://swagger.io/specification/#operation-object
func WithHandlerSummary(summary string) HandlerOption {
	return func(c *handlerConfig) {
		c.Summary = summary
	}
}

// WithHandlerDescription provides a verbose explanation of the
// operation behavior. CommonMark syntax MAY be used for rich
// text representation.
//
// See: https://swagger.io/specification/#operation-object
func WithHandlerDescription(description string) HandlerOption {
	return func(c *handlerConfig) {
		c.Description = description
	}
}

// WithHandlerTags adds tags to the operation, for logical
// grouping of operations.
//
// See: https://swagger.io/specification/#operation-object
func WithHandlerTags(tags ...string) HandlerOption {
	return func(c *handlerConfig) {
		c.Tags = tags
	}
}

// WithHandlerExternalDocs provides a reference to an external
// resource for extended documentation.
//
// See: https://swagger.io/specification/#external-documentation-object
func WithHandlerExternalDocs(url string, description string) HandlerOption {
	return func(c *handlerConfig) {
		c.ExternalDocs = &base.ExternalDoc{
			URL:         url,
			Description: description,
		}
	}
}

// WithHandlerOperationId provides a unique string used to
// identify the operation. The id MUST be unique among all
// operations described in the API.
//
// See: https://swagger.io/specification/#operation-object
func WithHandlerOperationId(operationId string) HandlerOption {
	return func(c *handlerConfig) {
		c.OperationId = operationId
	}
}

// WithHandlerDeprecated marks the operation as deprecated.
//
// See: https://swagger.io/specification/#operation-object
func WithHandlerDeprecated() HandlerOption {
	boolean := new(bool)
	*boolean = true
	return func(c *handlerConfig) {
		c.Deprecated = boolean
	}
}

// WithHandlerSecurity adds security requirements to the
// operation.
//
// See: https://swagger.io/specification/#security-requirement-object
func WithHandlerSecurity(requirements ...map[string][]string) HandlerOption {
	return func(c *handlerConfig) {
		for _, req := range requirements {
			securityRequirement := &base.SecurityRequirement{
				Requirements: orderedmap.New[string, []string](),
			}
			for k, v := range req {
				securityRequirement.Requirements.Set(k, v)
			}
			c.Security = append(c.Security, securityRequirement)
		}
	}
}

// WithHandlerServers adds servers to the operation.
//
// See: https://swagger.io/specification/#server-object
func WithHandlerServers(servers ...*v3.Server) HandlerOption {
	return func(c *handlerConfig) {
		c.Servers = append(c.Servers, servers...)
	}
}
