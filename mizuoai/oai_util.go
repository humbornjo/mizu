package mizuoai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Public Utils -------------------------------------------------

func convExtensions(extensions map[string]any) *orderedmap.Map[string, *yaml.Node] {
	ymap := make(map[string]*yaml.Node)
	for k, v := range extensions {
		var node yaml.Node
		if err := node.Encode(v); err == nil {
			ymap[k] = &node
		} else {
			fmt.Printf("🚨 [ERROR] Failed to encode extension: %s. KEY: %s, VALUE: %v.\n", err, k, v)
		}
	}
	return orderedmap.ToOrderedMap(ymap)
}

// createSchema creates a *base.SchemaProxy from a reflect.Type
func createSchema(typ reflect.Type) *base.SchemaProxy {
	// Dereference pointer types to get the underlying type.
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	schema := &base.Schema{Properties: orderedmap.New[string, *base.SchemaProxy]()}
	switch typ.Kind() {
	case reflect.String:
		schema.Type = append(schema.Type, "string")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = append(schema.Type, "integer")
	case reflect.Float32, reflect.Float64:
		schema.Type = append(schema.Type, "number")
	case reflect.Bool:
		schema.Type = append(schema.Type, "boolean")
	case reflect.Struct:
		schema.Type = append(schema.Type, "object")
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
			if jsonTag == "" || jsonTag == "-" {
				continue
			}
			fieldSchema := createSchema(field.Type)
			if fieldSchema != nil {
				schema.Properties.Set(jsonTag, fieldSchema)
			}
		}
	case reflect.Slice:
		schema.Type = append(schema.Type, "array")
		schema.Items = &base.DynamicValue[*base.SchemaProxy, bool]{A: createSchema(typ.Elem())}
	default:
		// Unsupported types will result in a nil schema.
		return nil
	}

	return base.CreateSchemaProxy(schema)
}

// setParamValue sets a value to a reflect.Value based on its kind
func setParamValue(value reflect.Value, paramValue string, kind reflect.Kind) error {
	switch kind {
	case reflect.String:
		value.SetString(paramValue)
	case reflect.Bool:
		boolValue, err := strconv.ParseBool(paramValue)
		if err != nil {
			return fmt.Errorf("cannot convert %s to bool: %w", paramValue, err)
		}
		value.SetBool(boolValue)
	case reflect.Struct:
		object := reflect.New(value.Type()).Interface()
		if err := json.Unmarshal([]byte(paramValue), &object); err != nil {
			return err
		}
		value.Set(reflect.ValueOf(object).Elem())
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intValue, err := strconv.ParseInt(paramValue, 10, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", paramValue, kind, err)
		}
		value.SetInt(intValue)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintValue, err := strconv.ParseUint(paramValue, 10, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", paramValue, kind, err)
		}
		value.SetUint(uintValue)
	case reflect.Float32, reflect.Float64:
		floatValue, err := strconv.ParseFloat(paramValue, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", paramValue, kind, err)
		}
		value.SetFloat(floatValue)
	default:
		return fmt.Errorf("unsupported type %s", kind)
	}
	return nil
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

// OpenAPI Object Utils -----------------------------------------

func (c *oaiConfig) render(json bool) ([]byte, error) {
	defaultPreLoaded := []byte("openapi: 3.1.0")
	if c.baseModel == nil {
		document, err := libopenapi.NewDocument(defaultPreLoaded)
		if err != nil {
			return nil, err
		}
		v3Model, err := document.BuildV3Model()
		if err != nil {
			return nil, err
		}
		c.baseModel = &v3Model.Model
	}

	// Merge with Pre Loaded OpenAPI Object
	model := c.baseModel
	model.Info = mergeInfo(model.Info, c.info)
	model.Extensions = mergeExtensions(model.Extensions, c.extensions)
	model.Tags = append(model.Tags, c.tags...)
	model.Servers = append(model.Servers, c.servers...)
	model.Security = append(model.Security, c.security...)
	if model.ExternalDocs != nil {
		model.ExternalDocs = c.externalDocs
	}

	if !strings.HasPrefix(model.Version, "3.1.0") &&
		!strings.HasPrefix(model.Version, "3.0") {
		return nil, ErrOaiVersion
	}

	if model.Paths == nil {
		model.Paths = new(v3.Paths)
	}

	for _, handler := range c.handlers {
		_, ok := model.Paths.PathItems.Get(handler.path)
		if ok {
			fmt.Printf("⚠️ [WARN] Path %s is already defined, replaced.\n", handler.path)
		}
		item := &v3.PathItem{}
		switch handler.method {
		case http.MethodGet:
			item.Get = &handler.Operation
		case http.MethodPost:
			item.Post = &handler.Operation
		case http.MethodPut:
			item.Put = &handler.Operation
		case http.MethodDelete:
			item.Delete = &handler.Operation
		case http.MethodPatch:
			item.Patch = &handler.Operation
		case http.MethodHead:
			item.Head = &handler.Operation
		case http.MethodOptions:
			item.Options = &handler.Operation
		case http.MethodTrace:
			item.Trace = &handler.Operation
		default:
			panic("unreachable")
		}
		model.Paths.PathItems.Set(handler.path, item)
	}

	if !json {
		return model.Render()
	}
	return model.RenderJSON("  ")
}

func mergeInfo(bench *base.Info, overlay *base.Info) *base.Info {
	if bench == nil {
		return overlay
	}

	if overlay.Contact != nil {
		bench.Contact = overlay.Contact
	}
	if overlay.Description != "" {
		bench.Description = overlay.Description
	}
	if overlay.License != nil {
		bench.License = overlay.License
	}
	if overlay.Title != "" {
		bench.Title = overlay.Title
	}
	if overlay.Version != "" {
		bench.Version = overlay.Version
	}
	if overlay.TermsOfService != "" {
		bench.TermsOfService = overlay.TermsOfService
	}

	bench.Extensions = mergeExtensions(bench.Extensions, overlay.Extensions)

	return bench
}

func mergeExtensions(bench *orderedmap.Map[string, *yaml.Node], overlay *orderedmap.Map[string, *yaml.Node],
) *orderedmap.Map[string, *yaml.Node] {
	if overlay == nil {
		return bench
	}
	for k := range overlay.KeysFromNewest() {
		v, _ := overlay.Get(k)
		bench.Set(k, v)
	}
	return bench
}

// Operation Utils ----------------------------------------------

func enrichOperation[I any, O any](config *operationConfig) {
	input := new(I)
	valInput := reflect.ValueOf(input).Elem()
	typInput := valInput.Type()

	// Process input type (request parameters/body)
	for i := range typInput.NumField() {
		field := typInput.Field(i)
		mizuTag, ok := field.Tag.Lookup("mizu")
		if !ok {
			continue
		}
		switch tag(mizuTag) {
		case _STRUCT_TAG_PATH, _STRUCT_TAG_QUERY, _STRUCT_TAG_HEADER:
			for i := range field.Type.NumField() {
				subField := field.Type.Field(i)
				config.createParameter(mizuTag, &subField)
			}
		case _STRUCT_TAG_BODY:
			config.createRequestBody(&field, false)
		case _STRUCT_TAG_FORM:
			config.createRequestBody(&field, true)
		}
	}

	output := new(O)
	valOutput := reflect.ValueOf(output).Elem()
	typOutput := valOutput.Type()
	config.createResponses(typOutput)
}

func (c *operationConfig) createParameter(tag string, field *reflect.StructField) {
	subTag := field.Tag.Get(tag)
	if subTag == "" || subTag == "-" {
		return
	}

	param := &v3.Parameter{
		Name:        subTag,
		In:          tag,
		Description: field.Tag.Get("desc"),
		Deprecated:  field.Tag.Get("deprecated") == "true",
		Required:    new(bool),
		Schema:      createSchema(field.Type),
	}
	*param.Required = field.Tag.Get("required") == "true"
	c.Parameters = append(c.Parameters, param)
}

func (c *operationConfig) createRequestBody(field *reflect.StructField, isForm bool) {
	request := &v3.RequestBody{
		Description: field.Tag.Get("desc"),
		Required:    new(bool),
		Content:     orderedmap.New[string, *v3.MediaType](),
	}
	*request.Required = field.Tag.Get("required") == "true"

	contentType := "application/json"
	if field.Type.Kind() == reflect.String {
		contentType = "plain/text"
	}
	if isForm {
		contentType = "application/x-www-form-urlencoded"
	}

	request.Content.Set(contentType, &v3.MediaType{Schema: createSchema(field.Type)})
	c.RequestBody = request
}

func (c *operationConfig) createResponses(typ reflect.Type) {
	// Set default response
	response := &v3.Response{
		Content: orderedmap.New[string, *v3.MediaType](),
	}
	response.Links = orderedmap.ToOrderedMap(c.responseLinks)
	response.Headers = orderedmap.ToOrderedMap(c.responseHeaders)

	var contentType string
	switch typ.Kind() {
	case reflect.String:
		contentType = "plain/text"
	default:
		contentType = "application/json"
	}
	encodings := orderedmap.New[string, *v3.Encoding]()
	encodings.Set(contentType, &v3.Encoding{ContentType: contentType})

	response.Content.Set(contentType, &v3.MediaType{
		Encoding: encodings,
		Schema:   createSchema(typ),
	})

	defaultKey := "200"
	if c.responseCode != nil {
		defaultKey = strconv.Itoa(*c.responseCode)
	}
	c.Responses.Codes.Set(defaultKey, response)
}
