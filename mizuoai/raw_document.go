package mizuoai

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"go.yaml.in/yaml/v4"
)

var _OPENAPI_OPERATION_KEYS = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
	"query": true, "additionalOperations": true,
}

type rawOperationEntry struct {
	path        string
	method      string
	operationId string
	operation   *yaml.Node
}

func parseYamlDocument(data []byte) (*yaml.Node, error) {
	root := new(yaml.Node)
	if err := yaml.Unmarshal(data, root); err != nil {
		return nil, err
	}
	if documentMap(root) == nil {
		return nil, fmt.Errorf("OpenAPI document root must be a mapping")
	}
	return root, nil
}

func documentMap(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

func cloneYamlNode(node *yaml.Node) *yaml.Node {
	seen := make(map[*yaml.Node]*yaml.Node)
	var clone func(*yaml.Node) *yaml.Node
	clone = func(source *yaml.Node) *yaml.Node {
		if source == nil {
			return nil
		}
		if previous, ok := seen[source]; ok {
			return previous
		}
		result := *source
		result.Content = nil
		result.Alias = nil
		seen[source] = &result
		for _, child := range source.Content {
			result.Content = append(result.Content, clone(child))
		}
		result.Alias = clone(source.Alias)
		return &result
	}
	return clone(node)
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = cloneYamlNode(value)
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		cloneYamlNode(value),
	)
}

func deleteMappingValue(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
		return
	}
}

func newDocumentNode() *yaml.Node {
	return &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}},
	}
}

func newMappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func mergeYamlDocuments(baseDocument, overlayDocument *yaml.Node) *yaml.Node {
	if baseDocument == nil {
		return cloneYamlNode(overlayDocument)
	}
	if overlayDocument == nil {
		return cloneYamlNode(baseDocument)
	}
	base := documentMap(baseDocument)
	overlay := documentMap(overlayDocument)
	merged := mergeYamlNodes(base, overlay)
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{merged}}
}

func mergeYamlNodes(base, overlay *yaml.Node) *yaml.Node {
	if base == nil {
		return cloneYamlNode(overlay)
	}
	if overlay == nil {
		return cloneYamlNode(base)
	}
	if base.Kind != yaml.MappingNode || overlay.Kind != yaml.MappingNode {
		return cloneYamlNode(overlay)
	}
	result := cloneYamlNode(base)
	for i := 0; i+1 < len(overlay.Content); i += 2 {
		key := overlay.Content[i].Value
		value := overlay.Content[i+1]
		if previous := mappingValue(result, key); previous != nil {
			setMappingValue(result, key, mergeYamlNodes(previous, value))
			continue
		}
		setMappingValue(result, key, value)
	}
	return result
}

func renderYamlDocument(root *yaml.Node, jsonOutput bool) ([]byte, error) {
	if !jsonOutput {
		return yaml.Marshal(root)
	}
	var value any
	if err := root.Decode(&value); err != nil {
		return nil, err
	}
	return json.MarshalIndent(value, "", "  ")
}

func yamlNodeEqual(a, b *yaml.Node) (bool, error) {
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

func sanitizedLibopenapiData(data []byte) ([]byte, error) {
	root, err := parseYamlDocument(data)
	if err != nil {
		return nil, err
	}
	unsupported := map[string]bool{
		"deviceAuthorization":    true,
		"deviceAuthorizationUrl": true,
		"itemEncoding":           true,
		"prefixEncoding":         true,
	}
	var sanitize func(*yaml.Node)
	sanitize = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); {
				if unsupported[node.Content[i].Value] {
					node.Content = append(node.Content[:i], node.Content[i+2:]...)
					continue
				}
				sanitize(node.Content[i+1])
				i += 2
			}
			return
		}
		for _, child := range node.Content {
			sanitize(child)
		}
	}
	sanitize(root)
	return yaml.Marshal(root)
}

func openApiVersion(root *yaml.Node) string {
	version := mappingValue(documentMap(root), "openapi")
	if version == nil {
		return ""
	}
	return version.Value
}

func normalizeOpenApi30Schemas(root *yaml.Node, inputVersion, targetVersion string) {
	if root == nil || !strings.HasPrefix(inputVersion, "3.0.") ||
		(!strings.HasPrefix(targetVersion, "3.1.") && !strings.HasPrefix(targetVersion, "3.2.")) {
		return
	}
	var visit func(*yaml.Node)
	visit = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i].Value
				value := node.Content[i+1]
				if key == "schema" && value.Kind == yaml.MappingNode {
					normalizeOpenApi30Schema(value)
				} else {
					visit(value)
				}
			}
			return
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	components := mappingValue(documentMap(root), "components")
	if schemas := mappingValue(components, "schemas"); schemas != nil && schemas.Kind == yaml.MappingNode {
		for i := 1; i < len(schemas.Content); i += 2 {
			normalizeOpenApi30Schema(schemas.Content[i])
		}
	}
	visit(documentMap(root))
}

func normalizeOpenApi30Schema(schema *yaml.Node) {
	if schema == nil || schema.Kind != yaml.MappingNode {
		return
	}
	if nullable := mappingValue(schema, "nullable"); nullable != nil {
		if nullable.Value == "true" {
			if schemaType := mappingValue(schema, "type"); schemaType != nil && schemaType.Kind == yaml.ScalarNode {
				schemaType.Kind = yaml.SequenceNode
				schemaType.Tag = "!!seq"
				schemaType.Content = []*yaml.Node{
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: schemaType.Value},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "null"},
				}
				schemaType.Value = ""
			} else {
				original := cloneYamlNode(schema)
				deleteMappingValue(original, "nullable")
				nullSchema := newMappingNode()
				setMappingValue(nullSchema, "type", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "null"})
				schema.Content = nil
				setMappingValue(schema, "anyOf", &yaml.Node{
					Kind: yaml.SequenceNode,
					Tag:  "!!seq",
					Content: []*yaml.Node{
						original,
						nullSchema,
					},
				})
			}
		}
		deleteMappingValue(schema, "nullable")
	}
	normalizeExclusiveBound(schema, "minimum", "exclusiveMinimum")
	normalizeExclusiveBound(schema, "maximum", "exclusiveMaximum")
	for i := 0; i+1 < len(schema.Content); i += 2 {
		key := schema.Content[i].Value
		value := schema.Content[i+1]
		switch key {
		case "properties", "patternProperties", "definitions":
			if value.Kind == yaml.MappingNode {
				for j := 1; j < len(value.Content); j += 2 {
					normalizeOpenApi30Schema(value.Content[j])
				}
			}
		case "items", "additionalProperties", "not":
			normalizeOpenApi30Schema(value)
		case "allOf", "oneOf", "anyOf":
			if value.Kind == yaml.SequenceNode {
				for _, child := range value.Content {
					normalizeOpenApi30Schema(child)
				}
			}
		}
	}
}

func normalizeExclusiveBound(schema *yaml.Node, inclusiveKey, exclusiveKey string) {
	exclusive := mappingValue(schema, exclusiveKey)
	if exclusive == nil || exclusive.Tag != "!!bool" {
		return
	}
	if exclusive.Value == "true" {
		if bound := mappingValue(schema, inclusiveKey); bound != nil {
			setMappingValue(schema, exclusiveKey, bound)
		}
	} else {
		deleteMappingValue(schema, exclusiveKey)
	}
}

func rawComponents(root *yaml.Node) map[string]*yaml.Node {
	result := make(map[string]*yaml.Node)
	components := mappingValue(documentMap(root), "components")
	if components == nil || components.Kind != yaml.MappingNode {
		return result
	}
	for i := 0; i+1 < len(components.Content); i += 2 {
		kind := components.Content[i].Value
		items := components.Content[i+1]
		if items.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(items.Content); j += 2 {
			name := items.Content[j].Value
			result[kind+"/"+name] = cloneYamlNode(items.Content[j+1])
		}
	}
	return result
}

func collectRawOperationOverlay(
	root *yaml.Node, path, operationId string, operation *v3.Operation,
) (*yaml.Node, map[string]*yaml.Node, error) {
	paths := mappingValue(documentMap(root), "paths")
	pathItem := mappingValue(paths, path)
	if pathItem == nil || pathItem.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("raw OpenAPI path %s not found", path)
	}
	pathItem, pathReferences, err := resolveRawPathItem(root, pathItem, make(map[string]bool))
	if err != nil {
		return nil, nil, err
	}
	operationKey, additionalKey, rawOperation := findRawOperation(pathItem, operationId)
	if rawOperation == nil {
		return nil, nil, fmt.Errorf("raw OpenAPI operation %q not found", operationId)
	}

	pathOverlay := cloneYamlNode(pathItem)
	for key := range _OPENAPI_OPERATION_KEYS {
		deleteMappingValue(pathOverlay, key)
	}
	if additionalKey == "" {
		setMappingValue(pathOverlay, operationKey, rawOperation)
	} else {
		additional := newMappingNode()
		setMappingValue(additional, additionalKey, rawOperation)
		setMappingValue(pathOverlay, "additionalOperations", additional)
	}

	overlay := newDocumentNode()
	overlayPaths := newMappingNode()
	setMappingValue(overlayPaths, path, pathOverlay)
	setMappingValue(documentMap(overlay), "paths", overlayPaths)

	available := rawComponents(root)
	references := append([]string(nil), pathReferences...)
	references = append(references, rawComponentReferences(rawOperation)...)
	references = append(references, rawComponentReferences(pathOverlay)...)
	for _, requirement := range operation.Security {
		if requirement == nil || requirement.Requirements == nil {
			continue
		}
		for name := range requirement.Requirements.KeysFromOldest() {
			references = append(references, "#/components/securitySchemes/"+name)
		}
	}
	selected := make(map[string]*yaml.Node)
	for len(references) > 0 {
		reference := references[0]
		references = references[1:]
		kind, name, ok := parseComponentReference(reference)
		key := kind + "/" + name
		if !ok || selected[key] != nil {
			continue
		}
		component := available[key]
		if component == nil {
			return nil, nil, fmt.Errorf("unresolved OpenAPI component reference %s.%s", kind, name)
		}
		selected[key] = component
		references = append(references, rawComponentReferences(component)...)
	}
	if len(selected) > 0 {
		overlayComponents := newMappingNode()
		for i := 0; i+1 < len(mappingValue(documentMap(root), "components").Content); i += 2 {
			kind := mappingValue(documentMap(root), "components").Content[i].Value
			sourceItems := mappingValue(documentMap(root), "components").Content[i+1]
			items := newMappingNode()
			for j := 0; j+1 < len(sourceItems.Content); j += 2 {
				name := sourceItems.Content[j].Value
				if component := selected[kind+"/"+name]; component != nil {
					setMappingValue(items, name, component)
				}
			}
			if len(items.Content) > 0 {
				setMappingValue(overlayComponents, kind, items)
			}
		}
		setMappingValue(documentMap(overlay), "components", overlayComponents)
	}
	return overlay, selected, nil
}

func rawDocumentOperations(root *yaml.Node) ([]rawOperationEntry, error) {
	result := make([]rawOperationEntry, 0)
	paths := mappingValue(documentMap(root), "paths")
	if paths == nil || paths.Kind != yaml.MappingNode {
		return result, nil
	}
	for i := 0; i+1 < len(paths.Content); i += 2 {
		path := paths.Content[i].Value
		item := paths.Content[i+1]
		if item.Kind != yaml.MappingNode {
			continue
		}
		var err error
		item, _, err = resolveRawPathItem(root, item, make(map[string]bool))
		if err != nil {
			return nil, fmt.Errorf("resolve OpenAPI path %s: %w", path, err)
		}
		for j := 0; j+1 < len(item.Content); j += 2 {
			method := item.Content[j].Value
			operation := item.Content[j+1]
			if method == "additionalOperations" && operation.Kind == yaml.MappingNode {
				for k := 0; k+1 < len(operation.Content); k += 2 {
					appendRawOperation(&result, path, operation.Content[k].Value, operation.Content[k+1])
				}
				continue
			}
			if _OPENAPI_OPERATION_KEYS[method] {
				appendRawOperation(&result, path, method, operation)
			}
		}
	}
	return result, nil
}

func appendRawOperation(result *[]rawOperationEntry, path, method string, operation *yaml.Node) {
	operationId := mappingValue(operation, "operationId")
	id := ""
	if operationId != nil {
		id = operationId.Value
	}
	*result = append(*result, rawOperationEntry{
		path:        path,
		method:      strings.ToUpper(method),
		operationId: id,
		operation:   operation,
	})
}

func buildRawOperationModel(root *yaml.Node, entry rawOperationEntry) (*v3.Operation, error) {
	mini := cloneYamlNode(root)
	paths := mappingValue(documentMap(mini), "paths")
	pathItem := mappingValue(paths, entry.path)
	if pathItem == nil {
		return nil, fmt.Errorf("raw OpenAPI path %s not found", entry.path)
	}
	pathItem, _, err := resolveRawPathItem(mini, pathItem, make(map[string]bool))
	if err != nil {
		return nil, err
	}
	for key := range _OPENAPI_OPERATION_KEYS {
		deleteMappingValue(pathItem, key)
	}
	setMappingValue(pathItem, "get", entry.operation)
	onlyPath := newMappingNode()
	setMappingValue(onlyPath, entry.path, pathItem)
	setMappingValue(documentMap(mini), "paths", onlyPath)
	data, err := yaml.Marshal(mini)
	if err != nil {
		return nil, err
	}
	data, err = sanitizedLibopenapiData(data)
	if err != nil {
		return nil, err
	}
	document, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, err
	}
	model, err := document.BuildV3Model()
	if err != nil {
		return nil, err
	}
	item, ok := model.Model.Paths.PathItems.Get(entry.path)
	if !ok || item.Get == nil {
		return nil, fmt.Errorf("build raw OpenAPI operation %q", entry.operationId)
	}
	return item.Get, nil
}

func resolveRawPathItem(
	root, item *yaml.Node, seen map[string]bool,
) (*yaml.Node, []string, error) {
	if item == nil {
		return nil, nil, nil
	}
	reference := mappingValue(item, "$ref")
	if reference == nil {
		return cloneYamlNode(item), nil, nil
	}
	kind, name, ok := parseComponentReference(reference.Value)
	if !ok || kind != "pathItems" {
		return cloneYamlNode(item), nil, nil
	}
	key := kind + "/" + name
	if seen[key] {
		return nil, nil, fmt.Errorf("circular OpenAPI path item reference %s", reference.Value)
	}
	seen[key] = true
	component := rawComponents(root)[key]
	if component == nil {
		return nil, nil, fmt.Errorf("unresolved OpenAPI component reference %s.%s", kind, name)
	}
	resolved, references, err := resolveRawPathItem(root, component, seen)
	if err != nil {
		return nil, nil, err
	}
	merged := mergeYamlNodes(resolved, item)
	deleteMappingValue(merged, "$ref")
	return merged, append([]string{reference.Value}, references...), nil
}

func findRawOperation(pathItem *yaml.Node, operationId string) (string, string, *yaml.Node) {
	for i := 0; i+1 < len(pathItem.Content); i += 2 {
		key := pathItem.Content[i].Value
		value := pathItem.Content[i+1]
		if key == "additionalOperations" && value.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(value.Content); j += 2 {
				candidate := value.Content[j+1]
				if id := mappingValue(candidate, "operationId"); id != nil && id.Value == operationId {
					return key, value.Content[j].Value, candidate
				}
			}
			continue
		}
		if !_OPENAPI_OPERATION_KEYS[key] || key == "additionalOperations" {
			continue
		}
		if id := mappingValue(value, "operationId"); id != nil && id.Value == operationId {
			return key, "", value
		}
	}
	return "", "", nil
}

func rawComponentReferences(node *yaml.Node) []string {
	result := make([]string, 0)
	var visit func(*yaml.Node, bool)
	visit = func(current *yaml.Node, referenceValues bool) {
		if current == nil {
			return
		}
		if current.Kind == yaml.ScalarNode && current.Tag == "!!str" &&
			referenceValues && strings.HasPrefix(current.Value, "#/components/") {
			result = append(result, current.Value)
		}
		if current.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(current.Content); i += 2 {
				key := current.Content[i].Value
				value := current.Content[i+1]
				if referenceValues {
					visit(value, true)
					continue
				}
				switch key {
				case "$ref", "$dynamicRef", "defaultMapping", "mapping":
					visit(value, true)
				default:
					visit(value, false)
				}
			}
			return
		}
		for _, child := range current.Content {
			visit(child, referenceValues)
		}
	}
	visit(node, false)
	return result
}
