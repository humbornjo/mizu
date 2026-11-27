package fieldmaskx

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
	_JSON_MARSHALER_TYPE = reflect.TypeFor[json.Marshaler]()
	_TEXT_MARSHALER_TYPE = reflect.TypeFor[encoding.TextMarshaler]()
)

// FieldMask is an immutable set of JSON field paths bound to T. Construct
// one with Intersect.
type FieldMask[T any] struct {
	typ   reflect.Type
	paths []string
	root  *node
}

// node is one level of the path trie: selected marks a path that ends
// here, children holds the paths that continue below.
type node struct {
	selected bool
	children map[string]*node
}

// field is a JSON-visible struct field: name is its JSON object key, index
// is its (possibly promoted) field path, and tagged records whether the
// name came from an explicit json tag, which wins ties.
type field struct {
	name   string
	tagged bool
	index  []int
	typ    reflect.Type
}

// Intersect returns a field mask containing the structurally valid overlap
// between allowed and requested. Malformed, unknown, and disallowed paths
// are omitted.
func Intersect[T any](allowed, requested []string) *FieldMask[T] {
	typ := reflect.TypeFor[T]()
	mask := &FieldMask[T]{typ: typ, root: &node{children: make(map[string]*node)}}
	if !isStruct(typ) {
		return mask
	}

	allowedPaths := validPaths(typ, allowed)
	requestedPaths := validPaths(typ, requested)
	var paths [][]string
	for _, allow := range allowedPaths {
		for _, want := range requestedPaths {
			switch {
			case hasPathPrefix(allow, want):
				paths = append(paths, allow)
			case hasPathPrefix(want, allow):
				paths = append(paths, want)
			}
		}
	}
	paths = normalizePaths(paths)
	mask.paths = make([]string, len(paths))
	for i, segments := range paths {
		mask.paths[i] = strings.Join(segments, ".")
		mask.root.add(segments)
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

// Filter keeps fields selected by the mask and clears all other
// JSON-visible fields. An empty mask clears every JSON-visible field.
func (m *FieldMask[T]) Filter(value *T) error {
	target, err := m.target(value, "filter")
	if err != nil {
		return err
	}
	filterValue(target, m.root)
	return nil
}

// Prune clears fields selected by the mask and leaves all other fields
// untouched. An empty mask is a no-op.
func (m *FieldMask[T]) Prune(value *T) error {
	target, err := m.target(value, "prune")
	if err != nil {
		return err
	}
	pruneValue(target, m.root)
	return nil
}

// Overwrite copies fields selected by the mask from src to dest and leaves
// all other destination fields untouched. Whole pointer, map, and slice
// fields use normal Go assignment semantics and may alias src. An empty
// mask is a no-op.
func (m *FieldMask[T]) Overwrite(src, dest *T) error {
	source, err := m.target(src, "overwrite source")
	if err != nil {
		return err
	}
	target, err := m.target(dest, "overwrite destination")
	if err != nil {
		return err
	}
	overwriteValue(source, target, m.root)
	return nil
}

func (m *FieldMask[T]) target(value *T, operation string) (reflect.Value, error) {
	if m == nil {
		return reflect.Value{}, fmt.Errorf("%s: field mask is nil", operation)
	}
	if !isStruct(m.typ) {
		return reflect.Value{}, fmt.Errorf("%s: field mask type must be a JSON struct, got %v", operation, m.typ)
	}
	if value == nil {
		return reflect.Value{}, fmt.Errorf("%s: value is nil", operation)
	}
	return reflect.ValueOf(value).Elem(), nil
}

// add inserts a normalized path below n; no inserted path prefixes
// another, so a selected node is never crossed on the way down.
func (n *node) add(parts []string) {
	for _, part := range parts {
		child := n.children[part]
		if child == nil {
			child = &node{children: make(map[string]*node)}
			n.children[part] = child
		}
		n = child
	}
	n.selected = true
	n.children = nil
}

func validPaths(typ reflect.Type, paths []string) [][]string {
	valid := make([][]string, 0, len(paths))
	for _, path := range paths {
		if segments, ok := resolvePath(typ, path); ok {
			valid = append(valid, segments)
		}
	}
	return normalizePaths(valid)
}

// resolvePath walks typ through existing JSON fields and returns the
// resolved path segments. Struct fields and string map keys consume one
// segment; slices and arrays consume none, so a mask applies to every
// element. Types with custom marshaling are leaves and reject any path
// that tries to descend into them.
func resolvePath(typ reflect.Type, path string) ([]string, bool) {
	if path == "" {
		return nil, false
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if isTerminal(typ) {
		return nil, false
	}
	switch typ.Kind() {
	case reflect.Struct:
		fields := typeFields(typ)
		for _, f := range fields {
			if f.name == path {
				return []string{f.name}, true
			}
		}
		for _, f := range fields {
			if rest, ok := strings.CutPrefix(path, f.name+"."); ok {
				if tail, ok := resolvePath(f.typ, rest); ok {
					return append([]string{f.name}, tail...), true
				}
			}
		}
		return nil, false
	case reflect.Array, reflect.Slice:
		return resolvePath(typ.Elem(), path)
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return nil, false
		}
		key, rest, hasDot := strings.Cut(path, ".")
		if key == "" {
			return nil, false
		}
		if !hasDot {
			return []string{key}, true
		}
		tail, ok := resolvePath(typ.Elem(), rest)
		if !ok {
			return nil, false
		}
		return append([]string{key}, tail...), true
	default:
		return nil, false
	}
}

// normalizePaths sorts paths and drops duplicates and any path that
// extends an earlier one, leaving a set with no prefix pairs.
func normalizePaths(paths [][]string) [][]string {
	paths = slices.Clone(paths)
	slices.SortFunc(paths, slices.Compare)
	kept := paths[:0]
	for _, path := range paths {
		if len(kept) > 0 && hasPathPrefix(path, kept[len(kept)-1]) {
			continue
		}
		kept = append(kept, path)
	}
	return kept
}

func hasPathPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

func isStruct(typ reflect.Type) bool {
	return typ != nil && typ.Kind() == reflect.Struct && !isTerminal(typ)
}

// isTerminal reports whether typ marshals as a leaf value because it or
// its pointer implements a JSON or text marshaler interface.
func isTerminal(typ reflect.Type) bool {
	if typ == nil {
		return true
	}
	types := []reflect.Type{typ}
	if typ.Kind() != reflect.Pointer {
		types = append(types, reflect.PointerTo(typ))
	}
	for _, candidate := range types {
		if candidate.Implements(_JSON_MARSHALER_TYPE) ||
			candidate.Implements(_TEXT_MARSHALER_TYPE) {
			return true
		}
	}
	return false
}

// typeFields returns the JSON-visible fields of typ, resolved the way
// encoding/json resolves them: anonymous struct fields are promoted, and
// among candidates sharing a name the shallowest wins, an explicitly
// tagged one wins a depth tie, and an even tie annihilates the name
// entirely.
func typeFields(typ reflect.Type) []field {
	var fields []field
	ancestors := make(map[reflect.Type]bool)
	var walk func(typ reflect.Type, index []int)
	walk = func(typ reflect.Type, index []int) {
		for i := range typ.NumField() {
			f := typ.Field(i)
			ft := f.Type
			if ft.Name() == "" && ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if !f.IsExported() && (!f.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if !validTag(name) {
				name = ""
			}
			index := append(slices.Clone(index), i)

			if name == "" && f.Anonymous && ft.Kind() == reflect.Struct {
				if !ancestors[ft] {
					ancestors[ft] = true
					walk(ft, index)
					delete(ancestors, ft)
				}
				continue
			}
			if !f.IsExported() {
				continue
			}

			tagged := name != ""
			if name == "" {
				name = f.Name
			}
			fields = append(fields, field{name: name, tagged: tagged, index: index, typ: f.Type})
		}
	}
	walk(typ, nil)

	slices.SortFunc(fields, func(left, right field) int {
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
		j := i + 1
		for j < len(fields) && fields[j].name == fields[i].name {
			j++
		}
		if j == i+1 ||
			len(fields[i].index) != len(fields[i+1].index) ||
			fields[i].tagged != fields[i+1].tagged {
			visible = append(visible, fields[i])
		}
		i = j
	}
	slices.SortFunc(visible, func(left, right field) int {
		return slices.Compare(left.index, right.index)
	})
	return visible
}

// validTag reports whether name is usable as a JSON object key, the same
// rule encoding/json applies to tag names.
func validTag(name string) bool {
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

// valueByIndex walks a promoted field path, dereferencing pointers along
// the way. With allocate set, nil pointers on the path are initialized;
// otherwise a nil pointer abandons the walk.
func valueByIndex(value reflect.Value, index []int, allocate bool) (reflect.Value, bool) {
	for _, i := range index {
		for value.Kind() == reflect.Pointer {
			if value.IsNil() {
				if !allocate || !value.CanSet() {
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

func filterValue(value reflect.Value, n *node) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Struct:
		for _, f := range typeFields(value.Type()) {
			child := n.children[f.name]
			fv, ok := valueByIndex(value, f.index, false)
			if !ok {
				continue
			}
			switch {
			case child == nil:
				fv.Set(reflect.Zero(fv.Type()))
			case child.selected:
			default:
				filterValue(fv, child)
			}
		}
	case reflect.Array, reflect.Slice:
		for i := range value.Len() {
			filterValue(value.Index(i), n)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			child := n.children[key.String()]
			if child == nil {
				value.SetMapIndex(key, reflect.Value{})
				continue
			}
			if child.selected {
				continue
			}
			item := reflect.New(value.Type().Elem()).Elem()
			item.Set(value.MapIndex(key))
			filterValue(item, child)
			value.SetMapIndex(key, item)
		}
	}
}

func pruneValue(value reflect.Value, n *node) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Struct:
		for _, f := range typeFields(value.Type()) {
			child := n.children[f.name]
			if child == nil {
				continue
			}
			fv, ok := valueByIndex(value, f.index, false)
			if !ok {
				continue
			}
			if child.selected {
				fv.Set(reflect.Zero(fv.Type()))
				continue
			}
			pruneValue(fv, child)
		}
	case reflect.Array, reflect.Slice:
		for i := range value.Len() {
			pruneValue(value.Index(i), n)
		}
	case reflect.Map:
		for name, child := range n.children {
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
			pruned := reflect.New(item.Type()).Elem()
			pruned.Set(item)
			pruneValue(pruned, child)
			value.SetMapIndex(key, pruned)
		}
	}
}

func overwriteValue(source, target reflect.Value, n *node) {
	if source.Kind() == reflect.Pointer {
		if source.IsNil() {
			if target.IsNil() {
				return
			}
			overwriteValue(reflect.Zero(source.Type().Elem()), target.Elem(), n)
			return
		}
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		overwriteValue(source.Elem(), target.Elem(), n)
		return
	}

	switch source.Kind() {
	case reflect.Struct:
		for _, f := range typeFields(source.Type()) {
			child := n.children[f.name]
			if child == nil {
				continue
			}
			sv, sok := valueByIndex(source, f.index, false)
			tv, tok := valueByIndex(target, f.index, sok)
			if child.selected {
				if !tok {
					continue
				}
				if sok {
					tv.Set(sv)
				} else {
					tv.Set(reflect.Zero(tv.Type()))
				}
				continue
			}
			if !sok {
				if tok {
					overwriteValue(reflect.Zero(tv.Type()), tv, child)
				}
				continue
			}
			overwriteValue(sv, tv, child)
		}
	case reflect.Array:
		for i := range source.Len() {
			overwriteValue(source.Index(i), target.Index(i), n)
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
			overwriteValue(source.Index(i), target.Index(i), n)
		}
	case reflect.Map:
		for name, child := range n.children {
			key := reflect.New(source.Type().Key()).Elem()
			key.SetString(name)
			sval := source.MapIndex(key)
			tval := target.MapIndex(key)
			if child.selected {
				if sval.IsValid() {
					if target.IsNil() {
						target.Set(reflect.MakeMap(target.Type()))
					}
					target.SetMapIndex(key, sval)
				} else if tval.IsValid() {
					target.SetMapIndex(key, reflect.Value{})
				}
				continue
			}
			if !sval.IsValid() {
				if !tval.IsValid() {
					continue
				}
				sval = reflect.Zero(source.Type().Elem())
			}
			merged := reflect.New(target.Type().Elem()).Elem()
			if tval.IsValid() {
				merged.Set(tval)
			}
			overwriteValue(sval, merged, child)
			if target.IsNil() {
				target.Set(reflect.MakeMap(target.Type()))
			}
			target.SetMapIndex(key, merged)
		}
	}
}
