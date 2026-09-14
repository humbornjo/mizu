package internal

import (
	"reflect"
	"testing"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

type canonicalTypeNameFixture struct{}

func TestInternal_CanonicalTypeName(t *testing.T) {
	cases := []struct {
		name string
		got  func() string
		want string
	}{
		{
			name: "named type",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[v3.Document]()) },
			want: "github.com.pb33f.libopenapi.datamodel.high.v3.Document",
		},
		{
			name: "pointer",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[*v3.Document]()) },
			want: "github.com.pb33f.libopenapi.datamodel.high.v3.Document",
		},
		{
			name: "double pointer",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[**v3.Document]()) },
			want: "github.com.pb33f.libopenapi.datamodel.high.v3.Document",
		},
		{
			name: "local unexported type",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[canonicalTypeNameFixture]()) },
			want: "github.com.humbornjo.mizu.mizuoai.internal.canonicalTypeNameFixture",
		},
		{
			name: "builtin",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[string]()) },
			want: "string",
		},
		{
			name: "slice",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[[]string]()) },
			want: "_string",
		},
		{
			name: "map",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[map[string]int]()) },
			want: "map_string_int",
		},
		{
			name: "anonymous struct",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[struct{ X int }]()) },
			want: "struct_X_int",
		},
		{
			name: "generic instantiation",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[orderedmap.Map[string, any]]()) },
			want: "github.com.pb33f.libopenapi.orderedmap.Map_string_interface",
		},
		{
			// reflect reports the defining package: go-yaml v4
			// defines Node in internal/libyaml and aliases it in
			// the root package.
			name: "aliased type",
			got:  func() string { return CanonicalTypeName(reflect.TypeFor[yaml.Node]()) },
			want: "go.yaml.in.yaml.v4.internal.libyaml.Node",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.got(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
