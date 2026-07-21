package mizucue

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/openapi"
)

// Schema is a compiled CUE schema used for model validation and OpenAPI generation.
type Schema struct {
	context     *cue.Context
	value       cue.Value
	openapiOnce sync.Once
	openapi     map[string]map[string]any
	openapiErr  error
}

func newSchema(context *cue.Context, value cue.Value) *Schema {
	return &Schema{context: context, value: value}
}

// Compile compiles an inline CUE schema.
func Compile(raw string) (*Schema, error) {
	context := cuecontext.New()
	value := context.CompileString(raw)
	if err := value.Err(); err != nil {
		return nil, fmt.Errorf("compile CUE schema: %w", err)
	}
	return newSchema(context, value), nil
}

// MustCompile compiles an inline CUE schema or panics on failure.
func MustCompile(raw string) *Schema {
	schema, err := Compile(raw)
	if err != nil {
		panic(err)
	}
	return schema
}

var _OPENAPI_PATH_PARAMETER = regexp.MustCompile(`\{([^{}]+)\}`)

// GenerateOpenAPI combines CUE-generated component schemas with operations
// declared by quoted double-underscore fields on request definitions.
//
//nolint:gocyclo // Projection, supplemental generation, and hint errors retain their local context.
func GenerateOpenAPI(schema *Schema, title, version, componentPrefix string) ([]byte, error) {
	config := &openapi.Config{
		Version:       "3.1.0",
		Info:          map[string]any{"title": title, "version": version},
		SelfContained: true,
	}
	config.NameFunc = func(_ cue.Value, path cue.Path) string {
		return openAPIComponentName(path, componentPrefix)
	}
	value := schema.value
	definitions, err := value.Fields(cue.Definitions(true), cue.Hidden(true), cue.Optional(true))
	if err != nil {
		return nil, fmt.Errorf("read CUE definitions for OpenAPI projection: %w", err)
	}
	operationDefinitions := make([]string, 0)
	selectedDefinitions := make(map[string]bool)
	aliases := make(map[string]string)
	for definitions.Next() {
		selector := definitions.Selector()
		if !selector.IsDefinition() {
			continue
		}
		fields, err := definitions.Value().Fields(cue.Hidden(true), cue.Optional(true))
		if err != nil {
			return nil, fmt.Errorf("read OpenAPI operation hints on %s: %w", selector, err)
		}
		for fields.Next() {
			name := fields.Selector().String()
			if fields.Selector().IsString() {
				name = fields.Selector().Unquoted()
			}
			if strings.HasPrefix(name, "__") {
				definition := strings.TrimPrefix(selector.String(), "#")
				operationDefinitions = append(operationDefinitions, definition)
				break
			}
		}
	}
	if len(operationDefinitions) > 0 {
		value = schema.context.CompileString("{}")
		for _, definition := range operationDefinitions {
			if !strings.HasSuffix(definition, "Request") {
				continue
			}
			names := []string{definition, strings.TrimSuffix(definition, "Request") + "Response"}
			for len(names) > 0 {
				name := names[0]
				names = names[1:]
				if selectedDefinitions[name] {
					continue
				}
				selectedDefinitions[name] = true
				path := cue.ParsePath("#" + name)
				selected := schema.value.LookupPath(path)
				if err := selected.Err(); err != nil {
					continue
				}
				if identifier, ok := selected.Syntax(cue.Raw()).(*ast.Ident); ok && strings.HasPrefix(identifier.Name, "#") {
					aliases[name] = strings.TrimPrefix(identifier.Name, "#")
				}
				value = value.FillPath(path, selected)
				if err := value.Err(); err != nil {
					return nil, fmt.Errorf("build OpenAPI projection %s: %w", name, err)
				}
				ast.Walk(selected.Syntax(cue.Raw()), func(node ast.Node) bool {
					identifier, ok := node.(*ast.Ident)
					if ok && strings.HasPrefix(identifier.Name, "#") {
						names = append(names, strings.TrimPrefix(identifier.Name, "#"))
					}
					return true
				}, nil)
			}
		}
	}
	file, err := openapi.Generate(value, config)
	if err != nil {
		return nil, fmt.Errorf("generate OpenAPI components: %w", err)
	}
	generated := schema.context.BuildFile(file)
	if err := generated.Err(); err != nil {
		return nil, fmt.Errorf("build generated OpenAPI components: %w", err)
	}
	generatedData, err := generated.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("encode generated OpenAPI components: %w", err)
	}
	var document map[string]any
	if err := json.Unmarshal(generatedData, &document); err != nil {
		return nil, fmt.Errorf("decode generated OpenAPI components: %w", err)
	}
	components, _ := document["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(selectedDefinitions)) {
		component := componentPrefix + name
		if _, ok := schemas[component]; ok {
			continue
		}
		path := cue.ParsePath("#" + name)
		selected := schema.value.LookupPath(path)
		if err := selected.Err(); err != nil {
			continue
		}
		single := schema.context.CompileString("{}").FillPath(path, selected)
		file, err := openapi.Generate(single, config)
		if err != nil {
			return nil, fmt.Errorf("generate OpenAPI component %s: %w", name, err)
		}
		generated := schema.context.BuildFile(file)
		if err := generated.Err(); err != nil {
			return nil, fmt.Errorf("build generated OpenAPI component %s: %w", name, err)
		}
		data, err := generated.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("encode generated OpenAPI component %s: %w", name, err)
		}
		var supplemental map[string]any
		if err := json.Unmarshal(data, &supplemental); err != nil {
			return nil, fmt.Errorf("decode generated OpenAPI component %s: %w", name, err)
		}
		supplementalComponents, _ := supplemental["components"].(map[string]any)
		supplementalSchemas, _ := supplementalComponents["schemas"].(map[string]any)
		if err := mergeOpenAPI(schemas, supplementalSchemas, "components.schemas"); err != nil {
			return nil, err
		}
	}
	for alias, target := range aliases {
		if _, ok := schemas[componentPrefix+target]; ok {
			schemas[componentPrefix+alias] = map[string]any{
				"$ref": "#/components/schemas/" + componentPrefix + target,
			}
		}
	}
	if err := applyOpenAPIHints(schema.value, document, componentPrefix); err != nil {
		return nil, err
	}
	if err := applyOpenAPIOperations(schema.value, document, componentPrefix); err != nil {
		return nil, err
	}
	result, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode OpenAPI document: %w", err)
	}
	return result, nil
}

func openAPIComponentName(path cue.Path, prefix string) string {
	parts := make([]string, 0, len(path.Selectors()))
	for _, selector := range path.Selectors() {
		if selector.Type().IsHidden() {
			return ""
		}
		parts = append(parts, strings.TrimPrefix(selector.String(), "#"))
	}
	return prefix + strings.Join(parts, ".")
}

func applyOpenAPIHints(value cue.Value, document map[string]any, componentPrefix string) error {
	components, ok := document["components"].(map[string]any)
	if !ok {
		return nil
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		return nil
	}
	definitions, err := value.Fields(cue.Definitions(true), cue.Hidden(true), cue.Optional(true))
	if err != nil {
		return fmt.Errorf("read CUE definitions for OpenAPI hints: %w", err)
	}
	for definitions.Next() {
		selector := definitions.Selector()
		if !selector.IsDefinition() {
			continue
		}
		name := componentPrefix + strings.TrimPrefix(selector.String(), "#")
		component, ok := schemas[name].(map[string]any)
		if !ok {
			continue
		}
		if err := applyOpenAPIFieldHints(definitions.Value(), component, schemas, "components.schemas."+name); err != nil {
			return err
		}
	}
	return nil
}

func applyOpenAPIFieldHints(value cue.Value, schema map[string]any, schemas map[string]any, path string) error {
	resolved, err := resolveOpenAPISchema(schemas, schema, path)
	if err != nil {
		return err
	}
	schema = resolved
	properties, _ := schema["properties"].(map[string]any)
	fields, err := value.Fields(cue.Definitions(false), cue.Hidden(true), cue.Optional(true))
	if err != nil {
		return fmt.Errorf("read CUE fields for OpenAPI hints at %s: %w", path, err)
	}
	regular := make(map[string]cue.Value)
	hints := make(map[string]cue.Value)
	for fields.Next() {
		selector := fields.Selector()
		if selector.Type().IsHidden() {
			name := selector.String()
			if strings.HasPrefix(name, "__") {
				continue
			}
			hints[strings.TrimPrefix(name, "_")] = fields.Value()
			continue
		}
		if selector.IsString() {
			name := selector.Unquoted()
			if strings.HasPrefix(name, "__") {
				continue
			}
			regular[name] = fields.Value()
		}
	}
	for name, hint := range hints {
		property, ok := properties[name].(map[string]any)
		if !ok {
			return fmt.Errorf("OpenAPI hint %s._%s has no generated field", path, name)
		}
		addition, err := decodeOpenAPIObject(hint, path+"._"+name)
		if err != nil {
			return err
		}
		if err := mergeOpenAPI(property, addition, path+".properties."+name); err != nil {
			return err
		}
	}
	if properties == nil {
		return nil
	}
	for name, field := range regular {
		property, ok := properties[name].(map[string]any)
		if !ok {
			continue
		}
		if err := applyOpenAPIFieldHints(field, property, schemas, path+".properties."+name); err != nil {
			return err
		}
	}
	return nil
}

//nolint:gocyclo // Operation validation stays linear so each failure names its owning definition.
func applyOpenAPIOperations(value cue.Value, document map[string]any, componentPrefix string) error {
	components, ok := document["components"].(map[string]any)
	if !ok {
		components = make(map[string]any)
		document["components"] = components
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		schemas = make(map[string]any)
		components["schemas"] = schemas
	}
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		paths = make(map[string]any)
		document["paths"] = paths
	}
	definitions, err := value.Fields(cue.Definitions(true), cue.Hidden(true), cue.Optional(true))
	if err != nil {
		return fmt.Errorf("read CUE definitions for OpenAPI operations: %w", err)
	}
	for definitions.Next() {
		selector := definitions.Selector()
		if !selector.IsDefinition() {
			continue
		}
		definition := strings.TrimPrefix(selector.String(), "#")
		fields, err := definitions.Value().Fields(cue.Hidden(true), cue.Optional(true))
		if err != nil {
			return fmt.Errorf("read OpenAPI operation hints on %s: %w", definition, err)
		}
		hints := make(map[string]cue.Value)
		for fields.Next() {
			selector := fields.Selector()
			name := selector.String()
			if selector.IsString() {
				name = selector.Unquoted()
			}
			if name, ok := strings.CutPrefix(name, "__"); ok {
				hints[name] = fields.Value()
			}
		}
		if len(hints) == 0 {
			continue
		}
		if !strings.HasSuffix(definition, "Request") {
			return fmt.Errorf("OpenAPI operation hints require a Request definition: %s", definition)
		}
		methodValue, hasMethod := hints["method"]
		pathValue, hasPath := hints["path"]
		if !hasMethod || !hasPath {
			return fmt.Errorf("OpenAPI operation %s requires __method and __path", definition)
		}
		method, err := methodValue.String()
		if err != nil {
			return fmt.Errorf("read OpenAPI operation %s __method: %w", definition, err)
		}
		switch method {
		case "get", "post", "put", "delete", "patch", "head", "options", "trace", "connect":
		default:
			return fmt.Errorf("OpenAPI operation %s has unsupported method %q", definition, method)
		}
		path, err := pathValue.String()
		if err != nil {
			return fmt.Errorf("read OpenAPI operation %s __path: %w", definition, err)
		}
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("OpenAPI operation %s path must start with /", definition)
		}
		operation, err := deriveOpenAPIOperation(
			schemas, definitions.Value(), definition, componentPrefix, path,
		)
		if err != nil {
			return err
		}
		additions := make(map[string]any)
		for name, hint := range hints {
			switch name {
			case "method", "path":
				continue
			case "components":
				addition, err := decodeOpenAPIObject(hint, definition+".__components")
				if err != nil {
					return err
				}
				if err := mergeOpenAPI(components, addition, "components"); err != nil {
					return err
				}
			default:
				addition, err := decodeOpenAPIValue(hint, definition+".__"+name)
				if err != nil {
					return err
				}
				additions[name] = addition
			}
		}
		if hinted, ok := additions["parameters"]; ok {
			parameters, ok := hinted.([]any)
			if !ok {
				return fmt.Errorf("OpenAPI operation %s __parameters must be an array", definition)
			}
			existing, _ := operation["parameters"].([]any)
			seen := make(map[string]bool, len(existing)+len(parameters))
			for _, parameter := range append(slices.Clone(existing), parameters...) {
				object, ok := parameter.(map[string]any)
				if !ok {
					return fmt.Errorf("OpenAPI operation %s parameter must be an object", definition)
				}
				name, nameOK := object["name"].(string)
				location, locationOK := object["in"].(string)
				if !nameOK || name == "" || !locationOK || location == "" {
					return fmt.Errorf("OpenAPI operation %s parameter requires name and in", definition)
				}
				key := location + "\x00" + name
				if seen[key] {
					return fmt.Errorf("OpenAPI operation %s has duplicate %s parameter %q", definition, location, name)
				}
				seen[key] = true
			}
			operation["parameters"] = append(existing, parameters...)
			delete(additions, "parameters")
		}
		if err := mergeOpenAPI(operation, additions, "paths."+path+"."+method); err != nil {
			return err
		}
		if operationId, _ := operation["operationId"].(string); operationId == "" {
			return fmt.Errorf("OpenAPI operation %s requires __operationId", definition)
		}
		if err := addOpenAPIResponse(value, schemas, operation, definition, componentPrefix); err != nil {
			return err
		}
		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			pathItem = make(map[string]any)
			paths[path] = pathItem
		}
		if _, exists := pathItem[method]; exists {
			return fmt.Errorf("duplicate OpenAPI operation %s %s", strings.ToUpper(method), path)
		}
		pathItem[method] = operation
		delete(schemas, componentPrefix+definition)
	}
	return nil
}

//nolint:gocyclo // Path and transport derivation share request state and error context.
func deriveOpenAPIOperation(
	schemas map[string]any, value cue.Value, definition, componentPrefix, path string,
) (map[string]any, error) {
	component := componentPrefix + definition
	request, ok := schemas[component]
	if !ok {
		return nil, fmt.Errorf("missing OpenAPI request schema %s", component)
	}
	requestSchema, err := resolveOpenAPISchema(schemas, request, "components.schemas."+component)
	if err != nil {
		return nil, err
	}
	properties, _ := requestSchema["properties"].(map[string]any)
	requestFields := make(map[string]cue.Value)
	optional := make(map[string]bool)
	fields, err := value.Fields(cue.Optional(true))
	if err != nil {
		return nil, fmt.Errorf("read OpenAPI request %s fields: %w", definition, err)
	}
	for fields.Next() {
		selector := fields.Selector()
		if !selector.IsString() {
			continue
		}
		name := selector.Unquoted()
		switch name {
		case "path", "form", "body":
			requestFields[name] = fields.Value()
			optional[name] = fields.IsOptional()
		}
	}
	operation := make(map[string]any)

	pathValue, hasPath := requestFields["path"]
	parameters := make([]any, 0)
	parameterNames := make([]string, 0)
	if hasPath {
		if optional["path"] {
			return nil, fmt.Errorf("OpenAPI request %s path must be required", definition)
		}
		pathSchemaValue, ok := properties["path"]
		if !ok {
			return nil, fmt.Errorf("missing OpenAPI request schema %s.path", definition)
		}
		pathSchema, err := resolveOpenAPISchema(schemas, pathSchemaValue, definition+".path")
		if err != nil {
			return nil, err
		}
		pathProperties, _ := pathSchema["properties"].(map[string]any)
		pathFields, err := pathValue.Fields(cue.Optional(true))
		if err != nil {
			return nil, fmt.Errorf("read OpenAPI request %s path fields: %w", definition, err)
		}
		for pathFields.Next() {
			selector := pathFields.Selector()
			if !selector.IsString() {
				continue
			}
			name := selector.Unquoted()
			if pathFields.IsOptional() {
				return nil, fmt.Errorf("OpenAPI request %s path field %s must be required", definition, name)
			}
			if _, ok := pathProperties[name]; !ok {
				return nil, fmt.Errorf("missing OpenAPI request schema %s.path.%s", definition, name)
			}
			parameterNames = append(parameterNames, name)
		}
		slices.Sort(parameterNames)
		for _, name := range parameterNames {
			parameters = append(parameters, map[string]any{
				"name": name, "in": "path", "required": true, "schema": pathProperties[name],
			})
		}
	}
	placeholderSet := make(map[string]bool)
	for _, match := range _OPENAPI_PATH_PARAMETER.FindAllStringSubmatch(path, -1) {
		if placeholderSet[match[1]] {
			return nil, fmt.Errorf("OpenAPI operation %s has duplicate path placeholder %q", definition, match[1])
		}
		placeholderSet[match[1]] = true
	}
	literalPath := _OPENAPI_PATH_PARAMETER.ReplaceAllString(path, "")
	if strings.ContainsAny(literalPath, "{}") {
		return nil, fmt.Errorf("OpenAPI operation %s has malformed path placeholders", definition)
	}
	if len(placeholderSet) != len(parameterNames) {
		return nil, fmt.Errorf("OpenAPI operation %s path placeholders do not match path fields", definition)
	}
	for _, name := range parameterNames {
		if !placeholderSet[name] {
			return nil, fmt.Errorf("OpenAPI operation %s path placeholders do not match path fields", definition)
		}
	}
	if len(parameters) > 0 {
		operation["parameters"] = parameters
	}

	_, hasForm := requestFields["form"]
	_, hasBody := requestFields["body"]
	if hasForm && hasBody {
		return nil, fmt.Errorf("OpenAPI request %s cannot define both form and body", definition)
	}
	if hasForm || hasBody {
		name, media := "form", "multipart/form-data"
		if hasBody {
			name, media = "body", "application/json"
		}
		if optional[name] {
			return nil, fmt.Errorf("OpenAPI request %s %s must be required", definition, name)
		}
		schemaValue, ok := properties[name]
		if !ok {
			return nil, fmt.Errorf("missing OpenAPI request schema %s.%s", definition, name)
		}
		mediaType := map[string]any{"schema": schemaValue}
		if hasForm {
			formSchema, err := resolveOpenAPISchema(schemas, schemaValue, definition+".form")
			if err != nil {
				return nil, err
			}
			formProperties, _ := formSchema["properties"].(map[string]any)
			encoding := make(map[string]any)
			for field, property := range formProperties {
				propertySchema, err := resolveOpenAPISchema(schemas, property, definition+".form."+field)
				if err != nil {
					return nil, err
				}
				if contentType, ok := propertySchema["contentMediaType"].(string); ok {
					encoding[field] = map[string]any{"contentType": contentType}
				}
			}
			if len(encoding) > 0 {
				mediaType["encoding"] = encoding
			}
		}
		operation["requestBody"] = map[string]any{
			"required": true, "content": map[string]any{media: mediaType},
		}
	}
	return operation, nil
}

func addOpenAPIResponse(
	value cue.Value, schemas map[string]any, operation map[string]any, definition, componentPrefix string,
) error {
	responses, ok := operation["responses"].(map[string]any)
	if !ok {
		return fmt.Errorf("OpenAPI operation %s requires __responses", definition)
	}
	successes := make([]string, 0, 1)
	for status := range responses {
		code, err := strconv.Atoi(status)
		if err == nil && (code == http.StatusSwitchingProtocols || code >= 200 && code < 300) {
			successes = append(successes, status)
		}
	}
	if len(successes) != 1 {
		return fmt.Errorf("OpenAPI operation %s requires exactly one successful response", definition)
	}
	responseDefinition := strings.TrimSuffix(definition, "Request") + "Response"
	responseValue := value.LookupPath(cue.ParsePath("#" + responseDefinition))
	if err := responseValue.Err(); err != nil {
		return fmt.Errorf("lookup OpenAPI response %s: %w", responseDefinition, err)
	}
	response, ok := responses[successes[0]].(map[string]any)
	if !ok {
		return fmt.Errorf("OpenAPI operation %s response %s must be an object", definition, successes[0])
	}
	if _, exists := response["content"]; exists {
		return nil
	}
	component := componentPrefix + responseDefinition
	if _, ok := schemas[component]; !ok {
		_, reference := responseValue.ReferencePath()
		if len(reference.Selectors()) > 0 {
			component = openAPIComponentName(reference, componentPrefix)
		}
	}
	if _, ok := schemas[component]; !ok {
		return fmt.Errorf("missing OpenAPI response schema %s", responseDefinition)
	}
	response["content"] = map[string]any{
		"application/json": map[string]any{
			"schema": map[string]any{"$ref": "#/components/schemas/" + component},
		},
	}
	return nil
}

func resolveOpenAPISchema(schemas map[string]any, value any, path string) (map[string]any, error) {
	seen := make(map[string]bool)
	for {
		schema, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OpenAPI schema %s must be an object", path)
		}
		reference, ok := schema["$ref"].(string)
		if !ok {
			return schema, nil
		}
		const prefix = "#/components/schemas/"
		name, ok := strings.CutPrefix(reference, prefix)
		if !ok {
			return nil, fmt.Errorf("OpenAPI schema %s has unsupported reference %q", path, reference)
		}
		if seen[name] {
			return nil, fmt.Errorf("OpenAPI schema %s has cyclic reference %q", path, reference)
		}
		seen[name] = true
		var exists bool
		value, exists = schemas[name]
		if !exists {
			return nil, fmt.Errorf("OpenAPI schema %s references missing component %s", path, name)
		}
	}
}

func decodeOpenAPIObject(value cue.Value, path string) (map[string]any, error) {
	decoded, err := decodeOpenAPIValue(value, path)
	if err != nil {
		return nil, err
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("OpenAPI hint %s must be an object", path)
	}
	return object, nil
}

func decodeOpenAPIValue(value cue.Value, path string) (any, error) {
	data, err := value.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("encode OpenAPI hint %s: %w", path, err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode OpenAPI hint %s: %w", path, err)
	}
	return decoded, nil
}

// MustGenerateOpenAPI generates a CUE-owned OpenAPI document or panics during
// package initialization.
func MustGenerateOpenAPI(schema *Schema, title, version, componentPrefix string) []byte {
	document, err := GenerateOpenAPI(schema, title, version, componentPrefix)
	if err != nil {
		panic(err)
	}
	return document
}

func mergeOpenAPI(target, additions map[string]any, path string) error {
	for key, addition := range additions {
		location := key
		if path != "" {
			location = path + "." + key
		}
		current, exists := target[key]
		if !exists {
			target[key] = addition
			continue
		}
		currentMap, currentOK := current.(map[string]any)
		additionMap, additionOK := addition.(map[string]any)
		if currentOK && additionOK {
			if err := mergeOpenAPI(currentMap, additionMap, location); err != nil {
				return err
			}
			continue
		}
		if !reflect.DeepEqual(current, addition) {
			return fmt.Errorf("conflicting OpenAPI value at %s", location)
		}
	}
	return nil
}

// LoadFS loads and compiles one CUE package from fsys.
func LoadFS(dir string, fsys fs.FS) (*Schema, error) {
	instances := load.Instances([]string{"."}, &load.Config{Dir: dir, FS: fsys})
	if len(instances) != 1 {
		return nil, fmt.Errorf("load CUE package %s: got %d instances", dir, len(instances))
	}
	if err := instances[0].Err; err != nil {
		return nil, fmt.Errorf("load CUE package %s: %w", dir, err)
	}
	context := cuecontext.New()
	value := context.BuildInstance(instances[0])
	if err := value.Err(); err != nil {
		return nil, fmt.Errorf("compile CUE package %s: %w", dir, err)
	}
	return newSchema(context, value), nil
}

// MustLoadFS loads and compiles one CUE package from fsys or panics on failure.
func MustLoadFS(dir string, fsys fs.FS) *Schema {
	schema, err := LoadFS(dir, fsys)
	if err != nil {
		panic(err)
	}
	return schema
}

// Validate checks a generated Go model against its same-named CUE definition.
func Validate[T any](schema *Schema, value T) error {
	typ := reflect.TypeOf(value)
	if typ == nil {
		return fmt.Errorf("validate model: nil value")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	definition := schema.value.LookupPath(cue.ParsePath("#" + typ.Name()))
	if err := definition.Err(); err != nil {
		return fmt.Errorf("lookup model schema %s: %w", typ.Name(), err)
	}
	unified := definition.Unify(schema.context.Encode(value))
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return fmt.Errorf("validate %s: %w", typ.Name(), err)
	}
	return nil
}

// ExtractOpenAPI returns the generated OpenAPI component for value's concrete Go type.
func ExtractOpenAPI[T any](schema *Schema, value T) (map[string]any, error) {
	schema.openapiOnce.Do(func() {
		file, err := openapi.Generate(schema.value, &openapi.Config{})
		if err != nil {
			schema.openapiErr = err
			return
		}
		top := schema.context.BuildFile(file)
		if err := top.Err(); err != nil {
			schema.openapiErr = err
			return
		}
		document := struct {
			Components struct {
				Schemas map[string]map[string]any `json:"schemas"`
			} `json:"components"`
		}{}
		if err := top.Decode(&document); err != nil {
			schema.openapiErr = err
			return
		}
		schema.openapi = document.Components.Schemas
	})
	if schema.openapiErr != nil {
		return nil, schema.openapiErr
	}
	typ := reflect.TypeOf(value)
	if typ == nil {
		return nil, fmt.Errorf("extract OpenAPI model: nil value")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	params, ok := schema.openapi[typ.Name()]
	if !ok {
		return nil, fmt.Errorf("missing schema: %s", typ.Name())
	}
	out := maps.Clone(params)
	if _, ok := out["properties"]; !ok {
		out["properties"] = map[string]any{}
	}
	return out, nil
}

// MustExtractOpenAPI returns the generated OpenAPI component or panics on failure.
func MustExtractOpenAPI[T any](schema *Schema, value T) map[string]any {
	out, err := ExtractOpenAPI(schema, value)
	if err != nil {
		panic(err)
	}
	return out
}
