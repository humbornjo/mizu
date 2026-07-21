package mizu

import (
	"cmp"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

var (
	_JSON_MARSHALER_TYPE   = reflect.TypeFor[json.Marshaler]()
	_JSON_UNMARSHALER_TYPE = reflect.TypeFor[json.Unmarshaler]()
	_TEXT_MARSHALER_TYPE   = reflect.TypeFor[encoding.TextMarshaler]()
	_TEXT_UNMARSHALER_TYPE = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// FieldMask is an immutable set of JSON field paths bound to T.
// Construct one with Intersect.
type FieldMask[T any] struct {
	typ   reflect.Type
	paths []string
	root  *fieldMaskNode
}

type fieldMaskNode struct {
	selected bool
	children map[string]*fieldMaskNode
}

type fieldMaskField struct {
	name   string
	tagged bool
	index  []int
	typ    reflect.Type
}

// Intersect returns a field mask containing the structurally valid overlap
// between allowed and requested. Malformed, unknown, and disallowed paths are
// omitted.
func Intersect[T any](allowed, requested []string) *FieldMask[T] {
	typ := reflect.TypeFor[T]()
	mask := &FieldMask[T]{typ: typ, root: newFieldMaskNode()}
	if !isFieldMaskStruct(typ) {
		return mask
	}

	allowed = validFieldMaskPaths(typ, allowed)
	requested = validFieldMaskPaths(typ, requested)
	paths := make([]string, 0)
	for _, left := range allowed {
		for _, right := range requested {
			switch {
			case hasFieldMaskPrefix(left, right):
				paths = append(paths, left)
			case hasFieldMaskPrefix(right, left):
				paths = append(paths, right)
			}
		}
	}
	mask.paths = normalizeFieldMaskPaths(paths)
	for _, path := range mask.paths {
		mask.root.add(strings.Split(path, "."))
	}
	return mask
}

// Paths returns a copy of the canonical paths in the field mask.
func (m *FieldMask[T]) Paths() []string {
	if m == nil {
		return nil
	}
	return slices.Clone(m.paths)
}

// Filter keeps fields selected by the mask and clears all other JSON-visible
// fields. An empty mask clears every JSON-visible field.
func (m *FieldMask[T]) Filter(value *T) error {
	target, err := m.target(value, "filter")
	if err != nil {
		return err
	}
	filterFieldMaskValue(target, m.root)
	return nil
}

// Prune clears fields selected by the mask and leaves all other fields
// untouched. An empty mask is a no-op.
func (m *FieldMask[T]) Prune(value *T) error {
	target, err := m.target(value, "prune")
	if err != nil {
		return err
	}
	pruneFieldMaskValue(target, m.root)
	return nil
}

// Overwrite copies fields selected by the mask from src to dest and leaves all
// other destination fields untouched. Whole pointer, map, and slice fields use
// normal Go assignment semantics and may alias src. An empty mask is a no-op.
func (m *FieldMask[T]) Overwrite(src, dest *T) error {
	source, err := m.target(src, "overwrite source")
	if err != nil {
		return err
	}
	target, err := m.target(dest, "overwrite destination")
	if err != nil {
		return err
	}
	overwriteFieldMaskValue(source, target, m.root)
	return nil
}

func (m *FieldMask[T]) target(value *T, operation string) (reflect.Value, error) {
	if m == nil {
		return reflect.Value{}, fmt.Errorf("%s: field mask is nil", operation)
	}
	if !isFieldMaskStruct(m.typ) {
		return reflect.Value{}, fmt.Errorf("%s: field mask type must be a JSON struct, got %v", operation, m.typ)
	}
	if value == nil {
		return reflect.Value{}, fmt.Errorf("%s: value is nil", operation)
	}
	return reflect.ValueOf(value).Elem(), nil
}

func newFieldMaskNode() *fieldMaskNode {
	return &fieldMaskNode{children: make(map[string]*fieldMaskNode)}
}

func (n *fieldMaskNode) add(parts []string) {
	current := n
	for _, part := range parts {
		if current.selected {
			return
		}
		child := current.children[part]
		if child == nil {
			child = newFieldMaskNode()
			current.children[part] = child
		}
		current = child
	}
	current.selected = true
	current.children = nil
}

func validFieldMaskPaths(typ reflect.Type, paths []string) []string {
	valid := make([]string, 0, len(paths))
	for _, path := range paths {
		if validFieldMaskPath(typ, path) {
			valid = append(valid, path)
		}
	}
	return normalizeFieldMaskPaths(valid)
}

func validFieldMaskPath(typ reflect.Type, path string) bool {
	if path == "" {
		return false
	}
	parts := strings.Split(path, ".")
	if slices.Contains(parts, "") {
		return false
	}

	for index := 0; index < len(parts); {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if isFieldMaskTerminal(typ) {
			return false
		}

		switch typ.Kind() {
		case reflect.Struct:
			field, ok := fieldMaskFieldsByName(typ)[parts[index]]
			if !ok {
				return false
			}
			typ = field.typ
			index++
		case reflect.Array, reflect.Slice:
			typ = typ.Elem()
		case reflect.Map:
			if typ.Key().Kind() != reflect.String {
				return false
			}
			typ = typ.Elem()
			index++
		default:
			return false
		}
	}
	return true
}

func normalizeFieldMaskPaths(paths []string) []string {
	paths = slices.Clone(paths)
	slices.Sort(paths)
	result := paths[:0]
	for _, path := range paths {
		if len(result) > 0 && hasFieldMaskPrefix(path, result[len(result)-1]) {
			continue
		}
		result = append(result, path)
	}
	return result
}

func hasFieldMaskPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix) &&
		(len(path) == len(prefix) || path[len(prefix)] == '.')
}

func isFieldMaskStruct(typ reflect.Type) bool {
	return typ != nil && typ.Kind() == reflect.Struct && !isFieldMaskTerminal(typ)
}

func isFieldMaskTerminal(typ reflect.Type) bool {
	if typ == nil {
		return true
	}
	types := []reflect.Type{typ}
	if typ.Kind() != reflect.Pointer {
		types = append(types, reflect.PointerTo(typ))
	}
	for _, candidate := range types {
		if candidate.Implements(_JSON_MARSHALER_TYPE) ||
			candidate.Implements(_JSON_UNMARSHALER_TYPE) ||
			candidate.Implements(_TEXT_MARSHALER_TYPE) ||
			candidate.Implements(_TEXT_UNMARSHALER_TYPE) {
			return true
		}
	}
	return false
}

func fieldMaskFieldsByName(typ reflect.Type) map[string]fieldMaskField {
	fields := fieldMaskFields(typ)
	result := make(map[string]fieldMaskField, len(fields))
	for _, field := range fields {
		result[field.name] = field
	}
	return result
}

func fieldMaskFields(typ reflect.Type) []fieldMaskField {
	current := []fieldMaskField{}
	next := []fieldMaskField{{typ: typ}}
	var count, nextCount map[reflect.Type]int
	visited := make(map[reflect.Type]bool)
	fields := make([]fieldMaskField, 0)

	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, make(map[reflect.Type]int)
		for _, parent := range current {
			if visited[parent.typ] {
				continue
			}
			visited[parent.typ] = true
			for i := range parent.typ.NumField() {
				field := parent.typ.Field(i)
				if !field.IsExported() {
					continue
				}
				tag := field.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, _, _ := strings.Cut(tag, ",")
				if !validFieldMaskTag(name) {
					name = ""
				}
				index := slices.Clone(parent.index)
				index = append(index, i)

				fieldType := field.Type
				if fieldType.Name() == "" && fieldType.Kind() == reflect.Pointer {
					fieldType = fieldType.Elem()
				}
				if name != "" || !field.Anonymous || fieldType.Kind() != reflect.Struct {
					tagged := name != ""
					if name == "" {
						name = field.Name
					}
					candidate := fieldMaskField{
						name: name, tagged: tagged, index: index,
						typ: fieldMaskTypeByIndex(typ, index),
					}
					fields = append(fields, candidate)
					if count[parent.typ] > 1 {
						fields = append(fields, candidate)
					}
					continue
				}

				nextCount[fieldType]++
				if nextCount[fieldType] == 1 {
					next = append(next, fieldMaskField{index: index, typ: fieldType})
				}
			}
		}
	}

	slices.SortFunc(fields, func(left, right fieldMaskField) int {
		if order := strings.Compare(left.name, right.name); order != 0 {
			return order
		}
		if order := cmp.Compare(len(left.index), len(right.index)); order != 0 {
			return order
		}
		if left.tagged != right.tagged {
			if left.tagged {
				return -1
			}
			return 1
		}
		return slices.Compare(left.index, right.index)
	})

	visible := fields[:0]
	for i := 0; i < len(fields); {
		end := i + 1
		for end < len(fields) && fields[end].name == fields[i].name {
			end++
		}
		group := fields[i:end]
		if len(group) == 1 || len(group[0].index) != len(group[1].index) || group[0].tagged != group[1].tagged {
			visible = append(visible, group[0])
		}
		i = end
	}
	slices.SortFunc(visible, func(left, right fieldMaskField) int {
		return slices.Compare(left.index, right.index)
	})
	return visible
}

func validFieldMaskTag(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", char):
		case !unicode.IsLetter(char) && !unicode.IsDigit(char):
			return false
		}
	}
	return true
}

func fieldMaskTypeByIndex(typ reflect.Type, index []int) reflect.Type {
	for _, i := range index {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		typ = typ.Field(i).Type
	}
	return typ
}

func fieldMaskValueByIndex(value reflect.Value, index []int, allocate bool) (reflect.Value, bool) {
	for _, i := range index {
		for value.Kind() == reflect.Pointer {
			if value.IsNil() {
				if !allocate {
					return reflect.Value{}, false
				}
				value.Set(reflect.New(value.Type().Elem()))
			}
			value = value.Elem()
		}
		value = value.Field(i)
	}
	return value, true
}

func filterFieldMaskValue(value reflect.Value, node *fieldMaskNode) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Struct:
		for _, field := range fieldMaskFields(value.Type()) {
			child := node.children[field.name]
			fieldValue, ok := fieldMaskValueByIndex(value, field.index, false)
			if !ok {
				continue
			}
			switch {
			case child == nil:
				fieldValue.Set(reflect.Zero(fieldValue.Type()))
			case child.selected:
			default:
				filterFieldMaskValue(fieldValue, child)
			}
		}
	case reflect.Array, reflect.Slice:
		for i := range value.Len() {
			filterFieldMaskValue(value.Index(i), node)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			child := node.children[key.String()]
			if child == nil {
				value.SetMapIndex(key, reflect.Value{})
				continue
			}
			if child.selected {
				continue
			}
			item := reflect.New(value.Type().Elem()).Elem()
			item.Set(value.MapIndex(key))
			filterFieldMaskValue(item, child)
			value.SetMapIndex(key, item)
		}
	}
}

func pruneFieldMaskValue(value reflect.Value, node *fieldMaskNode) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Struct:
		for _, field := range fieldMaskFields(value.Type()) {
			child := node.children[field.name]
			if child == nil {
				continue
			}
			fieldValue, ok := fieldMaskValueByIndex(value, field.index, false)
			if !ok {
				continue
			}
			if child.selected {
				fieldValue.Set(reflect.Zero(fieldValue.Type()))
				continue
			}
			pruneFieldMaskValue(fieldValue, child)
		}
	case reflect.Array, reflect.Slice:
		for i := range value.Len() {
			pruneFieldMaskValue(value.Index(i), node)
		}
	case reflect.Map:
		for name, child := range node.children {
			key := reflect.New(value.Type().Key()).Elem()
			key.SetString(name)
			item := value.MapIndex(key)
			if !item.IsValid() {
				continue
			}
			if child.selected {
				value.SetMapIndex(key, reflect.Value{})
				continue
			}
			copy := reflect.New(item.Type()).Elem()
			copy.Set(item)
			pruneFieldMaskValue(copy, child)
			value.SetMapIndex(key, copy)
		}
	}
}

func overwriteFieldMaskValue(source, target reflect.Value, node *fieldMaskNode) {
	if source.Kind() == reflect.Pointer {
		if source.IsNil() {
			if target.IsNil() {
				return
			}
			overwriteFieldMaskValue(reflect.Zero(source.Type().Elem()), target.Elem(), node)
			return
		}
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		overwriteFieldMaskValue(source.Elem(), target.Elem(), node)
		return
	}

	switch source.Kind() {
	case reflect.Struct:
		for _, field := range fieldMaskFields(source.Type()) {
			child := node.children[field.name]
			if child == nil {
				continue
			}
			sourceField, sourceExists := fieldMaskValueByIndex(source, field.index, false)
			targetField, targetExists := fieldMaskValueByIndex(target, field.index, sourceExists)
			if child.selected {
				if !targetExists {
					continue
				}
				if sourceExists {
					targetField.Set(sourceField)
				} else {
					targetField.Set(reflect.Zero(targetField.Type()))
				}
				continue
			}
			if !sourceExists {
				if targetExists {
					overwriteFieldMaskValue(reflect.Zero(targetField.Type()), targetField, child)
				}
				continue
			}
			overwriteFieldMaskValue(sourceField, targetField, child)
		}
	case reflect.Array:
		for i := range source.Len() {
			overwriteFieldMaskValue(source.Index(i), target.Index(i), node)
		}
	case reflect.Slice:
		if source.IsNil() {
			target.Set(reflect.Zero(target.Type()))
			return
		}
		length := source.Len()
		if target.Cap() < length {
			resized := reflect.MakeSlice(target.Type(), length, length)
			reflect.Copy(resized, target)
			target.Set(resized)
		} else {
			target.SetLen(length)
		}
		for i := range length {
			overwriteFieldMaskValue(source.Index(i), target.Index(i), node)
		}
	case reflect.Map:
		for name, child := range node.children {
			key := reflect.New(source.Type().Key()).Elem()
			key.SetString(name)
			sourceItem := source.MapIndex(key)
			targetItem := target.MapIndex(key)
			if child.selected {
				if sourceItem.IsValid() {
					if target.IsNil() {
						target.Set(reflect.MakeMap(target.Type()))
					}
					target.SetMapIndex(key, sourceItem)
				} else if targetItem.IsValid() {
					target.SetMapIndex(key, reflect.Value{})
				}
				continue
			}
			if !sourceItem.IsValid() && !targetItem.IsValid() {
				continue
			}
			if !sourceItem.IsValid() {
				sourceItem = reflect.Zero(source.Type().Elem())
			}
			copy := reflect.New(target.Type().Elem()).Elem()
			if targetItem.IsValid() {
				copy.Set(targetItem)
			}
			overwriteFieldMaskValue(sourceItem, copy, child)
			if target.IsNil() {
				target.Set(reflect.MakeMap(target.Type()))
			}
			target.SetMapIndex(key, copy)
		}
	}
}
