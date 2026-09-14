package serde

import (
	"cmp"
	"iter"
	"reflect"
	"slices"
)

type Tuple[A, B any] struct {
	A A
	B B
}

// Fieldlet holds metadata about a struct field to be parsed from a
// request.
type Fieldlet struct {
	inner []Tuple[int, string]
}

func NewFieldlet(typ reflect.Type) Fieldlet {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	fl := Fieldlet{}
	for i := range typ.NumField() {
		field := typ.Field(i)
		name, _ := JsonField(field)
		if name == "" || name == "-" {
			continue
		}
		fl.inner = append(fl.inner, Tuple[int, string]{i, name})
	}
	slices.SortFunc(fl.inner, func(a, b Tuple[int, string]) int { return cmp.Compare(a.B, b.B) })
	return fl
}

func (fl Fieldlet) Fields() iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		for _, tuple := range fl.inner {
			if !yield(tuple.A, tuple.B) {
				return
			}
		}
	}
}

func (fl Fieldlet) Find(fieldName string) (fieldIndex int, name string, _ bool) {
	idx, ok := slices.BinarySearchFunc(
		fl.inner,
		fieldName,
		func(fb Tuple[int, string], name string) int { return cmp.Compare(fb.B, name) },
	)
	if ok {
		tuple := fl.inner[idx]
		return tuple.A, tuple.B, true
	}
	return -1, "", false
}
