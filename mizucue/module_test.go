package mizucue_test

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"cuelang.org/go/cue"
	"cuelang.org/go/encoding/openapi"

	"github.com/humbornjo/mizu/mizucue"
)

const _TEST_MODULE_CUE = "module: \"example.com/test\"\nlanguage: version: \"v0.16.1\"\n"

// loadTestModule loads the fixture CUE module under internal/testdata:
// package app holds constrained, cross-package-referencing definitions;
// package lib holds the shared #Named and #Status definitions.
func loadTestModule(t *testing.T) mizucue.Module {
	t.Helper()
	module, err := mizucue.LoadModule(os.DirFS("internal/testdata"))
	if err != nil {
		t.Fatal(err)
	}
	return module
}

func TestModule_Extract(t *testing.T) {
	module := loadTestModule(t)
	schema, err := module.Extract("app")
	if err != nil {
		t.Fatal(err)
	}
	address := TestAddress{Street: "1 Infinite Loop", City: "Cupertino", ZipCode: "95014"}
	if err := schema.Validate(address); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(TestAddress{}); err == nil {
		t.Fatal("Validate() succeeded for an empty address")
	}
	lib, err := module.Extract("lib")
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.Validate(TestNamed{Name: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Extract("missing"); err == nil ||
		!strings.Contains(err.Error(), "extract CUE package missing: package not found") {
		t.Fatalf("Extract() error = %v", err)
	}
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("MustExtract() did not panic")
		}
	}()
	module.MustExtract("missing")
}

func TestModule_OpenAPIs(t *testing.T) {
	documents, err := loadTestModule(t).OpenAPIs(nil)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	byPath := map[string]map[string]any{}
	for path, data := range documents {
		order = append(order, path)
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("package %s: invalid JSON: %v", path, err)
		}
		byPath[path] = document
	}
	if len(order) != 2 ||
		order[0] != "example.com/mizucue/test/app" ||
		order[1] != "example.com/mizucue/test/lib" {
		t.Fatalf("iteration order = %v", order)
	}
	app := byPath["example.com/mizucue/test/app"]
	schemas := app["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["example.com.mizucue.test.app.TestModel"]; !ok {
		t.Fatalf("app schemas = %#v", schemas)
	}
	raw, _ := json.Marshal(app)
	if !strings.Contains(string(raw), `"$ref":"#/components/schemas/example.com.mizucue.test.lib.TestNamed"`) {
		t.Fatalf("app cross-package reference = %s", raw)
	}
	lib := byPath["example.com/mizucue/test/lib"]
	libSchemas := lib["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := libSchemas["example.com.mizucue.test.lib.TestNamed"]; !ok {
		t.Fatalf("lib schemas = %#v", libSchemas)
	}
}

func TestModule_OpenAPIsNameFuncOverride(t *testing.T) {
	documents, err := loadTestModule(t).OpenAPIs(&openapi.Config{
		NameFunc: func(_ cue.Value, path cue.Path) string {
			return "custom." + path.String()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range documents {
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(document)
		if strings.Contains(string(raw), "example.com.mizucue.test") {
			t.Fatalf("default naming leaked through override: %s", raw)
		}
	}
}

// Two packages defining the same definition name must coexist in the
// consuming package's document — the CUE generator's bare naming
// silently overwrites one of them.
func TestModule_OpenAPIsSameNameCoexist(t *testing.T) {
	module, err := mizucue.LoadModule(fstest.MapFS{
		"cue.mod/module.cue": {Data: []byte(_TEST_MODULE_CUE)},
		"a/schema.cue":       {Data: []byte("package a\n#Config: {a: string}")},
		"b/schema.cue": {Data: []byte(`package b
import "example.com/test/a"
#Config: {b: string, ref: a.#Config}
`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range module.MustOpenAPIs(nil) {
		if path != "example.com/test/b" {
			continue
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
		if _, ok := schemas["example.com.test.a.Config"]; !ok {
			t.Fatalf("imported Config missing: %#v", schemas)
		}
		if _, ok := schemas["example.com.test.b.Config"]; !ok {
			t.Fatalf("own Config missing: %#v", schemas)
		}
		raw, _ := json.Marshal(document)
		if !strings.Contains(string(raw), `"$ref":"#/components/schemas/example.com.test.a.Config"`) {
			t.Fatalf("cross-package reference = %s", raw)
		}
	}
}

func TestModule_OpenAPIsConfigPassthrough(t *testing.T) {
	info := map[string]any{"title": "T", "version": "v1"}
	documents, err := loadTestModule(t).OpenAPIs(&openapi.Config{Info: info})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range documents {
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		got := document["info"].(map[string]any)
		if got["title"] != "T" || got["version"] != "v1" || len(got) != 2 {
			t.Fatalf("info = %#v", got)
		}
	}
	if len(info) != 2 {
		t.Fatalf("caller info mutated: %#v", info)
	}
}

func TestModule_Instances(t *testing.T) {
	module := loadTestModule(t)
	var got []string
	for instance := range module.Instances() {
		importPath, _, _ := strings.Cut(instance.ImportPath, "@")
		got = append(got, importPath+":"+instance.PkgName)
	}
	want := []string{
		"example.com/mizucue/test/app:app",
		"example.com/mizucue/test/lib:lib",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Instances() = %v", got)
	}
}

func TestModule_OpenAPIsPackageFailure(t *testing.T) {
	module, err := mizucue.LoadModule(fstest.MapFS{
		"cue.mod/module.cue": {Data: []byte(_TEST_MODULE_CUE)},
		"bad/schema.cue":     {Data: []byte(`package bad` + "\n" + `#Bad: string & != "x"`)},
		"good/schema.cue":    {Data: []byte("package good\n#Good: {name: string}")},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = module.OpenAPIs(nil)
	if err == nil || !strings.Contains(err.Error(), "generate OpenAPI for CUE package example.com/test/bad") {
		t.Fatalf("OpenAPIs() error = %v", err)
	}
	defer func() {
		if recovered := recover(); recovered == nil ||
			!strings.Contains(fmt.Sprint(recovered), "generate OpenAPI for CUE package example.com/test/bad") {
			t.Fatalf("MustOpenAPIs() panic = %v", recovered)
		}
	}()
	module.MustOpenAPIs(nil)
}
