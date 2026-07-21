package mizuoai

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi-validator/schema_validation"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

func consFormReader(rx io.Reader, contentType string) (*multipart.Reader, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, fmt.Errorf("expected multipart content type, got %s", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("form boundary not found")
	}
	return multipart.NewReader(rx, boundary), nil
}

func convExtensions(extensions map[string]any) *orderedmap.Map[string, *yaml.Node] {
	if len(extensions) == 0 {
		return nil
	}
	result := orderedmap.New[string, *yaml.Node]()
	for key, value := range extensions {
		node := new(yaml.Node)
		if err := node.Encode(value); err != nil {
			continue
		}
		result.Set(key, node)
	}
	return orderedmap.SortAlpha(result)
}

func bitSize(kind reflect.Kind) int {
	switch kind {
	case reflect.Uint8, reflect.Int8:
		return 8
	case reflect.Uint16, reflect.Int16:
		return 16
	case reflect.Uint32, reflect.Int32, reflect.Float32:
		return 32
	case reflect.Uint, reflect.Int:
		return strconv.IntSize
	}
	return 64
}

func (c *oaiConfig) render(jsonOutput bool) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.renderUnlocked(jsonOutput)
}

func (c *oaiConfig) renderUnlocked(jsonOutput bool) ([]byte, error) {
	model, err := c.buildModel()
	if err != nil {
		return nil, err
	}
	content, err := model.Render()
	if err != nil {
		return nil, err
	}
	rendered, err := parseYamlDocument(content)
	if err != nil {
		return nil, err
	}
	inputVersion := ""
	var raw *yaml.Node
	if len(c.baseData) > 0 {
		raw, err = parseYamlDocument(c.baseData)
		if err != nil {
			return nil, err
		}
		inputVersion = openApiVersion(raw)
		normalizeOpenApi30Schemas(raw, inputVersion, c.version)
	}
	for _, handler := range c.handlers {
		if handler.external != nil && handler.external.rawOverlay != nil {
			overlay := cloneYamlNode(handler.external.rawOverlay)
			normalizeOpenApi30Schemas(overlay, handler.external.inputVersion, c.version)
			raw = mergeYamlDocuments(raw, overlay)
			if inputVersion == "" && strings.HasPrefix(handler.external.inputVersion, "3.0.") {
				inputVersion = handler.external.inputVersion
			}
		}
	}
	normalizeOpenApi30Schemas(rendered, inputVersion, c.version)
	rendered = mergeYamlDocuments(raw, rendered)
	content, err = renderYamlDocument(rendered, jsonOutput)
	if err != nil {
		return nil, err
	}
	if err := validateRenderedDocument(content); err != nil {
		return nil, err
	}
	return content, nil
}

func (c *oaiConfig) buildModel() (*v3.Document, error) {
	model := &v3.Document{}
	if len(c.baseData) > 0 {
		sanitized, err := sanitizedLibopenapiData(c.baseData)
		if err != nil {
			return nil, fmt.Errorf("prepare base OpenAPI document: %w", err)
		}
		document, err := libopenapi.NewDocument(sanitized)
		if err != nil {
			return nil, fmt.Errorf("parse base OpenAPI document: %w", err)
		}
		built, err := document.BuildV3Model()
		if err != nil {
			return nil, fmt.Errorf("build base OpenAPI document: %w", err)
		}
		model = &built.Model
	}
	model.Version = c.version
	if c.self != "" {
		model.Self = c.self
	}
	if c.jsonDialect != "" {
		model.JsonSchemaDialect = c.jsonDialect
	}
	model.Info = mergeInfo(model.Info, c.info)
	if model.Info.Version == "" {
		model.Info.Version = "1.0.0"
	}
	model.Extensions = mergeExtensions(model.Extensions, c.extensions)
	tags := append([]*base.Tag(nil), model.Tags...)
	if err := mergeTags(&tags, c.tags); err != nil {
		return nil, err
	}
	model.Tags = tags
	model.Servers = append(model.Servers, c.servers...)
	model.Security = append(model.Security, c.security...)
	if model.Webhooks == nil {
		model.Webhooks = orderedmap.New[string, *v3.PathItem]()
	}
	if err := mergeComponentMap("webhooks", model.Webhooks, c.webhooks); err != nil {
		return nil, err
	}
	if c.externalDocs != nil {
		model.ExternalDocs = c.externalDocs
	}
	model.Components = ensureComponents(model.Components)
	if err := mergeComponents(model.Components, c.components); err != nil {
		return nil, err
	}
	if model.Paths == nil {
		model.Paths = &v3.Paths{}
	}
	if model.Paths.PathItems == nil {
		model.Paths.PathItems = orderedmap.New[string, *v3.PathItem]()
	}
	for path, configured := range c.paths.FromOldest() {
		item, ok := model.Paths.PathItems.Get(path)
		if !ok {
			item = &v3.PathItem{}
			model.Paths.PathItems.Set(path, item)
		}
		if err := mergePathItem(item, configured); err != nil {
			return nil, fmt.Errorf("merge OpenAPI path %s: %w", path, err)
		}
	}
	for _, handler := range c.handlers {
		item, ok := model.Paths.PathItems.Get(handler.path)
		if !ok {
			item = &v3.PathItem{}
			model.Paths.PathItems.Set(handler.path, item)
		}
		if err := mergePathMetadata(item, handler.pathItem); err != nil {
			return nil, fmt.Errorf("merge OpenAPI path %s: %w", handler.path, err)
		}
		if err := setPathOperation(item, handler.method, &handler.Operation); err != nil {
			return nil, fmt.Errorf("add OpenAPI operation %s %s: %w", handler.method, handler.path, err)
		}
	}
	if err := validateModel(model); err != nil {
		return nil, err
	}
	return model, nil
}

// nolint: gocyclo
func validateModel(model *v3.Document) error {
	if err := validateTargetVersion(model.Version); err != nil {
		return err
	}
	if err := validateTagHierarchy(model.Tags); err != nil {
		return err
	}
	if err := validateSecurityRequirements(model.Security, model.Components, "document"); err != nil {
		return err
	}
	operationIds := make(map[string]string)
	seenPathItems := make(map[*v3.PathItem]bool)
	seenOperations := make(map[*v3.Operation]bool)
	var visitPathItem func(string, *v3.PathItem, bool) error
	visitPathItem = func(path string, item *v3.PathItem, validatePath bool) error {
		if item == nil || seenPathItems[item] {
			return nil
		}
		seenPathItems[item] = true
		if validatePath && !strings.HasPrefix(path, "/") {
			return fmt.Errorf("OpenAPI path %q must start with /", path)
		}
		operations := item.GetOperations()
		if operations == nil {
			return nil
		}
		for method, operation := range operations.FromOldest() {
			if operation == nil || seenOperations[operation] {
				continue
			}
			seenOperations[operation] = true
			location := strings.ToUpper(method) + " " + path
			if operation.OperationId != "" {
				if previous, ok := operationIds[operation.OperationId]; ok {
					return fmt.Errorf(
						"duplicate OpenAPI operationId %q at %s and %s",
						operation.OperationId, previous, location,
					)
				}
				operationIds[operation.OperationId] = location
			}
			if err := validateResponses(operation.Responses, location); err != nil {
				return err
			}
			if err := validateSecurityRequirements(operation.Security, model.Components, location); err != nil {
				return err
			}
			if err := validateParameterDuplicates(item, operation.Parameters, model.Components); err != nil {
				return err
			}
			config := &operationConfig{
				Operation:  *operation,
				method:     strings.ToUpper(method),
				path:       path,
				pathItem:   item,
				components: model.Components,
			}
			if validatePath {
				if err := validatePathParameters(config); err != nil {
					return err
				}
			}
			if operation.Callbacks != nil {
				for name, callback := range operation.Callbacks.FromOldest() {
					if callback == nil || callback.Expression == nil {
						continue
					}
					for expression, callbackItem := range callback.Expression.FromOldest() {
						if err := visitPathItem("callback "+name+" "+expression, callbackItem, false); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	}
	if model.Paths != nil && model.Paths.PathItems != nil {
		for path, item := range model.Paths.PathItems.FromOldest() {
			if err := visitPathItem(path, item, true); err != nil {
				return err
			}
		}
	}
	if model.Webhooks != nil {
		for name, item := range model.Webhooks.FromOldest() {
			if err := visitPathItem("webhook "+name, item, false); err != nil {
				return err
			}
		}
	}
	if model.Components != nil {
		if model.Components.PathItems != nil {
			for name, item := range model.Components.PathItems.FromOldest() {
				if err := visitPathItem("component pathItem "+name, item, false); err != nil {
					return err
				}
			}
		}
		if model.Components.Callbacks != nil {
			for name, callback := range model.Components.Callbacks.FromOldest() {
				if callback == nil || callback.Expression == nil {
					continue
				}
				for expression, item := range callback.Expression.FromOldest() {
					if err := visitPathItem("component callback "+name+" "+expression, item, false); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func validateSecurityRequirements(
	requirements []*base.SecurityRequirement, components *v3.Components, location string,
) error {
	for _, requirement := range requirements {
		if requirement == nil || requirement.Requirements == nil {
			continue
		}
		for name := range requirement.Requirements.KeysFromOldest() {
			if components == nil || components.SecuritySchemes == nil {
				return fmt.Errorf("OpenAPI security requirement %q on %s has no security scheme", name, location)
			}
			if _, ok := components.SecuritySchemes.Get(name); !ok {
				return fmt.Errorf("OpenAPI security requirement %q on %s has no security scheme", name, location)
			}
		}
	}
	return nil
}

func validateTagHierarchy(tags []*base.Tag) error {
	byName := make(map[string]*base.Tag)
	for _, tag := range tags {
		if tag == nil {
			continue
		}
		if previous := byName[tag.Name]; previous != nil {
			return fmt.Errorf("duplicate OpenAPI tag %q", tag.Name)
		}
		byName[tag.Name] = tag
	}
	for _, tag := range tags {
		if tag == nil || tag.Parent == "" {
			continue
		}
		if byName[tag.Parent] == nil {
			return fmt.Errorf("OpenAPI tag %q has undefined parent %q", tag.Name, tag.Parent)
		}
		seen := map[string]bool{tag.Name: true}
		for parent := tag.Parent; parent != ""; parent = byName[parent].Parent {
			if seen[parent] {
				return fmt.Errorf("OpenAPI tag hierarchy contains a cycle at %q", parent)
			}
			seen[parent] = true
		}
	}
	return nil
}

func validateRenderedDocument(content []byte) error {
	document, err := libopenapi.NewDocument(content)
	if err != nil {
		return fmt.Errorf("validate rendered OpenAPI document: %w", err)
	}
	valid, validationErrors := schema_validation.ValidateOpenAPIDocument(document)
	if !valid {
		errs := make([]error, 0, len(validationErrors))
		for _, validationError := range validationErrors {
			errs = append(errs, validationError)
		}
		return fmt.Errorf("validate rendered OpenAPI document: %w", errors.Join(errs...))
	}
	if _, err := document.BuildV3Model(); err != nil {
		return fmt.Errorf("validate rendered OpenAPI model: %w", err)
	}
	return nil
}

func mergeInfo(target, overlay *base.Info) *base.Info {
	if target == nil {
		target = new(base.Info)
	}
	if overlay == nil {
		return target
	}
	if overlay.Contact != nil {
		target.Contact = overlay.Contact
	}
	if overlay.Description != "" {
		target.Description = overlay.Description
	}
	if overlay.License != nil {
		target.License = overlay.License
	}
	if overlay.Summary != "" {
		target.Summary = overlay.Summary
	}
	if overlay.Title != "" {
		target.Title = overlay.Title
	}
	if overlay.Version != "" {
		target.Version = overlay.Version
	}
	if overlay.TermsOfService != "" {
		target.TermsOfService = overlay.TermsOfService
	}
	target.Extensions = mergeExtensions(target.Extensions, overlay.Extensions)
	return target
}

func mergeExtensions(
	target, overlay *orderedmap.Map[string, *yaml.Node],
) *orderedmap.Map[string, *yaml.Node] {
	if overlay == nil {
		return target
	}
	if target == nil {
		target = orderedmap.New[string, *yaml.Node]()
	}
	for key, value := range overlay.FromOldest() {
		target.Set(key, value)
	}
	return target
}

func enrichOperation[I any, O any](config *operationConfig, reflector *schemaReflector) {
	ensureOperation(config)
	inputType := reflect.TypeFor[I]()
	for inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}
	if inputType.Kind() != reflect.Struct {
		config.err = fmt.Errorf("request type must be a struct, got %s", inputType)
		return
	}
	requestBodyLocation := ""
	for field := range inputType.Fields() {
		location, options, ignored, err := requestLocation(field)
		if err != nil {
			config.err = err
			return
		}
		if ignored {
			continue
		}
		switch mizutag(location) {
		case _STRUCT_TAG_PATH, _STRUCT_TAG_QUERY, _STRUCT_TAG_HEADER:
			fieldType := field.Type
			for fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() != reflect.Struct {
				config.err = fmt.Errorf("%s request field must be a struct", location)
				return
			}
			for parameterField := range fieldType.Fields() {
				config.createParameter(mizutag(location), parameterField, reflector)
			}
		case _STRUCT_TAG_BODY, _STRUCT_TAG_FORM:
			if requestBodyLocation != "" {
				config.err = fmt.Errorf(
					"request fields %s and %s both define the request body",
					requestBodyLocation, field.Name,
				)
				return
			}
			requestBodyLocation = field.Name
			config.createRequestBody(field, mizutag(location) == _STRUCT_TAG_FORM, options, reflector)
		}
	}
	config.createResponse(reflect.TypeFor[O](), reflector)
	if reflector.err != nil {
		config.err = reflector.err
	}
}

func ensureOperation(config *operationConfig) {
	if config.Deprecated == nil {
		config.Deprecated = new(bool)
	}
	if config.Callbacks == nil {
		config.Callbacks = orderedmap.New[string, *v3.Callback]()
	}
	if config.Responses == nil {
		config.Responses = &v3.Responses{Codes: orderedmap.New[string, *v3.Response]()}
	}
	if config.Responses.Codes == nil {
		config.Responses.Codes = orderedmap.New[string, *v3.Response]()
	}
}

func (c *operationConfig) createParameter(
	location mizutag, field reflect.StructField, reflector *schemaReflector,
) {
	if field.PkgPath != "" {
		return
	}
	name, ignored, err := requestFieldName(field, location)
	if err != nil {
		c.err = err
		return
	}
	if ignored {
		return
	}
	required := location == _STRUCT_TAG_PATH
	if value, ok := field.Tag.Lookup("required"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			reflector.setError(fmt.Errorf("parse required tag on %s: %w", field.Name, err))
		} else if location != _STRUCT_TAG_PATH {
			required = parsed
		}
	}
	schema := reflector.schema(field.Type)
	if hasSchemaTags(field) {
		schema = decorateSchema(schema, field, reflector)
	}
	parameter := &v3.Parameter{
		Name:        name,
		In:          location.String(),
		Description: field.Tag.Get("desc"),
		Required:    &required,
		Schema:      schema,
		Style:       field.Tag.Get("style"),
	}
	parseParameterBool := func(key string, target *bool) {
		value, ok := field.Tag.Lookup(key)
		if !ok {
			return
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			reflector.setError(fmt.Errorf("parse %s tag on %s: %w", key, field.Name, err))
			return
		}
		*target = parsed
	}
	parseParameterBool("deprecated", &parameter.Deprecated)
	parseParameterBool("allowReserved", &parameter.AllowReserved)
	parseParameterBool("allowEmptyValue", &parameter.AllowEmptyValue)
	if value, ok := field.Tag.Lookup("example"); ok {
		parameter.Example = tagNode(value)
	}
	if value, ok := field.Tag.Lookup("explode"); ok {
		explode, err := strconv.ParseBool(value)
		if err != nil {
			reflector.setError(fmt.Errorf("parse explode tag on %s: %w", field.Name, err))
		} else {
			parameter.Explode = &explode
		}
	}
	c.Parameters = append(c.Parameters, parameter)
}

func (c *operationConfig) createRequestBody(
	field reflect.StructField, form bool, options map[string]bool, reflector *schemaReflector,
) {
	if c.requestBodySet {
		return
	}
	if c.RequestBody != nil {
		c.err = errors.New("request body is already defined")
		return
	}
	required := field.Tag.Get("required") == "true" ||
		(field.Tag.Get("required") != "false" && !options["omitempty"])
	request := &v3.RequestBody{
		Description: field.Tag.Get("desc"),
		Required:    &required,
		Content:     orderedmap.New[string, *v3.MediaType](),
	}
	contentType := field.Tag.Get("contentType")
	if contentType == "" {
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		switch {
		case form:
			contentType = "multipart/form-data"
		case isByteSequence(field.Type):
			contentType = "application/octet-stream"
		case fieldType.Kind() == reflect.String:
			contentType = "text/plain"
		default:
			contentType = "application/json"
		}
	}
	mediaType := &v3.MediaType{}
	switch {
	case form && strings.HasPrefix(contentType, "multipart/"):
		mediaType.Schema = reflector.multipartSchema(field.Type)
	case !isByteSequence(field.Type):
		mediaType.Schema = reflector.schema(field.Type)
	}
	request.Content.Set(contentType, mediaType)
	c.RequestBody = request
}

func (c *operationConfig) createResponse(typ reflect.Type, reflector *schemaReflector) {
	if c.responsesSet {
		return
	}
	ensureOperation(c)
	code := http.StatusOK
	if c.responseCode != nil {
		code = *c.responseCode
	}
	key := strconv.Itoa(code)
	if _, ok := c.Responses.Codes.Get(key); ok {
		return
	}
	response := &v3.Response{
		Description: http.StatusText(code),
		Headers:     orderedmap.ToOrderedMap(c.responseHeaders),
		Links:       orderedmap.ToOrderedMap(c.responseLinks),
		Content:     orderedmap.New[string, *v3.MediaType](),
	}
	contentType := "application/json"
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.String {
		contentType = "text/plain"
	}
	response.Content.Set(contentType, &v3.MediaType{Schema: reflector.schema(typ)})
	c.Responses.Codes.Set(key, response)
}
