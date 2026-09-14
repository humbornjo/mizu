package mizucue_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/humbornjo/mizu/mizucue"
)

// The Test* model types mirror the definitions of the fixture module
// under internal/testdata: TestModel, TestAddress, ExtraModel,
// IncompleteModel, and WrongTypeModel live in package app, TestNamed
// mirrors lib.#Named.
type TestNamed struct {
	Name string `json:"name"`
}

type TestAddress struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	ZipCode string `json:"zipCode"`
}

type TestModel struct {
	Name     string            `json:"name"`
	Age      int               `json:"age"`
	Email    string            `json:"email"`
	Tags     []string          `json:"tags"`
	Status   string            `json:"status"`
	Metadata map[string]string `json:"metadata"`
	Owner    TestNamed         `json:"owner"`
	Address  *TestAddress      `json:"address,omitempty"`
}

type NamedModel struct {
	Name string `json:"name"`
}

type ExtraModel struct {
	Name  string `json:"name"`
	Extra string `json:"extra"`
}

type WrongTypeModel struct {
	Count string `json:"count"`
}

type MissingModel struct{}

type IncompleteModel struct{}

// TestPublishOperation and IncompleteOperation feed
// TestSchema_Operation: the fragment is looked up as the CUE
// definition named after the concrete type.
type TestPublishOperation struct{}

type IncompleteOperation struct{}

func validTestModel() TestModel {
	return TestModel{
		Name:     "mizu",
		Age:      3,
		Email:    "dev@example.com",
		Tags:     []string{"cue"},
		Status:   "active",
		Metadata: map[string]string{"tier": "test"},
		Owner:    TestNamed{Name: "mizu"},
		Address:  &TestAddress{Street: "1 Infinite Loop", City: "Cupertino", ZipCode: "95014"},
	}
}

func TestMizucue_LoadSchemaAndValidate(t *testing.T) {
	schema, err := mizucue.LoadSchema(`
package test

#NamedModel: name: string & != ""
#IncompleteModel: value: string
`)
	if err != nil {
		t.Fatal(err)
	}
	model := NamedModel{Name: "valid"}
	if err := schema.Validate(model); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(&model); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(NamedModel{}); err == nil {
		t.Fatal("Validate() succeeded for an empty name")
	}
	var nilModel *NamedModel
	if err := schema.Validate(nilModel); err == nil {
		t.Fatal("Validate() succeeded for a typed nil pointer")
	}
	if err := schema.Validate(nil); err == nil {
		t.Fatal("Validate() succeeded for nil")
	}
	if err := schema.Validate(MissingModel{}); err == nil ||
		!strings.Contains(err.Error(), "lookup model schema MissingModel") {
		t.Fatalf("Validate() missing definition error = %v", err)
	}
	if err := schema.Validate(IncompleteModel{}); err == nil ||
		!strings.Contains(err.Error(), "validate IncompleteModel") {
		t.Fatalf("Validate() non-concrete error = %v", err)
	}

	if _, err := mizucue.LoadSchema("package test\n#Broken: {"); err == nil ||
		!strings.Contains(err.Error(), "compile CUE schema") {
		t.Fatalf("LoadSchema() error = %v", err)
	}
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), "compile CUE schema") {
			t.Fatalf("MustLoadSchema() panic = %v", recovered)
		}
	}()
	mizucue.MustLoadSchema("package test\n#Broken: {")
}

func TestSchema_Validate(t *testing.T) {
	schema, err := loadTestModule(t).Extract("app")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(validTestModel()); err != nil {
		t.Fatal(err)
	}
	withoutAddress := validTestModel()
	withoutAddress.Address = nil
	if err := schema.Validate(withoutAddress); err != nil {
		t.Fatalf("Validate() failed without the optional address: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(model *TestModel)
	}{
		{"negative age", func(model *TestModel) { model.Age = -1 }},
		{"age over bound", func(model *TestModel) { model.Age = 151 }},
		{"malformed email", func(model *TestModel) { model.Email = "not-an-email" }},
		{"empty email", func(model *TestModel) { model.Email = "" }},
		{"unknown status", func(model *TestModel) { model.Status = "retired" }},
		{"empty status", func(model *TestModel) { model.Status = "" }},
		{"malformed zip code", func(model *TestModel) { model.Address.ZipCode = "1234" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := validTestModel()
			test.mutate(&model)
			if err := schema.Validate(model); err == nil {
				t.Fatal("Validate() succeeded")
			}
		})
	}
	if err := schema.Validate(ExtraModel{Name: "mizu", Extra: "surprise"}); err == nil ||
		!strings.Contains(err.Error(), "validate ExtraModel") {
		t.Fatalf("Validate() extra field error = %v", err)
	}
	if err := schema.Validate(WrongTypeModel{Count: "1"}); err == nil {
		t.Fatal("Validate() succeeded for a mismatched field type")
	}
	if err := schema.Validate(MissingModel{}); err == nil ||
		!strings.Contains(err.Error(), "lookup model schema MissingModel") {
		t.Fatalf("Validate() missing definition error = %v", err)
	}
	if err := schema.Validate(IncompleteModel{}); err == nil ||
		!strings.Contains(err.Error(), "validate IncompleteModel") {
		t.Fatalf("Validate() non-concrete error = %v", err)
	}
}

func TestMizucue_SchemaOpenAPI(t *testing.T) {
	schema, err := loadTestModule(t).Extract("app")
	if err != nil {
		t.Fatal(err)
	}
	data, err := schema.OpenAPI(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"TestModel"`) {
		t.Fatalf("OpenAPI() document = %s", data)
	}
	if mustData := schema.MustOpenAPI(nil); !bytes.Equal(mustData, data) {
		t.Fatal("MustOpenAPI() differs from OpenAPI()")
	}

	if _, err := mizucue.MustLoadSchema("package test\n#TestModel: {name: string}").OpenAPI(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mizucue.MustLoadSchema("package test\n#Bad: string & != \"x\"").OpenAPI(nil); err == nil {
		t.Fatal("OpenAPI() succeeded for an unsupported constraint")
	}
}

func TestSchema_Operation(t *testing.T) {
	schema := mizucue.MustLoadSchema(`
package test

#TestPublishOperation: {
	operationId: "publishThing"
	requestBody: content: "multipart/form-data": schema: type: "object"
} @go(-)

#IncompleteOperation: operationId: string
`)
	data, err := schema.Operation(reflect.TypeFor[TestPublishOperation]())
	if err != nil {
		t.Fatal(err)
	}
	var fragment struct {
		OperationID string `json:"operationId"`
		RequestBody struct {
			Content map[string]struct {
				Schema struct {
					Type string `json:"type"`
				} `json:"schema"`
			} `json:"content"`
		} `json:"requestBody"`
	}
	if err := json.Unmarshal(data, &fragment); err != nil {
		t.Fatal(err)
	}
	if fragment.OperationID != "publishThing" {
		t.Fatalf("Operation() operationId = %q", fragment.OperationID)
	}
	if fragment.RequestBody.Content["multipart/form-data"].Schema.Type != "object" {
		t.Fatalf("Operation() fragment = %s", data)
	}
	if mustData := schema.MustOperation(reflect.TypeFor[TestPublishOperation]()); !bytes.Equal(mustData, data) {
		t.Fatal("MustOperation() differs from Operation()")
	}
	if _, err := schema.Operation(reflect.TypeFor[MissingModel]()); err == nil ||
		!strings.Contains(err.Error(), "lookup operation fragment MissingModel") {
		t.Fatalf("Operation() missing definition error = %v", err)
	}
	if _, err := schema.Operation(reflect.TypeFor[IncompleteOperation]()); err == nil ||
		!strings.Contains(err.Error(), "validate operation fragment IncompleteOperation") {
		t.Fatalf("Operation() non-concrete error = %v", err)
	}
	if _, err := schema.Operation(nil); err == nil {
		t.Fatal("Operation() succeeded for a nil type")
	}
	defer func() {
		if recovered := recover(); recovered == nil ||
			!strings.Contains(fmt.Sprint(recovered), "lookup operation fragment MissingModel") {
			t.Fatalf("MustOperation() panic = %v", recovered)
		}
	}()
	schema.MustOperation(reflect.TypeFor[MissingModel]())
}
