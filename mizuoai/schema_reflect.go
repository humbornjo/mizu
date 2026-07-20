package mizuoai

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

var (
	_TIME_TYPE        = reflect.TypeFor[time.Time]()
	_RAW_MESSAGE_TYPE = reflect.TypeFor[json.RawMessage]()
	_COMPONENT_NAME   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

type schemaReflector struct {
	schemas   *orderedmap.Map[string, *base.SchemaProxy]
	byType    map[reflect.Type]string
	byName    map[string]reflect.Type
	overrides map[reflect.Type]string
	err       error
}

func newSchemaReflector(schemas *orderedmap.Map[string, *base.SchemaProxy]) *schemaReflector {
	return &schemaReflector{
		schemas:   schemas,
		byType:    make(map[reflect.Type]string),
		byName:    make(map[string]reflect.Type),
		overrides: make(map[reflect.Type]string),
	}
}

func (r *schemaReflector) withSchemas(schemas *orderedmap.Map[string, *base.SchemaProxy]) *schemaReflector {
	result := newSchemaReflector(schemas)
	for typ, name := range r.overrides {
		result.overrides[typ] = name
	}
	return result
}

func (r *schemaReflector) schema(typ reflect.Type) *base.SchemaProxy {
	return r.schemaType(typ, true)
}

func (r *schemaReflector) multipartSchema(typ reflect.Type) *base.SchemaProxy {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return r.schema(typ)
	}
	schema := &base.Schema{
		Type:       []string{"object"},
		Properties: orderedmap.New[string, *base.SchemaProxy](),
	}
	for field := range typ.Fields() {
		if field.PkgPath != "" {
			continue
		}
		name, options, ignored := jsonField(field)
		if ignored {
			continue
		}
		if name == "" {
			name = field.Name
		}
		var fieldSchema *base.SchemaProxy
		if isByteSequence(field.Type) {
			contentType := field.Tag.Get("contentType")
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			fieldSchema = base.CreateSchemaProxy(&base.Schema{
				Type:             []string{"string"},
				ContentMediaType: contentType,
			})
		} else {
			fieldSchema = r.schema(field.Type)
		}
		if hasSchemaTags(field) {
			fieldSchema = decorateSchema(fieldSchema, field, r)
		}
		schema.Properties.Set(name, fieldSchema)
		required, explicit := field.Tag.Lookup("required")
		switch {
		case explicit && required == "true":
			schema.Required = append(schema.Required, name)
		case explicit && required == "false":
		case !options["omitempty"] && field.Type.Kind() != reflect.Pointer:
			schema.Required = append(schema.Required, name)
		}
	}
	return base.CreateSchemaProxy(schema)
}

func isByteSequence(typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return (typ.Kind() == reflect.Array || typ.Kind() == reflect.Slice) && typ.Elem().Kind() == reflect.Uint8
}

func (r *schemaReflector) schemaType(typ reflect.Type, component bool) *base.SchemaProxy {
	if typ == nil {
		return base.CreateSchemaProxy(&base.Schema{})
	}
	if typ.Kind() == reflect.Pointer {
		return nullableSchema(r.schemaType(typ.Elem(), true))
	}
	if typ == _TIME_TYPE {
		return base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}, Format: "date-time"})
	}
	if typ == _RAW_MESSAGE_TYPE {
		return base.CreateSchemaProxy(&base.Schema{})
	}
	if component && typ.Name() != "" && typ.PkgPath() != "" {
		return r.component(typ)
	}

	var schema *base.Schema
	switch typ.Kind() {
	case reflect.Interface:
		schema = &base.Schema{}
	case reflect.Bool:
		schema = &base.Schema{Type: []string{"boolean"}}
	case reflect.String:
		schema = &base.Schema{Type: []string{"string"}}
	case reflect.Int8, reflect.Int16, reflect.Int32:
		schema = &base.Schema{Type: []string{"integer"}, Format: "int32"}
	case reflect.Int, reflect.Int64:
		schema = &base.Schema{Type: []string{"integer"}, Format: "int64"}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		minimum := float64(0)
		schema = &base.Schema{Type: []string{"integer"}, Format: "int32", Minimum: &minimum}
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		minimum := float64(0)
		schema = &base.Schema{Type: []string{"integer"}, Format: "int64", Minimum: &minimum}
	case reflect.Float32:
		schema = &base.Schema{Type: []string{"number"}, Format: "float"}
	case reflect.Float64:
		schema = &base.Schema{Type: []string{"number"}, Format: "double"}
	case reflect.Array, reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 {
			schema = &base.Schema{
				Type:             []string{"string"},
				ContentEncoding:  "base64",
				ContentMediaType: "application/octet-stream",
			}
			break
		}
		schema = &base.Schema{
			Type: []string{"array"},
			Items: &base.DynamicValue[*base.SchemaProxy, bool]{
				A: r.schemaType(typ.Elem(), true),
			},
		}
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			r.setError(fmt.Errorf("cannot reflect map with %s key", typ.Key()))
			return base.CreateSchemaProxy(&base.Schema{})
		}
		schema = &base.Schema{
			Type: []string{"object"},
			AdditionalProperties: &base.DynamicValue[*base.SchemaProxy, bool]{
				A: r.schemaType(typ.Elem(), true),
			},
		}
	case reflect.Struct:
		schema = &base.Schema{
			Type:       []string{"object"},
			Properties: orderedmap.New[string, *base.SchemaProxy](),
		}
		r.addStructFields(schema, typ, false)
	default:
		r.setError(fmt.Errorf("cannot reflect OpenAPI schema for %s", typ))
		schema = &base.Schema{}
	}
	return base.CreateSchemaProxy(schema)
}

func (r *schemaReflector) component(typ reflect.Type) *base.SchemaProxy {
	if name, ok := r.byType[typ]; ok {
		return base.CreateSchemaProxyRef("#/components/schemas/" + name)
	}
	name := typ.Name()
	if override := r.overrides[typ]; override != "" {
		name = override
	}
	if previous, ok := r.byName[name]; ok && previous != typ {
		r.setError(fmt.Errorf(
			"OpenAPI schema component %q is used by both %s and %s",
			name, previous, typ,
		))
		return base.CreateSchemaProxyRef("#/components/schemas/" + name)
	}
	r.byType[typ] = name
	r.byName[name] = typ
	proxy := r.schemaType(typ, false)
	if previous, ok := r.schemas.Get(name); ok {
		equal, err := semanticEqual(previous, proxy)
		if err != nil {
			r.setError(fmt.Errorf("compare OpenAPI schema component %q: %w", name, err))
		} else if !equal {
			r.setError(fmt.Errorf("incompatible OpenAPI component collision at schemas.%s", name))
		}
	} else {
		r.schemas.Set(name, proxy)
	}
	return base.CreateSchemaProxyRef("#/components/schemas/" + name)
}

func (r *schemaReflector) setName(typ reflect.Type, name string) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Name() == "" || typ.PkgPath() == "" {
		return fmt.Errorf("OpenAPI component name can only be set for a named Go type")
	}
	if !_COMPONENT_NAME.MatchString(name) {
		return fmt.Errorf("invalid OpenAPI component name %q", name)
	}
	r.overrides[typ] = name
	return nil
}

func (r *schemaReflector) addStructFields(schema *base.Schema, typ reflect.Type, optional bool) {
	for field := range typ.Fields() {
		if field.PkgPath != "" {
			continue
		}
		name, options, ignored := jsonField(field)
		if ignored {
			continue
		}
		if field.Anonymous && name == "" {
			embedded := field.Type
			pointer := false
			for embedded.Kind() == reflect.Pointer {
				pointer = true
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				r.addStructFields(schema, embedded, optional || pointer)
				continue
			}
		}
		if name == "" {
			name = field.Name
		}
		fieldSchema := r.schemaType(field.Type, true)
		if hasSchemaTags(field) {
			fieldSchema = decorateSchema(fieldSchema, field, r)
		}
		schema.Properties.Set(name, fieldSchema)
		required, explicit := field.Tag.Lookup("required")
		switch {
		case explicit && required == "true":
			schema.Required = append(schema.Required, name)
		case explicit && required == "false":
		case !optional && !options["omitempty"] && field.Type.Kind() != reflect.Pointer:
			schema.Required = append(schema.Required, name)
		}
	}
}

func jsonField(field reflect.StructField) (string, map[string]bool, bool) {
	value, ok := field.Tag.Lookup("json")
	if !ok {
		return "", map[string]bool{}, false
	}
	parts := strings.Split(value, ",")
	if parts[0] == "-" {
		return "", nil, true
	}
	options := make(map[string]bool, len(parts)-1)
	for _, option := range parts[1:] {
		options[option] = true
	}
	return parts[0], options, false
}

func requestLocation(field reflect.StructField) (string, map[string]bool, bool, error) {
	location, options, ignored := jsonField(field)
	if ignored {
		return "", options, true, nil
	}
	legacy, ok := field.Tag.Lookup("mizu")
	if !ok {
		return location, options, false, nil
	}
	legacy = strings.Split(legacy, ",")[0]
	if location != "" && legacy != "" && location != legacy {
		return "", nil, false, fmt.Errorf(
			"conflicting request location tags on %s: json=%q mizu=%q",
			field.Name, location, legacy,
		)
	}
	if legacy != "" {
		location = legacy
	}
	return location, options, false, nil
}

func requestFieldName(field reflect.StructField, location mizutag) (string, bool, error) {
	name, _, ignored := jsonField(field)
	if ignored {
		return "", true, nil
	}
	legacy := strings.Split(field.Tag.Get(location.String()), ",")[0]
	if name != "" && legacy != "" && name != legacy {
		return "", false, fmt.Errorf(
			"conflicting %s parameter tags on %s: json=%q %s=%q",
			location, field.Name, name, location, legacy,
		)
	}
	if legacy != "" {
		name = legacy
	}
	if name == "" {
		name = field.Name
	}
	return name, false, nil
}

func hasSchemaTags(field reflect.StructField) bool {
	for _, key := range []string{
		"desc", "deprecated", "format", "pattern", "default", "example", "enum",
		"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems",
	} {
		if _, ok := field.Tag.Lookup(key); ok {
			return true
		}
	}
	return false
}

func decorateSchema(proxy *base.SchemaProxy, field reflect.StructField, reflector *schemaReflector) *base.SchemaProxy {
	decorated := &base.Schema{AllOf: []*base.SchemaProxy{proxy}}
	decorated.Description = field.Tag.Get("desc")
	decorated.Format = field.Tag.Get("format")
	decorated.Pattern = field.Tag.Get("pattern")
	if value, ok := field.Tag.Lookup("deprecated"); ok {
		deprecated, err := strconv.ParseBool(value)
		if err != nil {
			reflector.setError(fmt.Errorf("parse deprecated tag on %s: %w", field.Name, err))
		} else {
			decorated.Deprecated = &deprecated
		}
	}
	if value, ok := field.Tag.Lookup("default"); ok {
		decorated.Default = tagNode(value)
	}
	if value, ok := field.Tag.Lookup("example"); ok {
		decorated.Examples = []*yaml.Node{tagNode(value)}
	}
	if value, ok := field.Tag.Lookup("enum"); ok {
		for item := range strings.SplitSeq(value, ",") {
			decorated.Enum = append(decorated.Enum, tagNode(strings.TrimSpace(item)))
		}
	}
	parseFloatTag := func(key string, target **float64) {
		value, ok := field.Tag.Lookup(key)
		if !ok {
			return
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			reflector.setError(fmt.Errorf("parse %s tag on %s: %w", key, field.Name, err))
			return
		}
		*target = &parsed
	}
	parseIntTag := func(key string, target **int64) {
		value, ok := field.Tag.Lookup(key)
		if !ok {
			return
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			reflector.setError(fmt.Errorf("parse %s tag on %s: %w", key, field.Name, err))
			return
		}
		*target = &parsed
	}
	parseFloatTag("minimum", &decorated.Minimum)
	parseFloatTag("maximum", &decorated.Maximum)
	parseIntTag("minLength", &decorated.MinLength)
	parseIntTag("maxLength", &decorated.MaxLength)
	parseIntTag("minItems", &decorated.MinItems)
	parseIntTag("maxItems", &decorated.MaxItems)
	return base.CreateSchemaProxy(decorated)
}

func tagNode(value string) *yaml.Node {
	var decoded any
	if err := yaml.Unmarshal([]byte(value), &decoded); err != nil {
		decoded = value
	}
	node := new(yaml.Node)
	_ = node.Encode(decoded)
	return node
}

func nullableSchema(proxy *base.SchemaProxy) *base.SchemaProxy {
	return base.CreateSchemaProxy(&base.Schema{
		AnyOf: []*base.SchemaProxy{
			proxy,
			base.CreateSchemaProxy(&base.Schema{Type: []string{"null"}}),
		},
	})
}

func (r *schemaReflector) setError(err error) {
	if r.err == nil {
		r.err = err
	}
}
