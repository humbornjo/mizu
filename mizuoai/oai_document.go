package mizuoai

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/humbornjo/mizu/mizuoai/internal"
	"github.com/humbornjo/mizu/mizuoai/internal/serde"
)

const _OPENAPI_VERSION = "3.2.0"

type RenderFormat string

const (
	_RENDER_FORMAT_AUTO RenderFormat = "auto"
	_RENDER_FORMAT_JSON RenderFormat = "json"
	_RENDER_FORMAT_YAML RenderFormat = "yaml"
)

type DocumentOption func(*documentConfig)

// documentConfig holds the configuration and registrations used to
// build an OpenAPI 3.2 document. OpenAPI 3.1 documents can be used as
// input.
type documentConfig struct {
	mu sync.Mutex

	route        string
	renderHTML   bool
	renderFormat RenderFormat

	// base is the document under construction — registrations
	// (schemas, operations) accumulate here, starting empty or
	// from a preloaded document.
	base *v3.Document
	// override carries user-supplied document options, overlaid
	// on base at render.
	override *v3.Document

	overridedNames  map[string]string
	patchedSchemas  map[string]string
	operationIds    map[string]string
	operationRoutes map[string]bool
}

// FromRaw builds the base document from a raw OpenAPI document,
// panicking when data cannot be parsed.
func (c *documentConfig) FromRaw(data []byte) {
	document, err := libopenapi.NewDocument(data)
	if err != nil {
		panic(fmt.Sprintf("parse OpenAPI base document: %v", err))
	}
	model, err := document.BuildV3Model()
	if err != nil {
		panic(fmt.Sprintf("build OpenAPI base document: %v", err))
	}
	c.base = &model.Model
	// Registration targets must stay non-nil.
	if c.base.Paths == nil {
		c.base.Paths = &v3.Paths{}
	}
	if c.base.Paths.PathItems == nil {
		c.base.Paths.PathItems = orderedmap.New[string, *v3.PathItem]()
	}
	if c.base.Components == nil {
		c.base.Components = internal.NewComponents()
	}
	if c.base.Webhooks == nil {
		c.base.Webhooks = orderedmap.New[string, *v3.PathItem]()
	}
}

// ExtractOperation builds the OpenAPI operation for a handler with
// the given request and response types, following serde tag
// conventions: input fields carry a json tag marking their location
// (path|query|header|body|form) and group fields are structs.
func (c *documentConfig) ExtractOperation(input reflect.Type, output reflect.Type) *v3.Operation {
	// (name, in) is the dedup key when merging user-supplied
	// parameters later.
	operation := &v3.Operation{
		Parameters: internal.CanonicalParameters(input),
	}

	for field := range input.Fields() {
		if field.PkgPath != "" {
			continue
		}
		jsonTagValue, _ := serde.JsonField(field)
		var mediaType string
		switch serde.TagType(jsonTagValue) {
		case serde.TAG_BODY:
			mediaType = "application/json"
		case serde.TAG_FORM:
			// TODO: multipart/form-data when a []byte file field
			// carries a contentType tag.
			mediaType = "application/x-www-form-urlencoded"
		default:
			continue
		}
		if operation.RequestBody != nil {
			panic(fmt.Sprintf("multiple request body fields in %s", input))
		}
		required := true // serde always decodes the body
		operation.RequestBody = &v3.RequestBody{
			Required: &required,
			Content: orderedmap.ToOrderedMap(map[string]*v3.MediaType{
				mediaType: {Schema: c.RegisterSchema(field.Type)},
			}),
		}
	}

	// Media type mirrors serde.Encode: a string output encodes as
	// text/plain, anything else as application/json.
	mediaType := "application/json"
	if output.Kind() == reflect.String {
		mediaType = "text/plain"
	}
	operation.Responses = &v3.Responses{
		Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
			"200": {
				Description: "OK",
				Content: orderedmap.ToOrderedMap(map[string]*v3.MediaType{
					mediaType: {Schema: c.RegisterSchema(output)},
				}),
			},
		}),
	}

	return operation
}

// RegisterSchema returns the schema proxy for t. A named struct type
// registers once under the base document's Components.Schemas, keyed
// by CanonicalTypeName with overridedNames applied at registration,
// and is referenced as "#/components/schemas/<name>". Other types
// inline their canonical schema. time.Time is named but maps to a
// string format, not a component.
func (c *documentConfig) RegisterSchema(t reflect.Type) *base.SchemaProxy {
	named := t
	for named.Kind() == reflect.Pointer {
		named = named.Elem()
	}
	if named.Kind() != reflect.Struct || named.Name() == "" || named == reflect.TypeFor[time.Time]() {
		schema := internal.CanonicalSchema(t)
		return base.CreateSchemaProxy(&schema)
	}
	name := internal.CanonicalTypeName(named)
	if override, ok := c.overridedNames[name]; ok {
		name = override
	}
	if c.base.Components.Schemas == nil {
		c.base.Components.Schemas = orderedmap.New[string, *base.SchemaProxy]()
	}
	if _, ok := c.base.Components.Schemas.Get(name); !ok {
		schema := internal.CanonicalSchema(named)
		c.base.Components.Schemas.Set(name, base.CreateSchemaProxy(&schema))
	}
	return base.CreateSchemaProxyRef("#/components/schemas/" + name)
}

// RegisterOperation merges config's operation — the user override on
// the reflected base — into the base document at (method, path).
// Only the document-level override waits for render.
func (c *documentConfig) RegisterOperation(config *operationConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	location := config.method + " " + config.path
	if c.operationRoutes[location] {
		return fmt.Errorf("duplicate OpenAPI operation %s", location)
	}
	c.operationRoutes[location] = true

	operation := internal.MergeOperation(config.base, config.override)
	if operation.OperationId != "" {
		if previous, ok := c.operationIds[operation.OperationId]; ok {
			return fmt.Errorf(
				"duplicate OpenAPI operationId %q at %s and %s",
				operation.OperationId, previous, location,
			)
		}
		c.operationIds[operation.OperationId] = location
	}

	item, ok := c.base.Paths.PathItems.Get(config.path)
	if !ok {
		item = &v3.PathItem{}
		c.base.Paths.PathItems.Set(config.path, item)
	}
	var previous *v3.Operation
	switch config.method {
	case http.MethodGet:
		previous, item.Get = item.Get, operation
	case http.MethodPost:
		previous, item.Post = item.Post, operation
	case http.MethodPut:
		previous, item.Put = item.Put, operation
	case http.MethodDelete:
		previous, item.Delete = item.Delete, operation
	case http.MethodPatch:
		previous, item.Patch = item.Patch, operation
	case http.MethodHead:
		previous, item.Head = item.Head, operation
	case http.MethodOptions:
		previous, item.Options = item.Options, operation
	case http.MethodTrace:
		previous, item.Trace = item.Trace, operation
	default:
		if item.AdditionalOperations == nil {
			item.AdditionalOperations = orderedmap.New[string, *v3.Operation]()
		}
		key := strings.ToLower(config.method)
		previous, _ = item.AdditionalOperations.Get(key)
		item.AdditionalOperations.Set(key, operation)
	}
	if previous != nil {
		return fmt.Errorf("OpenAPI path %s already contains a %s operation", config.path, config.method)
	}

	c.base.Components = internal.MergeComponents(c.base.Components, config.components)

	return nil
}

// Render assembles the final OpenAPI document — the base document
// overlaid with the override document options — and renders it in
// the given format. _RENDER_FORMAT_AUTO resolves to the configured
// renderFormat, YAML when unset.
func (c *documentConfig) Render(format RenderFormat) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if format == _RENDER_FORMAT_AUTO {
		format = c.renderFormat
	}
	if format == "" {
		format = _RENDER_FORMAT_YAML
	}
	if format != _RENDER_FORMAT_JSON && format != _RENDER_FORMAT_YAML {
		return nil, fmt.Errorf("unknown OpenAPI render format %q", format)
	}

	// Patch schemas shadow reflected schemas of the same name by
	// design; log the shadows here, before the merge buries them.
	if c.base.Components != nil && c.base.Components.Schemas != nil {
		for name, importPath := range c.patchedSchemas {
			if _, ok := c.base.Components.Schemas.Get(name); ok {
				slog.Debug("reflected OpenAPI schema shadowed by patch", "name", name, "patch", importPath)
			}
		}
	}

	// MergeDocument returns a copy safe to touch; base survives for the
	// serve-time Render. MergeComponents is the exception — its replace
	// semantics are idempotent.
	document := internal.MergeDocument(c.base, c.override)
	if document.Info.Version == "" {
		document.Info.Version = "1.0.0"
	}

	// Both renderers work on the hand-built model — no low-level
	// document is attached.
	var rendered []byte
	var err error
	switch format {
	case _RENDER_FORMAT_JSON:
		rendered, err = document.RenderJSON("  ")
	default:
		rendered, err = document.Render()
	}
	if err != nil {
		return nil, fmt.Errorf("render OpenAPI document: %w", err)
	}

	// Validate what is served, not the in-memory model: re-parse the
	// rendered bytes and check them against the spec schema, so
	// Initialize fails early on an invalid document.
	parsed, err := libopenapi.NewDocument(rendered)
	if err != nil {
		return nil, fmt.Errorf("re-parse rendered OpenAPI document: %w", err)
	}
	docValidator, errs := validator.NewValidator(parsed)
	if len(errs) > 0 {
		return nil, fmt.Errorf("build OpenAPI validator: %w", errors.Join(errs...))
	}
	valid, validationErrors := docValidator.ValidateDocument()
	if !valid {
		const reportLimit = 5
		messages := make([]string, 0, min(len(validationErrors), reportLimit))
		for _, validationError := range validationErrors[:min(len(validationErrors), reportLimit)] {
			messages = append(messages, validationError.Error())
		}
		if rest := len(validationErrors) - reportLimit; rest > 0 {
			messages = append(messages, fmt.Sprintf("and %d more", rest))
		}
		return nil, fmt.Errorf("invalid OpenAPI document:\n  %s", strings.Join(messages, "\n  "))
	}

	return rendered, nil
}

// WithDocumentSelf sets the OpenAPI 3.2 $self URI used as the
// document base URI.
func WithDocumentSelf(uri string) DocumentOption {
	return func(c *documentConfig) {
		c.override.Self = uri
	}
}

// WithDocumentJsonSchemaDialect sets the default JSON Schema dialect
// for Schema Objects in the document.
func WithDocumentJsonSchemaDialect(uri string) DocumentOption {
	return func(c *documentConfig) {
		c.override.JsonSchemaDialect = uri
	}
}

// WithDocumentVersion selects the output OpenAPI version. Versions
// 3.1.x and 3.2.x are supported. The default is 3.2.0.
func WithDocumentVersion(version string) DocumentOption {
	if !strings.HasPrefix(version, "3.1.") && !strings.HasPrefix(version, "3.2.") {
		panic(fmt.Sprintf("unsupported OpenAPI output version %q: expected 3.1.x or 3.2.x", version))
	}
	return func(c *documentConfig) {
		c.override.Version = version
	}
}

// WithDocumentServePath sets the path to serve openapi.json.
func WithDocumentRoute(route string) DocumentOption {
	return func(c *documentConfig) {
		c.route = route
	}
}

// WithDocumentRenderJson use JSON rendering.
func WithDocumentJSON() DocumentOption {
	return func(c *documentConfig) {
		c.renderFormat = _RENDER_FORMAT_JSON
	}
}

// WithDocumentRenderYaml use YAML rendering.
func WithDocumentYAML() DocumentOption {
	return func(c *documentConfig) {
		c.renderFormat = _RENDER_FORMAT_YAML
	}
}

// WithDocumentDocumentation enables documentation generation.
func WithDocumentRenderHTML() DocumentOption {
	return func(c *documentConfig) {
		c.renderHTML = true
	}
}

// WithDocumentPreLoad loads an OpenAPI document from data.
func WithDocumentBase(data []byte) DocumentOption {
	return func(c *documentConfig) {
		c.FromRaw(data)
	}
}

// WithDocumentComponents adds reusable OpenAPI components to the
// generated document. Incompatible components with the same name are
// rejected.
func WithDocumentComponents(components *v3.Components) DocumentOption {
	return func(c *documentConfig) {
		if components == nil {
			return
		}
		c.override.Components = components
	}
}

// WithDocumentPatch merges the component schemas of a raw OpenAPI
// document — typically one mizucue package's generated output —
// into the assembled document. importPath is the patch's
// provenance, carried into panic messages. Schemas are expected to
// be qualified already (mizucue's default naming): the canonical
// name is the schema's identity, so a name repeated across patches
// dedupes silently — first occurrence wins. At render a patch
// schema shadows the reflected schema of the same name — the CUE
// document is the richer contract; the shadow is logged at debug
// level. Fields other than component schemas are ignored.
func WithDocumentPatch(importPath string, raw []byte) DocumentOption {
	document, err := libopenapi.NewDocument(raw)
	if err != nil {
		panic(fmt.Sprintf("parse OpenAPI patch %s: %v", importPath, err))
	}
	model, err := document.BuildV3Model()
	if err != nil {
		panic(fmt.Sprintf("build OpenAPI patch %s: %v", importPath, err))
	}
	schemas := orderedmap.New[string, *base.SchemaProxy]()
	if model.Model.Components != nil && model.Model.Components.Schemas != nil {
		schemas = model.Model.Components.Schemas
	}
	return func(c *documentConfig) {
		if c.override.Components.Schemas == nil {
			c.override.Components.Schemas = orderedmap.New[string, *base.SchemaProxy]()
		}
		for name, schema := range schemas.FromOldest() {
			if _, ok := c.override.Components.Schemas.Get(name); ok {
				continue
			}
			c.override.Components.Schemas.Set(name, schema)
			c.patchedSchemas[name] = importPath
		}
	}
}

// WithDocumentSchemaName overrides the reflected component name for T.
func WithDocumentSchemaName[T any](name string) DocumentOption {
	return func(c *documentConfig) {
		if name == "" {
			panic("OpenAPI schema name cannot be empty")
		}
		c.overridedNames[internal.CanonicalTypeName(reflect.TypeFor[T]())] = name
	}
}

// WithDocumentDescription provides a verbose description of the API.
// CommonMark syntax MAY be used for rich text representation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#info-object
func WithDocumentDescription(description string) DocumentOption {
	return func(c *documentConfig) {
		c.override.Info.Description = description
	}
}

// WithDocumentSummary provides the OpenAPI 3.2 API summary.
func WithDocumentSummary(summary string) DocumentOption {
	return func(c *documentConfig) {
		c.override.Info.Summary = summary
	}
}

// WithDocumentInfoVersion sets the version of the described API.
func WithDocumentInfoVersion(version string) DocumentOption {
	return func(c *documentConfig) {
		c.override.Info.Version = version
	}
}

// WithDocumentInfo supplies a complete OpenAPI Info Object.
// Initialize's title is retained when info does not specify one.
func WithDocumentInfo(info *base.Info) DocumentOption {
	return func(c *documentConfig) {
		if info == nil {
			panic("OpenAPI info is nil")
		}
		title := c.override.Info.Title
		copied := *info
		c.override.Info = &copied
		if c.override.Info.Title == "" {
			c.override.Info.Title = title
		}
	}
}

// WithDocumentTermsOfService provides a URL to the Terms of Service
// for the API. Must be in the form of URI.
//
// - https://spec.openapis.org/oas/v3.0.4.html#info-object
func WithDocumentTermsOfService(url string) DocumentOption {
	return func(c *documentConfig) {
		c.override.Info.TermsOfService = url
	}
}

// WithDocumentContact provides contact information for the exposed
// API.
//
// - https://spec.openapis.org/oas/v3.0.4.html#contact-object
func WithDocumentContact(name string, url string, email string, extensions ...orderedmap.Map[string, any],
) DocumentOption {
	var firstExtensions orderedmap.Map[string, any]
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}

	return func(c *documentConfig) {
		c.override.Info.Contact = &base.Contact{
			Name:       name,
			URL:        url,
			Email:      email,
			Extensions: internal.CanonicalExtensions(firstExtensions),
		}
	}
}

// WithDocumentLicense provides the license information for the
// exposed API.
//
// - https://spec.openapis.org/oas/v3.0.4.html#license-object
func WithDocumentLicense(name string, url string, extensions ...orderedmap.Map[string, any]) DocumentOption {
	var firstExtensions orderedmap.Map[string, any]
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *documentConfig) {
		if name == "" {
			panic("OpenAPI license name cannot be empty")
		}
		c.override.Info.License = &base.License{
			Name:       name,
			URL:        url,
			Extensions: internal.CanonicalExtensions(firstExtensions),
		}
	}
}

// WithDocumentWebhook adds an OpenAPI 3.1+ webhook Path Item.
func WithDocumentWebhook(name string, item *v3.PathItem) DocumentOption {
	return func(c *documentConfig) {
		if name == "" || item == nil {
			panic("OpenAPI webhook name and path item are required")
		}
		c.override.Webhooks.Set(name, item)
	}
}

// WithDocumentSecurityScheme adds a reusable security scheme component.
func WithDocumentSecurityScheme(name string, scheme *v3.SecurityScheme) DocumentOption {
	return func(c *documentConfig) {
		if name == "" || scheme == nil {
			panic("OpenAPI security scheme name and value are required")
		}
		c.override.Components.SecuritySchemes.Set(name, scheme)
	}
}

// WithDocumentServers adds an array of Server Objects, which provide
// connectivity information to a target server. If the servers field
// is not provided, or is an empty array, the default value would be a
// Server Object with a url value of /.
//
// - https://spec.openapis.org/oas/v3.0.4.html#server-object
func WithDocumentServer(
	url string, desc string, variables map[string]*v3.ServerVariable, extensions ...orderedmap.Map[string, any],
) DocumentOption {
	var firstExtensions orderedmap.Map[string, any]
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}

	return func(c *documentConfig) {
		c.override.Servers = append(c.override.Servers, &v3.Server{
			URL:         url,
			Description: desc,
			Variables:   orderedmap.ToOrderedMap(variables),
			Extensions:  internal.CanonicalExtensions(firstExtensions),
		})
	}
}

// WithDocumentServerObject adds a complete Server Object.
func WithDocumentServerObject(server *v3.Server) DocumentOption {
	return func(c *documentConfig) {
		if server == nil {
			panic("OpenAPI server is nil")
		}
		c.override.Servers = append(c.override.Servers, server)
	}
}

// WithDocumentSecurity adds a security requirement to the operation.
// Each name MUST correspond to a security scheme which is declared in
// the Security Schemes under the Components Object.
//
// - https://spec.openapis.org/oas/v3.0.4.html#security-requirement-object
func WithDocumentSecurity(requirement map[string][]string) DocumentOption {
	var containEmpty bool
	for _, v := range requirement {
		if len(v) == 0 {
			containEmpty = true
			break
		}
	}
	return func(c *documentConfig) {
		c.override.Security = append(c.override.Security, &base.SecurityRequirement{
			ContainsEmptyRequirement: containEmpty,
			Requirements:             orderedmap.ToOrderedMap(requirement),
		})
	}
}

// WithDocumentTags adds tags to the operation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#tag-object
func WithDocumentTag(name string, desc string, externalDocs *base.ExternalDoc, extensions ...orderedmap.Map[string, any],
) DocumentOption {
	var firstExtensions orderedmap.Map[string, any]
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *documentConfig) {
		tag := &base.Tag{
			Name:         name,
			Description:  desc,
			ExternalDocs: externalDocs,
			Extensions:   internal.CanonicalExtensions(firstExtensions),
		}
		if name == "" {
			panic("OpenAPI tag name is required")
		}
		c.override.Tags = append(c.override.Tags, tag)
	}
}

// WithDocumentTagObject adds a complete Tag Object.
func WithDocumentTagObject(tag *base.Tag) DocumentOption {
	return func(c *documentConfig) {
		if tag == nil {
			panic("OpenAPI tag is nil")
		}
		if tag.Name == "" {
			panic("OpenAPI tag name is required")
		}
		c.override.Tags = append(c.override.Tags, tag)
	}
}

// WithDocumentExternalDocs provides a reference to an external
// resource for extended documentation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#external-documentation-object
func WithDocumentExternalDocs(url string, description string, extensions ...orderedmap.Map[string, any]) DocumentOption {
	var firstExtensions orderedmap.Map[string, any]
	if len(extensions) > 0 {
		firstExtensions = extensions[0]
	}
	return func(c *documentConfig) {
		c.override.ExternalDocs = &base.ExternalDoc{
			Description: description,
			URL:         url,
			Extensions:  internal.CanonicalExtensions(firstExtensions),
		}
	}
}

// WithDocumentExtensions adds extensions to the operation.
//
// - https://spec.openapis.org/oas/v3.0.4.html#openapi-object
func WithDocumentExtensions(extensions orderedmap.Map[string, any]) DocumentOption {
	return func(c *documentConfig) {
		c.override.Extensions = internal.CanonicalExtensions(extensions)
	}
}
