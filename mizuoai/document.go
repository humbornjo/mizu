package mizuoai

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi-validator/schema_validation"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

var _PATH_PARAMETER = regexp.MustCompile(`\{([^}/]+)(?:\.\.\.)?\}`)

// OpenApiDocument is a parsed OpenAPI 3.0, 3.1, or 3.2 document that can supply
// authoritative operations and reusable components to registrations.
type OpenApiDocument struct {
	data       []byte
	model      *v3.Document
	raw        *yaml.Node
	components map[string]*yaml.Node
	operations map[string]*documentOperation
}

type documentOperation struct {
	method        string
	path          string
	pathItem      *v3.PathItem
	operation     *v3.Operation
	components    *v3.Components
	tags          []*base.Tag
	rawOverlay    *yaml.Node
	rawComponents map[string]*yaml.Node
	inputVersion  string
}

// ParseOpenAPI parses an OpenAPI 3.0, 3.1, or 3.2 document and indexes its
// operations by operationId. Output is normalized to the configured target
// version when the document is registered.
func ParseOpenAPI(data []byte) (*OpenApiDocument, error) {
	if len(data) == 0 {
		return nil, errors.New("openapi document is empty")
	}
	raw, err := parseYamlDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parse openapi document: %w", err)
	}
	document, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parse openapi document: %w", err)
	}
	valid, validationErrors := schema_validation.ValidateOpenAPIDocument(document)
	if !valid {
		errs := make([]error, 0, len(validationErrors))
		for _, validationError := range validationErrors {
			errs = append(errs, validationError)
		}
		return nil, fmt.Errorf("validate openapi document: %w", errors.Join(errs...))
	}
	sanitized, err := sanitizedLibopenapiData(data)
	if err != nil {
		return nil, fmt.Errorf("prepare openapi document: %w", err)
	}
	document, err = libopenapi.NewDocument(sanitized)
	if err != nil {
		return nil, fmt.Errorf("parse openapi document model: %w", err)
	}
	model, err := document.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("build openapi document: %w", err)
	}
	if err := validateInputVersion(model.Model.Version); err != nil {
		return nil, err
	}

	parsed := &OpenApiDocument{
		data:       append([]byte(nil), data...),
		model:      &model.Model,
		raw:        raw,
		components: rawComponents(raw),
		operations: make(map[string]*documentOperation),
	}
	if mappingValue(documentMap(raw), "paths") == nil {
		return parsed, nil
	}
	rawOperations, err := rawDocumentOperations(raw)
	if err != nil {
		return nil, err
	}
	for _, entry := range rawOperations {
		if entry.operationId == "" {
			continue
		}
		if previous, ok := parsed.operations[entry.operationId]; ok {
			return nil, fmt.Errorf(
				"duplicate openapi operationId %q at %s %s and %s %s",
				entry.operationId, previous.method, previous.path, entry.method, entry.path,
			)
		}
		var item *v3.PathItem
		if model.Model.Paths != nil && model.Model.Paths.PathItems != nil {
			item, _ = model.Model.Paths.PathItems.Get(entry.path)
		}
		var operation *v3.Operation
		if item != nil && item.GetOperations() != nil {
			operation, _ = item.GetOperations().Get(strings.ToLower(entry.method))
		}
		if operation == nil {
			operation, err = buildRawOperationModel(raw, entry)
			if err != nil {
				return nil, fmt.Errorf("build operation %q: %w", entry.operationId, err)
			}
		}
		effective := *operation
		if effective.Security == nil && model.Model.Security != nil {
			effective.Security = model.Model.Security
		}
		if len(effective.Servers) == 0 && (item == nil || len(item.Servers) == 0) && len(model.Model.Servers) > 0 {
			effective.Servers = model.Model.Servers
		}
		parsed.operations[entry.operationId] = &documentOperation{
			method:       entry.method,
			path:         entry.path,
			pathItem:     item,
			operation:    &effective,
			inputVersion: model.Model.Version,
		}
	}
	for _, operation := range parsed.operations {
		components, err := collectOperationComponents(&model.Model, operation)
		if err != nil {
			return nil, fmt.Errorf("collect components for operation %q: %w", operation.operation.OperationId, err)
		}
		operation.components = components
		operation.tags = collectOperationTags(model.Model.Tags, operation.operation.Tags)
		overlay, rawComponents, err := collectRawOperationOverlay(
			raw, operation.path, operation.operation.OperationId, operation.operation,
		)
		if err != nil {
			return nil, fmt.Errorf("collect raw operation %q: %w", operation.operation.OperationId, err)
		}
		operation.rawOverlay = overlay
		operation.rawComponents = rawComponents
	}
	return parsed, nil
}

// MustParseOpenAPI parses data or panics. It is intended for embedded,
// generated contracts initialized at process startup.
func MustParseOpenAPI(data []byte) *OpenApiDocument {
	document, err := ParseOpenAPI(data)
	if err != nil {
		panic(err)
	}
	return document
}

// Version returns the source document's OpenAPI version.
func (d *OpenApiDocument) Version() string {
	if d == nil || d.model == nil {
		return ""
	}
	return d.model.Version
}

// Model returns the parsed libopenapi model. Callers should treat it as
// immutable after it has been used for registration.
func (d *OpenApiDocument) Model() *v3.Document {
	if d == nil {
		return nil
	}
	return d.model
}

func validateInputVersion(version string) error {
	if strings.HasPrefix(version, "3.0.") || strings.HasPrefix(version, "3.1.") ||
		strings.HasPrefix(version, "3.2.") {
		return nil
	}
	return fmt.Errorf("unsupported OpenAPI input version %q: expected 3.0.x, 3.1.x, or 3.2.x", version)
}

func validateTargetVersion(version string) error {
	if strings.HasPrefix(version, "3.1.") || strings.HasPrefix(version, "3.2.") {
		return nil
	}
	return fmt.Errorf("unsupported OpenAPI output version %q: expected 3.1.x or 3.2.x", version)
}

func newComponents() *v3.Components {
	return &v3.Components{
		Schemas:         orderedmap.New[string, *base.SchemaProxy](),
		Responses:       orderedmap.New[string, *v3.Response](),
		Parameters:      orderedmap.New[string, *v3.Parameter](),
		Examples:        orderedmap.New[string, *base.Example](),
		RequestBodies:   orderedmap.New[string, *v3.RequestBody](),
		Headers:         orderedmap.New[string, *v3.Header](),
		SecuritySchemes: orderedmap.New[string, *v3.SecurityScheme](),
		Links:           orderedmap.New[string, *v3.Link](),
		Callbacks:       orderedmap.New[string, *v3.Callback](),
		PathItems:       orderedmap.New[string, *v3.PathItem](),
		MediaTypes:      orderedmap.New[string, *v3.MediaType](),
		Extensions:      orderedmap.New[string, *yaml.Node](),
	}
}

func collectOperationComponents(
	document *v3.Document, operation *documentOperation,
) (*v3.Components, error) {
	result := newComponents()
	if document.Components == nil {
		return result, nil
	}
	references := make([]string, 0)
	appendReferences := func(value any) error {
		found, err := componentReferences(value)
		if err != nil {
			return err
		}
		references = append(references, found...)
		return nil
	}
	if err := appendReferences(operation.operation); err != nil {
		return nil, err
	}
	if operation.pathItem != nil {
		if err := appendReferences(operation.pathItem.Parameters); err != nil {
			return nil, err
		}
	}
	for _, requirement := range operation.operation.Security {
		if requirement == nil || requirement.Requirements == nil {
			continue
		}
		for name := range requirement.Requirements.KeysFromOldest() {
			references = append(references, "#/components/securitySchemes/"+name)
		}
	}

	seen := make(map[string]bool)
	for len(references) > 0 {
		reference := references[0]
		references = references[1:]
		kind, name, ok := parseComponentReference(reference)
		if !ok || seen[kind+"/"+name] {
			continue
		}
		seen[kind+"/"+name] = true
		value, err := copyComponent(document.Components, result, kind, name)
		if err != nil {
			return nil, err
		}
		found, err := componentReferences(value)
		if err != nil {
			return nil, err
		}
		references = append(references, found...)
	}
	return result, nil
}

func componentReferences(value any) ([]string, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	result := make([]string, 0)
	var visit func(any, bool)
	visit = func(value any, referenceValues bool) {
		switch current := value.(type) {
		case string:
			if referenceValues && strings.HasPrefix(current, "#/components/") {
				result = append(result, current)
			}
		case []any:
			for _, item := range current {
				visit(item, referenceValues)
			}
		case map[string]any:
			for key, item := range current {
				if referenceValues {
					visit(item, true)
					continue
				}
				switch key {
				case "$ref", "$dynamicRef", "defaultMapping":
					visit(item, true)
				case "mapping":
					visit(item, true)
				default:
					visit(item, false)
				}
			}
		}
	}
	visit(decoded, false)
	return result, nil
}

func parseComponentReference(reference string) (string, string, bool) {
	parts := strings.Split(reference, "/")
	if len(parts) < 4 || parts[0] != "#" || parts[1] != "components" {
		return "", "", false
	}
	name := strings.ReplaceAll(strings.ReplaceAll(parts[3], "~1", "/"), "~0", "~")
	return parts[2], name, name != ""
}

func copyComponent(source, target *v3.Components, kind, name string) (any, error) {
	source = ensureComponents(source)
	target = ensureComponents(target)
	switch kind {
	case "schemas":
		return copyNamedComponent(kind, name, source.Schemas, target.Schemas)
	case "responses":
		return copyNamedComponent(kind, name, source.Responses, target.Responses)
	case "parameters":
		return copyNamedComponent(kind, name, source.Parameters, target.Parameters)
	case "examples":
		return copyNamedComponent(kind, name, source.Examples, target.Examples)
	case "requestBodies":
		return copyNamedComponent(kind, name, source.RequestBodies, target.RequestBodies)
	case "headers":
		return copyNamedComponent(kind, name, source.Headers, target.Headers)
	case "securitySchemes":
		return copyNamedComponent(kind, name, source.SecuritySchemes, target.SecuritySchemes)
	case "links":
		return copyNamedComponent(kind, name, source.Links, target.Links)
	case "callbacks":
		return copyNamedComponent(kind, name, source.Callbacks, target.Callbacks)
	case "pathItems":
		return copyNamedComponent(kind, name, source.PathItems, target.PathItems)
	case "mediaTypes":
		return copyNamedComponent(kind, name, source.MediaTypes, target.MediaTypes)
	default:
		return nil, fmt.Errorf("unsupported OpenAPI component reference %q", kind)
	}
}

func copyNamedComponent[T any](
	kind, name string, source, target *orderedmap.Map[string, T],
) (T, error) {
	value, ok := source.Get(name)
	if !ok {
		var zero T
		return zero, fmt.Errorf("unresolved OpenAPI component reference %s.%s", kind, name)
	}
	target.Set(name, value)
	return value, nil
}

func collectOperationTags(documentTags []*base.Tag, operationTags []string) []*base.Tag {
	needed := make(map[string]bool, len(operationTags))
	for _, name := range operationTags {
		needed[name] = true
	}
	for {
		changed := false
		for _, tag := range documentTags {
			if tag != nil && needed[tag.Name] && tag.Parent != "" && !needed[tag.Parent] {
				needed[tag.Parent] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	result := make([]*base.Tag, 0, len(needed))
	for _, tag := range documentTags {
		if tag != nil && needed[tag.Name] {
			result = append(result, tag)
		}
	}
	return result
}

func ensureComponents(components *v3.Components) *v3.Components {
	if components == nil {
		components = newComponents()
	}
	if components.Schemas == nil {
		components.Schemas = orderedmap.New[string, *base.SchemaProxy]()
	}
	if components.Responses == nil {
		components.Responses = orderedmap.New[string, *v3.Response]()
	}
	if components.Parameters == nil {
		components.Parameters = orderedmap.New[string, *v3.Parameter]()
	}
	if components.Examples == nil {
		components.Examples = orderedmap.New[string, *base.Example]()
	}
	if components.RequestBodies == nil {
		components.RequestBodies = orderedmap.New[string, *v3.RequestBody]()
	}
	if components.Headers == nil {
		components.Headers = orderedmap.New[string, *v3.Header]()
	}
	if components.SecuritySchemes == nil {
		components.SecuritySchemes = orderedmap.New[string, *v3.SecurityScheme]()
	}
	if components.Links == nil {
		components.Links = orderedmap.New[string, *v3.Link]()
	}
	if components.Callbacks == nil {
		components.Callbacks = orderedmap.New[string, *v3.Callback]()
	}
	if components.PathItems == nil {
		components.PathItems = orderedmap.New[string, *v3.PathItem]()
	}
	if components.MediaTypes == nil {
		components.MediaTypes = orderedmap.New[string, *v3.MediaType]()
	}
	if components.Extensions == nil {
		components.Extensions = orderedmap.New[string, *yaml.Node]()
	}
	return components
}

func mergeComponents(target, source *v3.Components) error {
	if source == nil {
		return nil
	}
	target = ensureComponents(target)
	if err := mergeComponentMap("schemas", target.Schemas, source.Schemas); err != nil {
		return err
	}
	if err := mergeComponentMap("responses", target.Responses, source.Responses); err != nil {
		return err
	}
	if err := mergeComponentMap("parameters", target.Parameters, source.Parameters); err != nil {
		return err
	}
	if err := mergeComponentMap("examples", target.Examples, source.Examples); err != nil {
		return err
	}
	if err := mergeComponentMap("requestBodies", target.RequestBodies, source.RequestBodies); err != nil {
		return err
	}
	if err := mergeComponentMap("headers", target.Headers, source.Headers); err != nil {
		return err
	}
	if err := mergeComponentMap("securitySchemes", target.SecuritySchemes, source.SecuritySchemes); err != nil {
		return err
	}
	if err := mergeComponentMap("links", target.Links, source.Links); err != nil {
		return err
	}
	if err := mergeComponentMap("callbacks", target.Callbacks, source.Callbacks); err != nil {
		return err
	}
	if err := mergeComponentMap("pathItems", target.PathItems, source.PathItems); err != nil {
		return err
	}
	if err := mergeComponentMap("mediaTypes", target.MediaTypes, source.MediaTypes); err != nil {
		return err
	}
	return mergeComponentMap("extensions", target.Extensions, source.Extensions)
}

func cloneComponents(source *v3.Components) *v3.Components {
	result := newComponents()
	if source == nil {
		return result
	}
	cloneComponentMap(result.Schemas, source.Schemas)
	cloneComponentMap(result.Responses, source.Responses)
	cloneComponentMap(result.Parameters, source.Parameters)
	cloneComponentMap(result.Examples, source.Examples)
	cloneComponentMap(result.RequestBodies, source.RequestBodies)
	cloneComponentMap(result.Headers, source.Headers)
	cloneComponentMap(result.SecuritySchemes, source.SecuritySchemes)
	cloneComponentMap(result.Links, source.Links)
	cloneComponentMap(result.Callbacks, source.Callbacks)
	cloneComponentMap(result.PathItems, source.PathItems)
	cloneComponentMap(result.MediaTypes, source.MediaTypes)
	cloneComponentMap(result.Extensions, source.Extensions)
	return result
}

func cloneComponentMap[T any](target, source *orderedmap.Map[string, T]) {
	if source == nil {
		return
	}
	for name, value := range source.FromOldest() {
		target.Set(name, value)
	}
}

func mergeComponentMap[T any](
	kind string, target, source *orderedmap.Map[string, T],
) error {
	if source == nil {
		return nil
	}
	for name, value := range source.FromOldest() {
		if previous, ok := target.Get(name); ok {
			equal, err := semanticEqual(previous, value)
			if err != nil {
				return fmt.Errorf("compare OpenAPI component %s.%s: %w", kind, name, err)
			}
			if !equal {
				return fmt.Errorf("incompatible OpenAPI component collision at %s.%s", kind, name)
			}
			continue
		}
		target.Set(name, value)
	}
	return nil
}

func semanticEqual(a, b any) (bool, error) {
	aData, err := yaml.Marshal(a)
	if err != nil {
		return false, err
	}
	bData, err := yaml.Marshal(b)
	if err != nil {
		return false, err
	}
	var aValue, bValue any
	if err := yaml.Unmarshal(aData, &aValue); err != nil {
		return false, err
	}
	if err := yaml.Unmarshal(bData, &bValue); err != nil {
		return false, err
	}
	return reflect.DeepEqual(aValue, bValue), nil
}

func cloneOperation(source *v3.Operation) *v3.Operation {
	if source == nil {
		return nil
	}
	operation := *source
	operation.Tags = append([]string(nil), source.Tags...)
	operation.Parameters = append([]*v3.Parameter(nil), source.Parameters...)
	operation.Security = append([]*base.SecurityRequirement(nil), source.Security...)
	operation.Servers = append([]*v3.Server(nil), source.Servers...)
	if source.Deprecated != nil {
		deprecated := *source.Deprecated
		operation.Deprecated = &deprecated
	}
	if source.Callbacks != nil {
		operation.Callbacks = orderedmap.New[string, *v3.Callback]()
		for name, callback := range source.Callbacks.FromOldest() {
			operation.Callbacks.Set(name, callback)
		}
	}
	if source.Extensions != nil {
		operation.Extensions = orderedmap.New[string, *yaml.Node]()
		cloneComponentMap(operation.Extensions, source.Extensions)
	}
	if source.Responses != nil {
		responses := *source.Responses
		if source.Responses.Codes != nil {
			responses.Codes = orderedmap.New[string, *v3.Response]()
			for code, response := range source.Responses.Codes.FromOldest() {
				responses.Codes.Set(code, response)
			}
		}
		if source.Responses.Extensions != nil {
			responses.Extensions = orderedmap.New[string, *yaml.Node]()
			cloneComponentMap(responses.Extensions, source.Responses.Extensions)
		}
		operation.Responses = &responses
	}
	return &operation
}

func supplementOperation(authoritative, supplemental *v3.Operation) (*v3.Operation, error) {
	result := cloneOperation(authoritative)
	if result == nil {
		result = new(v3.Operation)
	}
	if supplemental == nil {
		return result, nil
	}
	mergeString := func(name string, target *string, value string) error {
		if value == "" {
			return nil
		}
		if *target != "" && *target != value {
			return fmt.Errorf("conflicting OpenAPI operation %s", name)
		}
		*target = value
		return nil
	}
	if err := mergeString("summary", &result.Summary, supplemental.Summary); err != nil {
		return nil, err
	}
	if err := mergeString("description", &result.Description, supplemental.Description); err != nil {
		return nil, err
	}
	if err := mergeString("operationId", &result.OperationId, supplemental.OperationId); err != nil {
		return nil, err
	}
	for _, tag := range supplemental.Tags {
		if !slices.Contains(result.Tags, tag) {
			result.Tags = append(result.Tags, tag)
		}
	}
	if err := mergeOptionalValue("externalDocs", &result.ExternalDocs, supplemental.ExternalDocs); err != nil {
		return nil, err
	}
	parameters, err := mergeParameters(result.Parameters, supplemental.Parameters)
	if err != nil {
		return nil, err
	}
	result.Parameters = parameters
	if err := mergeOptionalValue("requestBody", &result.RequestBody, supplemental.RequestBody); err != nil {
		return nil, err
	}
	result.Responses, err = mergeResponses(result.Responses, supplemental.Responses)
	if err != nil {
		return nil, err
	}
	result.Callbacks, err = mergeNamedValues("callback", result.Callbacks, supplemental.Callbacks)
	if err != nil {
		return nil, err
	}
	if err := mergeOptionalValue("deprecated", &result.Deprecated, supplemental.Deprecated); err != nil {
		return nil, err
	}
	result.Security, err = mergeValueList("security", result.Security, supplemental.Security)
	if err != nil {
		return nil, err
	}
	result.Servers, err = mergeValueList("server", result.Servers, supplemental.Servers)
	if err != nil {
		return nil, err
	}
	result.Extensions, err = mergeNamedValues("extension", result.Extensions, supplemental.Extensions)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func mergeOptionalValue[T any](name string, target **T, value *T) error {
	if value == nil {
		return nil
	}
	if *target == nil {
		*target = value
		return nil
	}
	equal, err := semanticEqual(*target, value)
	if err != nil {
		return err
	}
	if !equal {
		return fmt.Errorf("conflicting OpenAPI operation %s", name)
	}
	return nil
}

func mergeParameters(target, source []*v3.Parameter) ([]*v3.Parameter, error) {
	result := append([]*v3.Parameter(nil), target...)
	index := make(map[string]*v3.Parameter)
	for _, parameter := range result {
		index[parameterIdentity(parameter)] = parameter
	}
	for _, parameter := range source {
		key := parameterIdentity(parameter)
		if previous := index[key]; previous != nil {
			equal, err := semanticEqual(previous, parameter)
			if err != nil {
				return nil, err
			}
			if !equal {
				return nil, fmt.Errorf("conflicting OpenAPI operation parameter %s", key)
			}
			continue
		}
		index[key] = parameter
		result = append(result, parameter)
	}
	return result, nil
}

func parameterIdentity(parameter *v3.Parameter) string {
	if parameter == nil {
		return "<nil>"
	}
	if parameter.Reference != "" {
		return parameter.Reference
	}
	return parameter.In + "/" + parameter.Name
}

func mergeResponses(target, source *v3.Responses) (*v3.Responses, error) {
	if target == nil {
		if source == nil {
			return nil, nil
		}
		return cloneOperation(&v3.Operation{Responses: source}).Responses, nil
	}
	result := cloneOperation(&v3.Operation{Responses: target}).Responses
	if source == nil {
		return result, nil
	}
	if err := mergeOptionalValue("default response", &result.Default, source.Default); err != nil {
		return nil, err
	}
	var err error
	result.Codes, err = mergeNamedValues("response", result.Codes, source.Codes)
	if err != nil {
		return nil, err
	}
	result.Extensions, err = mergeNamedValues("response extension", result.Extensions, source.Extensions)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func mergeNamedValues[T any](
	kind string, target, source *orderedmap.Map[string, T],
) (*orderedmap.Map[string, T], error) {
	if target == nil && source == nil {
		return nil, nil
	}
	result := orderedmap.New[string, T]()
	cloneComponentMap(result, target)
	if source == nil {
		return result, nil
	}
	for name, value := range source.FromOldest() {
		if previous, ok := result.Get(name); ok {
			equal, err := semanticEqual(previous, value)
			if err != nil {
				return nil, err
			}
			if !equal {
				return nil, fmt.Errorf("conflicting OpenAPI operation %s %s", kind, name)
			}
			continue
		}
		result.Set(name, value)
	}
	return result, nil
}

func mergeValueList[T any](kind string, target, source []T) ([]T, error) {
	result := append([]T(nil), target...)
	for _, value := range source {
		found := false
		for _, previous := range result {
			equal, err := semanticEqual(previous, value)
			if err != nil {
				return nil, err
			}
			if equal {
				found = true
				break
			}
		}
		if !found {
			result = append(result, value)
		}
	}
	return result, nil
}

func validateResponses(responses *v3.Responses, location string) error {
	if responses == nil ||
		((responses.Codes == nil || responses.Codes.Len() == 0) && responses.Default == nil) {
		return fmt.Errorf("OpenAPI operation %s has no responses", location)
	}
	if responses.Codes != nil {
		for code, response := range responses.Codes.FromOldest() {
			if response != nil && response.Reference == "" && response.Description == "" {
				return fmt.Errorf("OpenAPI response %s on %s has no description", code, location)
			}
		}
	}
	if responses.Default != nil && responses.Default.Reference == "" && responses.Default.Description == "" {
		return fmt.Errorf("OpenAPI default response on %s has no description", location)
	}
	return nil
}

func (c *oaiConfig) addOperation(operation *operationConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if operation.err != nil {
		return operation.err
	}
	if !strings.HasPrefix(operation.path, "/") {
		return fmt.Errorf("OpenAPI path %q must start with /", operation.path)
	}
	if operation.external != nil && operation.external.path != "" {
		if !strings.EqualFold(operation.method, operation.external.method) {
			return fmt.Errorf(
				"OpenAPI operation %q uses method %s, registered as %s",
				operation.OperationId, operation.external.method, operation.method,
			)
		}
		if operation.path != operation.external.path {
			return fmt.Errorf(
				"OpenAPI operation %q uses path %s, registered as %s",
				operation.OperationId, operation.external.path, operation.path,
			)
		}
	}
	if operation.external != nil {
		merged, err := supplementOperation(operation.external.operation, &operation.Operation)
		if err != nil {
			return fmt.Errorf("authoritative OpenAPI operation %q: %w", operation.OperationId, err)
		}
		operation.Operation = *merged
	}
	location := operation.method + " " + operation.path
	if c.routes[location] {
		return fmt.Errorf("duplicate OpenAPI operation %s", location)
	}
	if operation.OperationId != "" {
		if previous, ok := c.operationIds[operation.OperationId]; ok {
			return fmt.Errorf(
				"duplicate OpenAPI operationId %q at %s and %s",
				operation.OperationId, previous, location,
			)
		}
	}
	if err := validateResponses(operation.Responses, location); err != nil {
		return err
	}
	candidateComponents := cloneComponents(c.components)
	if err := mergeComponents(candidateComponents, operation.components); err != nil {
		return err
	}
	for key, component := range operation.externalRawComponents() {
		if previous := c.rawComponents[key]; previous != nil {
			equal, err := yamlNodeEqual(previous, component)
			if err != nil {
				return fmt.Errorf("compare raw OpenAPI component %s: %w", key, err)
			}
			if !equal {
				return fmt.Errorf("incompatible OpenAPI component collision at %s", key)
			}
		}
	}
	validated := *operation
	validated.components = candidateComponents
	if err := validateParameterDuplicates(validated.pathItem, validated.Parameters, candidateComponents); err != nil {
		return err
	}
	if err := validatePathParameters(&validated); err != nil {
		return err
	}
	if _, err := collectOperationComponents(&v3.Document{Components: candidateComponents}, &documentOperation{
		operation: &validated.Operation,
		pathItem:  validated.pathItem,
	}); err != nil {
		return err
	}
	candidateTags := append([]*base.Tag(nil), c.tags...)
	if err := mergeTags(&candidateTags, operation.documentTags); err != nil {
		return err
	}
	previousComponents := c.components
	previousTags := c.tags
	c.components = candidateComponents
	c.tags = candidateTags
	c.handlers = append(c.handlers, operation)
	if _, err := c.renderUnlocked(false); err != nil {
		c.handlers = c.handlers[:len(c.handlers)-1]
		c.components = previousComponents
		c.tags = previousTags
		c.reflector.schemas = c.components.Schemas
		return err
	}
	c.reflector.schemas = c.components.Schemas
	if operation.OperationId != "" {
		c.operationIds[operation.OperationId] = location
	}
	c.routes[location] = true
	for key, component := range operation.externalRawComponents() {
		c.rawComponents[key] = component
	}
	return nil
}

func (c *operationConfig) externalRawComponents() map[string]*yaml.Node {
	if c.external == nil {
		return nil
	}
	return c.external.rawComponents
}

func mergeTags(target *[]*base.Tag, source []*base.Tag) error {
	for _, tag := range source {
		if tag == nil {
			continue
		}
		found := false
		for _, previous := range *target {
			if previous == nil || previous.Name != tag.Name {
				continue
			}
			found = true
			equal, err := semanticEqual(previous, tag)
			if err != nil {
				return err
			}
			if !equal {
				return fmt.Errorf("incompatible OpenAPI tag collision at %s", tag.Name)
			}
			break
		}
		if !found {
			*target = append(*target, tag)
		}
	}
	return nil
}

func validatePathParameters(operation *operationConfig) error {
	parameters := operation.Parameters
	if operation.pathItem != nil {
		parameters = append(append([]*v3.Parameter(nil), operation.pathItem.Parameters...), parameters...)
	}
	defined := make(map[string]bool)
	for _, parameter := range parameters {
		parameter = resolveParameter(operation.components, parameter)
		if parameter == nil || parameter.In != "path" {
			continue
		}
		if parameter.Required == nil || !*parameter.Required {
			return fmt.Errorf("path parameter %q on %s must be required", parameter.Name, operation.path)
		}
		defined[parameter.Name] = true
	}
	placeholders := make(map[string]bool)
	for _, match := range _PATH_PARAMETER.FindAllStringSubmatch(operation.path, -1) {
		placeholders[match[1]] = true
		if !defined[match[1]] {
			return fmt.Errorf("OpenAPI operation %s %s is missing path parameter %q", operation.method, operation.path, match[1])
		}
	}
	for name := range defined {
		if !placeholders[name] {
			return fmt.Errorf("OpenAPI path parameter %q is not present in path %s", name, operation.path)
		}
	}
	return nil
}

func validateParameterDuplicates(
	pathItem *v3.PathItem, operationParameters []*v3.Parameter, components *v3.Components,
) error {
	validate := func(location string, parameters []*v3.Parameter) error {
		seen := make(map[string]bool)
		for _, parameter := range parameters {
			resolved := resolveParameter(components, parameter)
			if resolved == nil {
				continue
			}
			key := resolved.In + "/" + resolved.Name
			if seen[key] {
				return fmt.Errorf("duplicate OpenAPI %s parameter %s", location, key)
			}
			seen[key] = true
		}
		return nil
	}
	if pathItem != nil {
		if err := validate("path item", pathItem.Parameters); err != nil {
			return err
		}
	}
	return validate("operation", operationParameters)
}

func resolveParameter(components *v3.Components, parameter *v3.Parameter) *v3.Parameter {
	if parameter == nil || parameter.Reference == "" || components == nil {
		return parameter
	}
	kind, name, ok := parseComponentReference(parameter.Reference)
	if !ok || kind != "parameters" || components.Parameters == nil {
		return parameter
	}
	resolved, ok := components.Parameters.Get(name)
	if !ok {
		return parameter
	}
	return resolved
}

func setPathOperation(item *v3.PathItem, method string, operation *v3.Operation) error {
	var previous *v3.Operation
	switch method {
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
	case "QUERY":
		previous, item.Query = item.Query, operation
	default:
		if item.AdditionalOperations == nil {
			item.AdditionalOperations = orderedmap.New[string, *v3.Operation]()
		}
		key := strings.ToLower(method)
		previous, _ = item.AdditionalOperations.Get(key)
		item.AdditionalOperations.Set(key, operation)
	}
	if previous == nil {
		return nil
	}
	equal, err := semanticEqual(previous, operation)
	if err != nil {
		return err
	}
	if equal {
		return nil
	}
	return fmt.Errorf("OpenAPI path already contains %s operation", method)
}

func clonePathItem(source *v3.PathItem) *v3.PathItem {
	if source == nil {
		return &v3.PathItem{}
	}
	item := *source
	item.Parameters = append([]*v3.Parameter(nil), source.Parameters...)
	item.Servers = append([]*v3.Server(nil), source.Servers...)
	if source.Extensions != nil {
		item.Extensions = orderedmap.New[string, *yaml.Node]()
		cloneComponentMap(item.Extensions, source.Extensions)
	}
	if source.AdditionalOperations != nil {
		item.AdditionalOperations = orderedmap.New[string, *v3.Operation]()
		cloneComponentMap(item.AdditionalOperations, source.AdditionalOperations)
	}
	return &item
}

func mergePathMetadata(target, source *v3.PathItem) error {
	if source == nil {
		return nil
	}
	if target.Summary != "" && source.Summary != "" && target.Summary != source.Summary {
		return errors.New("conflicting OpenAPI path summaries")
	}
	if target.Description != "" && source.Description != "" && target.Description != source.Description {
		return errors.New("conflicting OpenAPI path descriptions")
	}
	if target.Summary == "" {
		target.Summary = source.Summary
	}
	if target.Description == "" {
		target.Description = source.Description
	}
	if len(source.Parameters) > 0 {
		if len(target.Parameters) == 0 {
			target.Parameters = source.Parameters
		} else {
			equal, err := semanticEqual(target.Parameters, source.Parameters)
			if err != nil || !equal {
				return errors.New("conflicting OpenAPI path parameters")
			}
		}
	}
	if len(source.Servers) > 0 {
		if len(target.Servers) == 0 {
			target.Servers = source.Servers
		} else {
			equal, err := semanticEqual(target.Servers, source.Servers)
			if err != nil || !equal {
				return errors.New("conflicting OpenAPI path servers")
			}
		}
	}
	if source.Extensions != nil {
		if target.Extensions == nil {
			target.Extensions = orderedmap.New[string, *yaml.Node]()
		}
		if err := mergeComponentMap("path extensions", target.Extensions, source.Extensions); err != nil {
			return err
		}
	}
	return nil
}

func mergePathItem(target, source *v3.PathItem) error {
	if source == nil {
		return nil
	}
	if target.Reference != "" && source.Reference != "" && target.Reference != source.Reference {
		return errors.New("conflicting OpenAPI path references")
	}
	if target.Reference == "" {
		target.Reference = source.Reference
	}
	if err := mergePathMetadata(target, source); err != nil {
		return err
	}
	operations := source.GetOperations()
	if operations == nil {
		return nil
	}
	for method, operation := range operations.FromOldest() {
		if err := setPathOperation(target, strings.ToUpper(method), operation); err != nil {
			return err
		}
	}
	return nil
}
