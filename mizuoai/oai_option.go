package mizuoai

import (
	"log"
	"reflect"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"gopkg.in/yaml.v3"
)

// INFO: mizuoai only support OPENAPI v3.1.0 (version is not
// customizable), but still compatible with OpenAPI v3.0.x

// --------------------------------------------------------------
// OpenAPI Options

type OaiOption func(*oaiConfig)

// oaiConfig holds the configuration for an OpenAPI Object. It is
// populated by the OaiOption functions and used to generate the
// OpenAPI specification. Version is fixed as 3.1.0.
//
// Each field corresponds to a field in the OpenAPI Object.
// See: https://swagger.io/specification/#operation-object
//
// WARN: components are ignored for now.
type oaiConfig struct {
	enableDoc                bool
	info                     *base.Info
	externalDocs             *base.ExternalDoc
	preLoaded                []byte
	tags                     []*base.Tag
	servers                  []*v3.Server
	handlers                 []*operationConfig
	securities               []*base.SecurityRequirement
	webhooks                 *orderedmap.Map[string, *v3.PathItem]
	jsonSchemaDialect        string
	componentSecuritySchemas *orderedmap.Map[string, *v3.SecurityScheme]
}

// WithOaiDocumentation enables to serve HTML OpenAPI
// documentation. It will be served at the same path as
// openapi.json
func WithOaiDocumentation() OaiOption {
	return func(c *oaiConfig) {
		c.enableDoc = true
	}
}

func WithOaiPreLoadSpec(content []byte) OaiOption {
	return func(c *oaiConfig) {
		c.preLoaded = content
	}
}

func WithOaiJsonSchemaDialect(dialect string) OaiOption {
	return func(c *oaiConfig) {
		c.jsonSchemaDialect = dialect
	}
}

// WithOaiServers adds an array of Server Objects, which provide
// connectivity information to a target server. If the servers
// field is not provided, or is an empty array, the default value
// would be a Server Object with a url value of /.
//
// See: https://swagger.io/specification/#operation-object
func WithOaiServer(url string, desc string, variables map[string]*v3.ServerVariable, extensions ...map[string]any,
) OaiOption {
	yamlExtensions := orderedmap.New[string, *yaml.Node]()
	if len(extensions) > 0 {
		for k, v := range extensions[0] {
			yamlb, _ := yaml.Marshal(v)
			node := &yaml.Node{}
			_ = yaml.Unmarshal(yamlb, node)
			yamlExtensions.Set(k, node)
		}
	}

	orderedVariables := orderedmap.New[string, *v3.ServerVariable]()
	for k, v := range variables {
		orderedVariables.Set(k, v)
	}
	return func(c *oaiConfig) {
		c.servers = append(c.servers, &v3.Server{
			URL:         url,
			Description: desc,
			Variables:   orderedVariables,
			Extensions:  yamlExtensions,
		})
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

func WithOaiSecuritySchema(identifier string, schema *v3.SecurityScheme) OaiOption {
	return func(c *oaiConfig) {
		if c.componentSecuritySchemas == nil {
			c.componentSecuritySchemas = orderedmap.New[string, *v3.SecurityScheme]()
		}
		c.componentSecuritySchemas.Set(identifier, schema)
	}
}

// WithOaiSecurity adds a security requirement to the operation.
//
// See: https://swagger.io/specification/#security-requirement-object
func WithOaiSecurity(requirement map[string][]string) OaiOption {
	return func(c *oaiConfig) {
		security := &base.SecurityRequirement{Requirements: orderedmap.New[string, []string]()}
		for k, v := range requirement {
			if _, ok := c.componentSecuritySchemas.Get(k); !ok {
				log.Fatalf("security schema %s not found", k)
			}
			security.Requirements.Set(k, v)
		}
		c.securities = append(c.securities, security)
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
func WithOaiTag(name string, desc string, externalDocs *base.ExternalDoc, extensions ...map[string]any) OaiOption {
	yamlExtensions := orderedmap.New[string, *yaml.Node]()
	if len(extensions) > 0 {
		for k, v := range extensions[0] {
			yamlb, _ := yaml.Marshal(v)
			node := &yaml.Node{}
			_ = yaml.Unmarshal(yamlb, node)
			yamlExtensions.Set(k, node)
		}
	}
	return func(c *oaiConfig) {
		c.tags = append(c.tags, &base.Tag{
			Name:         name,
			Description:  desc,
			ExternalDocs: externalDocs,
			Extensions:   yamlExtensions,
		})
	}
}

func WithOaiWebhook(webhookName string, pathItem *v3.PathItem) OaiOption {
	return func(c *oaiConfig) {
		c.webhooks.Set(webhookName, pathItem)
	}
}

// --------------------------------------------------------------
// Operation Options

type OperationOption func(*operationConfig)

type operationConfig struct {
	v3.Operation
	path                       string
	method                     string
	responseCode               int
	responseHeaders            *orderedmap.Map[string, *v3.Header]
	responseLinks              *orderedmap.Map[string, *v3.Link]
	responseDescription        string
	extraResponses             map[int]*v3.Response
	callbacks                  *orderedmap.Map[string, *v3.Callback]
	getComponentSecuritySchema func(string) (*v3.SecurityScheme, bool)
}

// WithOperationSummary provides a summary of what the operation
// does.
//
// See: https://swagger.io/specification/#operation-object
func WithOperationSummary(summary string) OperationOption {
	return func(c *operationConfig) {
		c.Summary = summary
	}
}

// WithOperationDescription provides a verbose explanation of the
// operation behavior. CommonMark syntax MAY be used for rich
// text representation.
//
// See: https://swagger.io/specification/#operation-object
func WithOperationDescription(description string) OperationOption {
	return func(c *operationConfig) {
		c.Description = description
	}
}

// WithOperationTags adds tags to the operation, for logical
// grouping of operations.
//
// See: https://swagger.io/specification/#operation-object
func WithOperationTags(tags ...string) OperationOption {
	return func(c *operationConfig) {
		c.Tags = tags
	}
}

// WithOperationExternalDocs provides a reference to an external
// resource for extended documentation.
//
// See: https://swagger.io/specification/#external-documentation-object
func WithOperationExternalDocs(url string, description string) OperationOption {
	return func(c *operationConfig) {
		c.ExternalDocs = &base.ExternalDoc{
			URL:         url,
			Description: description,
		}
	}
}

// WithOperationOperationId provides a unique string used to
// identify the operation. The id MUST be unique among all
// operations described in the API.
//
// See: https://swagger.io/specification/#operation-object
func WithOperationOperationId(operationId string) OperationOption {
	return func(c *operationConfig) {
		c.OperationId = operationId
	}
}

// WithOperationDeprecated marks the operation as deprecated.
//
// See: https://swagger.io/specification/#operation-object
func WithOperationDeprecated() OperationOption {
	boolean := new(bool)
	*boolean = true
	return func(c *operationConfig) {
		c.Deprecated = boolean
	}
}

// WithOperationSecurity adds security requirements to the
// operation.
//
// See: https://swagger.io/specification/#security-requirement-object
func WithOperationSecurity(requirements ...map[string][]string) OperationOption {
	return func(c *operationConfig) {
		for _, req := range requirements {
			securityRequirement := &base.SecurityRequirement{
				Requirements: orderedmap.New[string, []string](),
			}
			for k, v := range req {
				if _, ok := c.getComponentSecuritySchema(k); !ok {
					log.Fatalf("security schema %s not found", k)
				}
				securityRequirement.Requirements.Set(k, v)
			}
			c.Security = append(c.Security, securityRequirement)
		}
	}
}

func WithOperationCallback(callbackName string, callback *v3.Callback) OperationOption {
	return func(c *operationConfig) {
		if c.callbacks == nil {
			c.callbacks = orderedmap.New[string, *v3.Callback]()
		}
		c.callbacks.Set(callbackName, callback)
	}
}

// WithOperationServers adds servers to the operation.
//
// See: https://swagger.io/specification/#server-object
func WithOperationServer(url string, desc string, variables map[string]*v3.ServerVariable, extensions ...map[string]any) OperationOption {
	yamlExtensions := orderedmap.New[string, *yaml.Node]()
	if len(extensions) > 0 {
		for k, v := range extensions[0] {
			yamlb, _ := yaml.Marshal(v)
			node := &yaml.Node{}
			_ = yaml.Unmarshal(yamlb, node)
			yamlExtensions.Set(k, node)
		}
	}

	orderedVariables := orderedmap.New[string, *v3.ServerVariable]()
	for k, v := range variables {
		orderedVariables.Set(k, v)
	}
	return func(c *operationConfig) {
		c.Servers = append(c.Servers, &v3.Server{
			URL:         url,
			Description: desc,
			Variables:   orderedVariables,
			Extensions:  yamlExtensions,
		})
	}
}

func WithOperationResponseCode(code int) OperationOption {
	return func(c *operationConfig) {
		c.responseCode = code
	}
}

func WithOperationResponseHeaders(headers map[string]*v3.Header) OperationOption {
	return func(c *operationConfig) {
		if c.responseHeaders == nil {
			c.responseHeaders = orderedmap.New[string, *v3.Header]()
		}
		for k, v := range headers {
			c.responseHeaders.Set(k, v)
		}
	}
}

func WithOperationResponseLinks(links map[string]*v3.Link) OperationOption {
	return func(c *operationConfig) {
		if c.responseLinks == nil {
			c.responseLinks = orderedmap.New[string, *v3.Link]()
		}
		for k, v := range links {
			c.responseLinks.Set(k, v)
		}
	}
}

func WithOperationResponseDescription(description string) OperationOption {
	return func(c *operationConfig) {
		c.responseDescription = description
	}
}

// WithOperationResponse sets extra responses of the operation.
//
// Multiple response schema is voodoo, mizuoai support it, but it
// is definitely not the right way to do it. Try your best to
// avoid involving this function.
func WithOperationResponse[T any](code int, contentType string, response *v3.Response) OperationOption {
	return func(c *operationConfig) {
		output := new(T)
		valOutput := reflect.ValueOf(output).Elem()
		typOutput := valOutput.Type()

		if contentType == "" {
			switch typOutput.Kind() {
			case reflect.String:
				contentType = "plain/text"
			default:
				contentType = "application/json"
			}
		}

		response.Content.Set(contentType, &v3.MediaType{Schema: createSchemaProxy(typOutput)})
		c.extraResponses[code] = response
	}
}

// --------------------------------------------------------------
// Path Options

// WARN: path-wise option is not supported, need mizu to impl
// GROUP
