package internal

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"

	"github.com/humbornjo/mizu/mizuoai/internal/serde"
)

func NewComponents() *v3.Components {
	return &v3.Components{}
}

// CanonicalTypeName returns the canonical OpenAPI-compatible name of
// t as "<import path>.<type name>" —
// "github.com.pb33f.libopenapi.datamodel.high.v3.Document". Pointers
// are dereferenced; built-in and unnamed types fall back to their
// reflect name. OpenAPI component keys must match ^[a-zA-Z0-9.\-_]+$:
// "/" joins the import path with ".", any other disallowed run
// collapses to "_".
func CanonicalTypeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	name, pkgPath := t.Name(), t.PkgPath()
	full := pkgPath + "." + name
	if pkgPath == "" {
		full = name
		if name == "" {
			full = t.String()
		}
	}

	full = strings.ReplaceAll(full, "/", ".")
	var b strings.Builder
	disallowed := false
	for _, r := range full {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '-' || r == '_'
		if !allowed {
			disallowed = true
			continue
		}
		if disallowed {
			b.WriteByte('_')
			disallowed = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CanonicalSchema maps t to its inline OpenAPI schema. Pointer chains
// are dereferenced and union the resolved type with "null"; free-form
// schemas stay empty rather than narrowing to "null" only. Well-known
// types short-circuit before the kind switch.
func CanonicalSchema(t reflect.Type) highbase.Schema {
	nullable := false
	for t.Kind() == reflect.Pointer {
		nullable = true
		t = t.Elem()
	}
	unionNull := func(schema highbase.Schema) highbase.Schema {
		if nullable && len(schema.Type) == 1 {
			schema.Type = append(schema.Type, "null")
		}
		return schema
	}

	switch t {
	case reflect.TypeFor[time.Time]():
		return unionNull(highbase.Schema{Type: []string{"string"}, Format: "date-time"})
	case reflect.TypeFor[json.RawMessage]():
		return highbase.Schema{}
	}

	switch t.Kind() {
	case reflect.Bool:
		return unionNull(highbase.Schema{Type: []string{"boolean"}})
	case reflect.String:
		return unionNull(highbase.Schema{Type: []string{"string"}})
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return unionNull(highbase.Schema{Type: []string{"integer"}, Format: "int32"})
	case reflect.Int, reflect.Int64:
		return unionNull(highbase.Schema{Type: []string{"integer"}, Format: "int64"})
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		zero := float64(0)
		return unionNull(highbase.Schema{Type: []string{"integer"}, Format: "int32", Minimum: &zero})
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		zero := float64(0)
		return unionNull(highbase.Schema{Type: []string{"integer"}, Format: "int64", Minimum: &zero})
	case reflect.Float32:
		return unionNull(highbase.Schema{Type: []string{"number"}, Format: "float"})
	case reflect.Float64:
		return unionNull(highbase.Schema{Type: []string{"number"}, Format: "double"})
	case reflect.Interface:
		return highbase.Schema{}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return unionNull(highbase.Schema{
				Type:             []string{"string"},
				ContentEncoding:  "base64",
				ContentMediaType: "application/octet-stream",
			})
		}
		item := CanonicalSchema(t.Elem())
		return unionNull(highbase.Schema{
			Type:  []string{"array"},
			Items: &highbase.DynamicValue[*highbase.SchemaProxy, bool]{A: highbase.CreateSchemaProxy(&item)},
		})
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("cannot reflect map with %s key", t.Key()))
		}
		value := CanonicalSchema(t.Elem())
		return unionNull(highbase.Schema{
			Type: []string{"object"},
			AdditionalProperties: &highbase.DynamicValue[*highbase.SchemaProxy, bool]{
				A: highbase.CreateSchemaProxy(&value),
			},
		})
	case reflect.Struct:
		schema := highbase.Schema{Type: []string{"object"}}

		// TODO: cycle guard — recursive types (Next *Node, type M
		// map[string]M) recurse until a fatal stack overflow. Plan:
		// thread a seen-stack of named reflect.Type through the
		// recursion (canonicalSchema(t, seen)), panicking on re-entry
		// with the type named. The real fix is refs at the caller
		// (RegisterSchema emits $ref for named structs instead of
		// inlining); the guard stays afterward for named map/slice
		// recursion.
		SchemaStructFields(&schema, t, false)
		return unionNull(schema)
	default:
		panic(fmt.Sprintf("cannot reflect OpenAPI schema for %s", t))
	}
}

// SchemaStructFields walks t's exported fields into schema's
// Properties and Required list, following encoding/json naming: the
// json tag name wins, "-" skips, untagged fields keep their Go name.
// An anonymous struct field without a tag name flattens into the
// parent, and flattening through a pointer marks every promoted field
// optional. A field is required unless the required tag says
// otherwise, or it is omitempty, a pointer, or promoted through a
// pointer-embedded struct.
func SchemaStructFields(schema *highbase.Schema, t reflect.Type, optional bool) {
	for field := range t.Fields() {
		if field.PkgPath != "" {
			continue
		}
		if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name == "-" {
			continue
		}
		name, options := serde.JsonField(field)

		if field.Anonymous && name == "" {
			embedded := field.Type
			embeddedOptional := optional
			for embedded.Kind() == reflect.Pointer {
				embeddedOptional = true
				embedded = embedded.Elem()
			}
			switch embedded {
			case reflect.TypeFor[time.Time](), reflect.TypeFor[json.RawMessage]():
				// Well-known leaf types marshal as a field named
				// after the type, never flattened.
			default:
				if embedded.Kind() == reflect.Struct {
					SchemaStructFields(schema, embedded, embeddedOptional)
					continue
				}
			}
		}
		if name == "" {
			name = field.Name
		}

		fieldSchema := CanonicalSchema(field.Type)
		if schema.Properties == nil {
			schema.Properties = orderedmap.New[string, *highbase.SchemaProxy]()
		}
		schema.Properties.Set(name, highbase.CreateSchemaProxy(&fieldSchema))

		required := !optional && field.Type.Kind() != reflect.Pointer && !slices.Contains(options, "omitempty")
		if value, ok := field.Tag.Lookup("required"); ok {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				panic(fmt.Sprintf("parse required tag on %s: %v", field.Name, err))
			}
			required = parsed
		}
		if required {
			schema.Required = append(schema.Required, name)
		}
	}
}

// CanonicalParameters extracts OpenAPI parameters from a handler
// input type following serde conventions: top-level fields tagged
// "path", "query", or "header" are parameter groups, and every
// exported group field with a json name becomes one parameter at that
// location — untagged fields are skipped, mirroring serde's decoding.
// body/form/untagged/ignored top-level fields are skipped. Path
// parameters are always required; other locations follow an explicit
// required tag, then pointer/omitempty optionality.
func CanonicalParameters(input reflect.Type) []*v3.Parameter {
	var parameters []*v3.Parameter
	for field := range input.Fields() {
		if field.PkgPath != "" {
			continue
		}
		jsonTagValue, _ := serde.JsonField(field)
		var in string
		switch serde.TagType(jsonTagValue) {
		case serde.TAG_PATH, serde.TAG_QUERY, serde.TAG_HEADER:
			in = jsonTagValue
		default:
			continue
		}
		group := field.Type
		for group.Kind() == reflect.Pointer {
			group = group.Elem()
		}
		for groupField := range group.Fields() {
			if groupField.PkgPath != "" {
				continue
			}
			name, options := serde.JsonField(groupField)
			if name == "" {
				continue
			}
			required := in == serde.TAG_PATH.String()
			if !required {
				if value, ok := groupField.Tag.Lookup("required"); ok {
					parsed, err := strconv.ParseBool(value)
					if err != nil {
						panic(fmt.Sprintf("parse required tag on %s: %v", groupField.Name, err))
					}
					required = parsed
				} else {
					required = !slices.Contains(options, "omitempty") &&
						groupField.Type.Kind() != reflect.Pointer
				}
			}
			fieldSchema := CanonicalSchema(groupField.Type)
			parameters = append(parameters, &v3.Parameter{
				Name:     name,
				In:       in,
				Required: &required,
				Schema:   highbase.CreateSchemaProxy(&fieldSchema),
			})
		}
	}
	return parameters
}

// CanonicalExtensions converts specification extensions into their
// canonical OpenAPI form: every key carries the "x-" prefix and every
// value is a YAML node, in surface order. Values that are already
// *yaml.Node are deep-cloned rather than re-encoded, so shared graphs
// are never mutated. Returns nil when surface holds no extensions.
//
//	surface := *orderedmap.ToOrderedMap(map[string]any{
//		"internal": true,
//		"x-tier":   map[string]any{"name": "gold", "level": 3},
//	})
//	yaml.Marshal(CanonicalExtensions(surface))
//
//	// x-internal: true
//	// x-tier:
//	//     level: 3
//	//     name: gold
func CanonicalExtensions(surface orderedmap.Map[string, any]) *orderedmap.Map[string, *yaml.Node] {
	if surface.OrderedMap == nil {
		return nil
	}
	extensions := orderedmap.New[string, *yaml.Node]()
	for key, value := range surface.FromOldest() {
		var node *yaml.Node
		if vn, ok := value.(*yaml.Node); ok && vn != nil {
			// Encode aliases and desolves the input graph in place, clone
			// instead of mutating a shared node.
			node = utils.CloneYAMLNode(vn)
		} else {
			node = &yaml.Node{}
			if err := node.Encode(value); err != nil {
				continue
			}
		}
		if !strings.HasPrefix(key, "x-") {
			key = "x-" + key
		}
		extensions.Set(key, node)
	}
	return extensions
}

// MergeOperation overlays the non-zero fields of override on base;
// either nil means empty. Lists override wholesale — a reflected
// base never sets them. Parameters merge by (name, in): an override
// parameter replaces the base one with the same key, new keys
// append. Responses overlay per status code: override codes add to
// or replace base codes, untouched codes survive. Extensions merge
// key-wise.
func MergeOperation(base, override *v3.Operation) *v3.Operation {
	merged := &v3.Operation{}
	if base != nil {
		*merged = *base
	}
	if override == nil {
		return merged
	}

	if len(override.Tags) > 0 {
		merged.Tags = override.Tags
	}
	if override.Summary != "" {
		merged.Summary = override.Summary
	}
	if override.Description != "" {
		merged.Description = override.Description
	}
	if override.ExternalDocs != nil {
		merged.ExternalDocs = override.ExternalDocs
	}
	if override.OperationId != "" {
		merged.OperationId = override.OperationId
	}
	if len(override.Parameters) > 0 {
		parameters := append([]*v3.Parameter(nil), merged.Parameters...)
		for _, parameter := range override.Parameters {
			replaced := false
			for i, existing := range parameters {
				if existing.Name == parameter.Name && existing.In == parameter.In {
					parameters[i] = parameter
					replaced = true
					break
				}
			}
			if !replaced {
				parameters = append(parameters, parameter)
			}
		}
		merged.Parameters = parameters
	}
	if override.RequestBody != nil {
		merged.RequestBody = override.RequestBody
	}
	if override.Responses != nil {
		responses := &v3.Responses{Codes: orderedmap.New[string, *v3.Response]()}
		if merged.Responses != nil {
			responses.Default = merged.Responses.Default
			for code, response := range merged.Responses.Codes.FromOldest() {
				responses.Codes.Set(code, response)
			}
		}
		for code, response := range override.Responses.Codes.FromOldest() {
			responses.Codes.Set(code, response)
		}
		if override.Responses.Default != nil {
			responses.Default = override.Responses.Default
		}
		merged.Responses = responses
	}
	if override.Callbacks != nil {
		merged.Callbacks = override.Callbacks
	}
	if override.Deprecated != nil {
		merged.Deprecated = override.Deprecated
	}
	if len(override.Security) > 0 {
		merged.Security = override.Security
	}
	if len(override.Servers) > 0 {
		merged.Servers = override.Servers
	}
	if override.Extensions != nil {
		extensions := orderedmap.New[string, *yaml.Node]()
		for key, value := range merged.Extensions.FromOldest() {
			extensions.Set(key, value)
		}
		for key, value := range override.Extensions.FromOldest() {
			extensions.Set(key, value)
		}
		merged.Extensions = extensions
	}
	return merged
}

// MergeDocument overlays the non-zero fields of override on base
// and returns the merged document; either nil means empty. Scalar
// fields override when set, Info merges field-wise, list fields
// concatenate base's entries then override's, and map fields merge
// key-wise with override replacing duplicates. Paths carries over
// from base untouched — operations accumulate there at
// registration. The result shares base's Components, whose maps
// gain override's entries in place.
func MergeDocument(base, override *v3.Document) *v3.Document {
	document := &v3.Document{}
	if base != nil {
		*document = *base
	}
	if override == nil {
		override = &v3.Document{}
	}

	if override.Version != "" {
		document.Version = override.Version
	}
	if override.Self != "" {
		document.Self = override.Self
	}
	if override.JsonSchemaDialect != "" {
		document.JsonSchemaDialect = override.JsonSchemaDialect
	}

	info := new(highbase.Info)
	if document.Info != nil {
		*info = *document.Info
	}
	if override.Info != nil {
		if override.Info.Title != "" {
			info.Title = override.Info.Title
		}
		if override.Info.Summary != "" {
			info.Summary = override.Info.Summary
		}
		if override.Info.Description != "" {
			info.Description = override.Info.Description
		}
		if override.Info.Version != "" {
			info.Version = override.Info.Version
		}
		if override.Info.TermsOfService != "" {
			info.TermsOfService = override.Info.TermsOfService
		}
		if override.Info.Contact != nil {
			info.Contact = override.Info.Contact
		}
		if override.Info.License != nil {
			info.License = override.Info.License
		}
		info.Extensions = MergeExtensions(info.Extensions, override.Info.Extensions)
	}
	document.Info = info

	document.Servers = append(append([]*v3.Server{}, document.Servers...), override.Servers...)
	document.Tags = append(append([]*highbase.Tag{}, document.Tags...), override.Tags...)
	document.Security = append(append([]*highbase.SecurityRequirement{}, document.Security...), override.Security...)

	document.Webhooks = MergeComponentMap(document.Webhooks, override.Webhooks)
	document.Extensions = MergeExtensions(document.Extensions, override.Extensions)

	if override.ExternalDocs != nil {
		document.ExternalDocs = override.ExternalDocs
	}

	document.Components = MergeComponents(document.Components, override.Components)

	return document
}

// MergeComponents merges source into target and returns it,
// allocating target when nil. A component name already present is
// replaced.
func MergeComponents(target, source *v3.Components) *v3.Components {
	if target == nil {
		target = NewComponents()
	}
	if source == nil {
		return target
	}
	target.Schemas = MergeComponentMap(target.Schemas, source.Schemas)
	target.Responses = MergeComponentMap(target.Responses, source.Responses)
	target.Parameters = MergeComponentMap(target.Parameters, source.Parameters)
	target.Examples = MergeComponentMap(target.Examples, source.Examples)
	target.RequestBodies = MergeComponentMap(target.RequestBodies, source.RequestBodies)
	target.Headers = MergeComponentMap(target.Headers, source.Headers)
	target.SecuritySchemes = MergeComponentMap(target.SecuritySchemes, source.SecuritySchemes)
	target.Links = MergeComponentMap(target.Links, source.Links)
	target.Callbacks = MergeComponentMap(target.Callbacks, source.Callbacks)
	target.PathItems = MergeComponentMap(target.PathItems, source.PathItems)
	target.MediaTypes = MergeComponentMap(target.MediaTypes, source.MediaTypes)
	target.Extensions = MergeComponentMap(target.Extensions, source.Extensions)
	return target
}

// MergeComponentMap sets every source entry on target, allocating
// the map when nil.
func MergeComponentMap[T any](target, source *orderedmap.Map[string, T]) *orderedmap.Map[string, T] {
	for name, value := range source.FromOldest() {
		if target == nil {
			target = orderedmap.New[string, T]()
		}
		target.Set(name, value)
	}
	return target
}

// MergeExtensions merges override into target key-wise and returns
// the merged map; override entries replace target entries with the
// same key. Returns target unchanged when override is nil.
func MergeExtensions(target, override *orderedmap.Map[string, *yaml.Node]) *orderedmap.Map[string, *yaml.Node] {
	if override == nil {
		return target
	}
	extensions := orderedmap.New[string, *yaml.Node]()
	for key, value := range target.FromOldest() {
		extensions.Set(key, value)
	}
	for key, value := range override.FromOldest() {
		extensions.Set(key, value)
	}
	return extensions
}
