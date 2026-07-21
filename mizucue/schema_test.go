package mizucue

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

type TestModel struct {
	Name string `json:"name"`
}

type MissingModel struct{}

type IncompleteModel struct{}

type Scalar string

func TestMizucue_CompileAndValidate(t *testing.T) {
	schema, err := Compile(`
package test

#TestModel: name: string & != ""
#IncompleteModel: value: string
`)
	if err != nil {
		t.Fatal(err)
	}
	model := TestModel{Name: "valid"}
	if err := Validate(schema, model); err != nil {
		t.Fatal(err)
	}
	if err := Validate(schema, &model); err != nil {
		t.Fatal(err)
	}
	var nilModel *TestModel
	if err := Validate(schema, nilModel); err == nil {
		t.Fatal("Validate() succeeded for a typed nil pointer")
	}
	if err := Validate[any](schema, nil); err == nil {
		t.Fatal("Validate() succeeded for nil")
	}
	if err := Validate(schema, MissingModel{}); err == nil || !strings.Contains(err.Error(), "lookup model schema MissingModel") {
		t.Fatalf("Validate() missing definition error = %v", err)
	}
	if err := Validate(schema, IncompleteModel{}); err == nil || !strings.Contains(err.Error(), "validate IncompleteModel") {
		t.Fatalf("Validate() non-concrete error = %v", err)
	}

	if _, err := Compile("package test\n#Broken: {"); err == nil || !strings.Contains(err.Error(), "compile CUE schema") {
		t.Fatalf("Compile() error = %v", err)
	}
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), "compile CUE schema") {
			t.Fatalf("MustCompile() panic = %v", recovered)
		}
	}()
	MustCompile("package test\n#Broken: {")
}

func TestMizucue_LoadFS(t *testing.T) {
	const module = "module: \"example.com/test\"\nlanguage: version: \"v0.16.1\"\n"
	schema, err := LoadFS("app", fstest.MapFS{
		"cue.mod/module.cue": {Data: []byte(module)},
		"app/schema.cue": {
			Data: []byte("package app\nimport \"example.com/test/lib\"\n#TestModel: lib.#Named"),
		},
		"lib/schema.cue": {
			Data: []byte("package lib\n#Named: {name: string & != \"\"}"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(schema, TestModel{Name: "valid"}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(schema, TestModel{}); err == nil {
		t.Fatal("Validate() succeeded for an empty imported name")
	}

	panicText := func(run func()) (message string) {
		defer func() {
			if value := recover(); value != nil {
				message = fmt.Sprint(value)
			}
		}()
		run()
		return ""
	}
	for _, test := range []struct {
		name string
		fs   fstest.MapFS
		want string
	}{
		{
			name: "malformed root schema",
			fs: fstest.MapFS{
				"cue.mod/module.cue": {Data: []byte(module)},
				"app/schema.cue":     {Data: []byte("package app\n#Broken: {")},
			},
			want: "load CUE package app:",
		},
		{
			name: "missing import",
			fs: fstest.MapFS{
				"cue.mod/module.cue": {Data: []byte(module)},
				"app/schema.cue": {
					Data: []byte("package app\nimport \"example.com/test/missing\"\n#TestModel: missing.#Model"),
				},
			},
			want: "load CUE package app:",
		},
		{
			name: "multiple packages",
			fs: fstest.MapFS{
				"cue.mod/module.cue": {Data: []byte(module)},
				"app/one.cue":        {Data: []byte("package one\n#One: string")},
				"app/two.cue":        {Data: []byte("package two\n#Two: string")},
			},
			want: "load CUE package app: found packages",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadFS("app", test.fs)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadFS() error = %v, want it to contain %q", err, test.want)
			}
			message := panicText(func() { MustLoadFS("app", test.fs) })
			if !strings.Contains(message, test.want) {
				t.Fatalf("panic = %q, want it to contain %q", message, test.want)
			}
		})
	}
}

func TestMizucue_GenerateOpenAPI(t *testing.T) {
	schema := MustCompile(`
package test

#Pet: {
	name: string
	_name: {
		description: "Pet name"
		contentMediaType: "text/plain"
	}
}

#CreatePetForm: {
	name: string
	image: string
	_image: contentMediaType: "image/png"
}

_authorized: {
	"__security": [{bearerAuth: []}] @go(-)
	"__components": {
		securitySchemes: bearerAuth: {
			type: "http"
			scheme: "bearer"
		}
	} @go(-)
}

#CreatePetRequest: _authorized & {
	path: id: string
	form: #CreatePetForm
	"__method": "post" @go(-)
	"__path": "/pets/{id}" @go(-)
	"__operationId": "createPet" @go(-)
	"__parameters": [{
		name: "trace", in: "header", required: false
		schema: type: "string"
	}, {
		name: "verbose", in: "query", required: false
		schema: type: "boolean"
	}] @go(-)
	"__responses": {
		"201": description: "Pet created"
		"400": description: "Invalid pet"
	} @go(-)
}

#CreatePetResponse: #Pet

#UpdatePetRequest: _authorized & {
	body: #Pet
	"__method": "put" @go(-)
	"__path": "/pets" @go(-)
	"__operationId": "updatePet" @go(-)
	"__responses": "200": {description: "Pet updated"} @go(-)
}

#UpdatePetResponse: #Pet

#ConnectRequest: {
	"__method": "get" @go(-)
	"__path": "/connect" @go(-)
	"__operationId": "connect" @go(-)
	"__responses": "101": {description: "protocol selected", content: {}} @go(-)
}

#ConnectResult: {protocol: string}
#ConnectResponse: #ConnectResult

#DownloadPetRequest: {
	"__method": "get" @go(-)
	"__path": "/pets/package" @go(-)
	"__operationId": "downloadPetPackage" @go(-)
	"__responses": "200": {
		description: "Compressed package"
		headers: "Content-Disposition": {
			required: true
			schema: type: "string"
		}
		content: "application/gzip": {}
	} @go(-)
}

#DownloadPetResponse: {}

#UnrelatedProviderModel: {value: _} & ({kind: "one"} | {kind: "two"})
`)
	document, err := GenerateOpenAPI(schema, "Pets", "v1", "TestV1")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v, want 3.1.0", decoded["openapi"])
	}
	if strings.Contains(string(document), "__") {
		t.Fatalf("generated document contains operation hints: %s", document)
	}
	object := func(value any) map[string]any {
		t.Helper()
		result, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("value = %#v, want object", value)
		}
		return result
	}
	paths := object(decoded["paths"])
	operation := object(object(paths["/pets/{id}"])["post"])
	if operation["operationId"] != "createPet" {
		t.Fatalf("operation = %#v", operation)
	}
	parameters := operation["parameters"].([]any)
	if len(parameters) != 3 || object(parameters[0])["name"] != "id" ||
		object(parameters[1])["name"] != "trace" || object(parameters[2])["name"] != "verbose" {
		t.Fatalf("parameters = %#v", parameters)
	}
	multipart := object(object(object(operation["requestBody"])["content"])["multipart/form-data"])
	if object(object(multipart["encoding"])["image"])["contentType"] != "image/png" {
		t.Fatalf("multipart = %#v", multipart)
	}
	update := object(object(paths["/pets"])["put"])
	jsonBody := object(object(object(update["requestBody"])["content"])["application/json"])
	if object(jsonBody["schema"])["$ref"] != "#/components/schemas/TestV1Pet" {
		t.Fatalf("JSON request body = %#v", jsonBody)
	}
	if security, ok := update["security"].([]any); !ok || len(security) != 1 {
		t.Fatalf("update security = %#v", update["security"])
	}
	connect := object(object(paths["/connect"])["get"])
	if _, ok := object(connect["responses"])["101"]; !ok {
		t.Fatalf("connect responses = %#v", connect["responses"])
	}
	download := object(object(paths["/pets/package"])["get"])
	downloadResponse := object(object(download["responses"])["200"])
	if _, ok := object(downloadResponse["content"])["application/gzip"]; !ok {
		t.Fatalf("download response content = %#v", downloadResponse["content"])
	}
	if _, ok := object(downloadResponse["headers"])["Content-Disposition"]; !ok {
		t.Fatalf("download response headers = %#v", downloadResponse["headers"])
	}
	created := object(object(operation["responses"])["201"])
	createdSchema := object(object(object(created["content"])["application/json"])["schema"])
	if createdSchema["$ref"] != "#/components/schemas/TestV1CreatePetResponse" {
		t.Fatalf("created response = %#v", created)
	}
	components := object(decoded["components"])
	securitySchemes := object(components["securitySchemes"])
	if object(securitySchemes["bearerAuth"])["scheme"] != "bearer" {
		t.Fatalf("security schemes = %#v", securitySchemes)
	}
	schemas := object(components["schemas"])
	if _, ok := schemas["TestV1ConnectResult"]; !ok {
		t.Fatalf("response alias target missing from schemas: %#v", schemas)
	}
	if _, ok := schemas["TestV1CreatePetRequest"]; ok {
		t.Fatalf("request operation source leaked as a component: %#v", schemas)
	}
	for name := range schemas {
		if strings.HasSuffix(name, "_value") {
			t.Fatalf("schemas contain an operation artifact %s", name)
		}
	}
	pet, ok := schemas["TestV1Pet"].(map[string]any)
	if !ok {
		t.Fatalf("schemas = %#v", schemas)
	}
	name := pet["properties"].(map[string]any)["name"].(map[string]any)
	if name["description"] != "Pet name" || name["contentMediaType"] != "text/plain" {
		t.Fatalf("name schema = %#v", name)
	}
}

func TestMizucue_GenerateOpenAPIRecursivePropertyHint(t *testing.T) {
	schema := MustCompile(`
package test

#Parent: {
	child: {
		value: string
		_value: description: "Nested value"
	}
}
`)
	document, err := GenerateOpenAPI(schema, "Recursive", "v1", "TestV1")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatal(err)
	}
	components := decoded["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	parent := schemas["TestV1Parent"].(map[string]any)
	child := parent["properties"].(map[string]any)["child"].(map[string]any)
	value := child["properties"].(map[string]any)["value"].(map[string]any)
	if value["description"] != "Nested value" {
		t.Fatalf("nested value schema = %#v", value)
	}
}

func TestMizucue_GenerateOpenAPISupportedMethods(t *testing.T) {
	methods := []string{"get", "post", "put", "delete", "patch", "head", "options", "trace", "connect"}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			raw := fmt.Sprintf(`package test
#MethodRequest: {
	"__method": %q @go(-)
	"__path": %q @go(-)
	"__operationId": %q @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#MethodResponse: {}
`, method, "/"+method, method+"Operation")
			document, err := GenerateOpenAPI(MustCompile(raw), "Methods", "v1", "")
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(document, &decoded); err != nil {
				t.Fatal(err)
			}
			path := decoded["paths"].(map[string]any)["/"+method].(map[string]any)
			if _, ok := path[method]; !ok {
				t.Fatalf("path item = %#v", path)
			}
		})
	}
}

func TestMizucue_GenerateOpenAPIRejectsOrphanHint(t *testing.T) {
	schema := MustCompile(`
package test

#Broken: {
	_missing: description: "Missing field"
}

`)
	_, err := GenerateOpenAPI(schema, "Broken", "v1", "")
	if err == nil || !strings.Contains(err.Error(), "has no generated field") {
		t.Fatalf("GenerateOpenAPI() error = %v", err)
	}
}

func TestMizucue_GenerateOpenAPIRejectsConflictingHint(t *testing.T) {
	schema := MustCompile(`
package test

#Broken: {
	name: string
	_name: type: "integer"
}

`)
	_, err := GenerateOpenAPI(schema, "Broken", "v1", "")
	if err == nil || !strings.Contains(err.Error(), "conflicting OpenAPI value") {
		t.Fatalf("GenerateOpenAPI() error = %v", err)
	}
}

func TestMizucue_GenerateOpenAPIRejectsInvalidOperations(t *testing.T) {
	for _, test := range []struct {
		name string
		cue  string
		want string
	}{
		{
			name: "non-request operation",
			cue: `package test
#Broken: {
	"__method": "get" @go(-)
	"__path": "/broken" @go(-)
}`,
			want: "require a Request definition",
		},
		{
			name: "missing binding",
			cue: `package test
#BrokenRequest: {
	"__method": "get" @go(-)
}`,
			want: "requires __method and __path",
		},
		{
			name: "unsupported method",
			cue: `package test
#BrokenRequest: {
	"__method": "fetch" @go(-)
	"__path": "/broken" @go(-)
}`,
			want: `unsupported method "fetch"`,
		},
		{
			name: "missing operation id",
			cue: `package test
#BrokenRequest: {
	"__method": "get" @go(-)
	"__path": "/broken" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "requires __operationId",
		},
		{
			name: "empty operation id",
			cue: `package test
#BrokenRequest: {
	"__method": "get" @go(-)
	"__path": "/broken" @go(-)
	"__operationId": "" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "requires __operationId",
		},
		{
			name: "missing responses",
			cue: `package test
#BrokenRequest: {
	"__method": "get" @go(-)
	"__path": "/broken" @go(-)
	"__operationId": "broken" @go(-)
}
#BrokenResponse: {}`,
			want: "requires __responses",
		},
		{
			name: "no successful response",
			cue: `package test
#BrokenRequest: {
	"__method": "get" @go(-)
	"__path": "/broken" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "400": {description: "bad request"} @go(-)
}
#BrokenResponse: {}`,
			want: "requires exactly one successful response",
		},
		{
			name: "multiple successful responses",
			cue: `package test
#BrokenRequest: {
	"__method": "get" @go(-)
	"__path": "/broken" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": {
		"200": {description: "ok"}
		"201": {description: "also ok"}
	} @go(-)
}
#BrokenResponse: {}`,
			want: "requires exactly one successful response",
		},
		{
			name: "optional path",
			cue: `package test
#BrokenRequest: {
	path?: {id: string}
	"__method": "get" @go(-)
	"__path": "/broken/{id}" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "path must be required",
		},
		{
			name: "optional path field",
			cue: `package test
#BrokenRequest: {
	path: id?: string
	"__method": "get" @go(-)
	"__path": "/broken/{id}" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "path field id must be required",
		},
		{
			name: "placeholder mismatch",
			cue: `package test
#BrokenRequest: {
	path: id: string
	"__method": "get" @go(-)
	"__path": "/broken/{other}" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {
		description: "ok"
		content: "text/plain": {}
	} @go(-)
}
#BrokenResponse: {}`,
			want: "path placeholders do not match path fields",
		},
		{
			name: "missing placeholder",
			cue: `package test
#BrokenRequest: {
	path: id: string
	"__method": "get" @go(-)
	"__path": "/broken" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "path placeholders do not match path fields",
		},
		{
			name: "extra placeholder",
			cue: `package test
#BrokenRequest: {
	"__method": "get" @go(-)
	"__path": "/broken/{id}" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "path placeholders do not match path fields",
		},
		{
			name: "malformed placeholder",
			cue: `package test
#BrokenRequest: {
	path: id: string
	"__method": "get" @go(-)
	"__path": "/broken/{id" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {
		description: "ok"
		content: "text/plain": {}
	} @go(-)
}
#BrokenResponse: {}`,
			want: "malformed path placeholders",
		},
		{
			name: "duplicate placeholder",
			cue: `package test
#BrokenRequest: {
	path: id: string
	"__method": "get" @go(-)
	"__path": "/broken/{id}/{id}" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: `duplicate path placeholder "id"`,
		},
		{
			name: "form and body",
			cue: `package test
#Payload: {name: string}
#BrokenRequest: {
	form: #Payload
	body: #Payload
	"__method": "post" @go(-)
	"__path": "/broken" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "cannot define both form and body",
		},
		{
			name: "optional body",
			cue: `package test
#Payload: {name: string}
#BrokenRequest: {
	body?: #Payload
	"__method": "post" @go(-)
	"__path": "/broken" @go(-)
	"__operationId": "broken" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "body must be required",
		},
		{
			name: "conflicting request body hint",
			cue: `package test
#Payload: {name: string}
#BrokenRequest: {
	body: #Payload
	"__method": "post" @go(-)
	"__path": "/broken" @go(-)
	"__operationId": "broken" @go(-)
	"__requestBody": {required: false} @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: "conflicting OpenAPI value at paths./broken.post.requestBody.required",
		},
		{
			name: "duplicate route",
			cue: `package test
#OneRequest: {
	"__method": "get" @go(-)
	"__path": "/same" @go(-)
	"__operationId": "one" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#OneResponse: {}
#TwoRequest: {
	"__method": "get" @go(-)
	"__path": "/same" @go(-)
	"__operationId": "two" @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#TwoResponse: {}`,
			want: "duplicate OpenAPI operation GET /same",
		},
		{
			name: "duplicate hinted parameter",
			cue: `package test
#BrokenRequest: {
	path: id: string
	"__method": "get" @go(-)
	"__path": "/broken/{id}" @go(-)
	"__operationId": "broken" @go(-)
	"__parameters": [{name: "id", in: "path", required: true, schema: type: "string"}] @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#BrokenResponse: {}`,
			want: `duplicate path parameter "id"`,
		},
		{
			name: "component conflict",
			cue: `package test
#OneRequest: {
	"__method": "get" @go(-)
	"__path": "/one" @go(-)
	"__operationId": "one" @go(-)
	"__components": {securitySchemes: auth: {type: "http", scheme: "bearer"}} @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#OneResponse: {}
#TwoRequest: {
	"__method": "get" @go(-)
	"__path": "/two" @go(-)
	"__operationId": "two" @go(-)
	"__components": {securitySchemes: auth: {type: "http", scheme: "basic"}} @go(-)
	"__responses": "200": {description: "ok", content: "text/plain": {}} @go(-)
}
#TwoResponse: {}`,
			want: "conflicting OpenAPI value at components.securitySchemes.auth.scheme",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := MustCompile(test.cue)
			_, err := GenerateOpenAPI(schema, "Broken", "v1", "")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("GenerateOpenAPI() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMizucue_MustGenerateOpenAPI(t *testing.T) {
	schema := MustCompile(`package test
#BrokenRequest: {
	"__method": "get" @go(-)
	"__path": "/broken" @go(-)
}`)
	if _, err := GenerateOpenAPI(schema, "Broken", "v1", ""); err == nil {
		t.Fatal("GenerateOpenAPI() succeeded for an invalid operation")
	}
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), "requires __operationId") {
			t.Fatalf("MustGenerateOpenAPI() panic = %v", recovered)
		}
	}()
	MustGenerateOpenAPI(schema, "Broken", "v1", "")
}

func TestMizucue_ExtractOpenAPIIsConcurrentAndDoesNotShareTopLevelMutation(t *testing.T) {
	schema := MustCompile("package test\n#TestModel: {name: string}")
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			out, err := ExtractOpenAPI(schema, TestModel{})
			if err != nil {
				t.Error(err)
				return
			}
			out["caller"] = true
		}()
	}
	wait.Wait()
	out, err := ExtractOpenAPI(schema, TestModel{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out["caller"]; ok {
		t.Fatal("caller mutation leaked into cached schema")
	}
	pointer, err := ExtractOpenAPI(schema, &TestModel{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pointer["properties"]; !ok {
		t.Fatalf("pointer component = %#v", pointer)
	}
}

func TestMizucue_ExtractOpenAPIErrors(t *testing.T) {
	schema := MustCompile("package test\n#TestModel: {name: string}\n#Scalar: string")
	if _, err := ExtractOpenAPI(schema, MissingModel{}); err == nil || !strings.Contains(err.Error(), "missing schema: MissingModel") {
		t.Fatalf("ExtractOpenAPI() missing schema error = %v", err)
	}
	if _, err := ExtractOpenAPI[any](schema, nil); err == nil || !strings.Contains(err.Error(), "nil value") {
		t.Fatalf("ExtractOpenAPI() nil error = %v", err)
	}
	scalar := MustExtractOpenAPI(schema, Scalar("value"))
	properties, ok := scalar["properties"].(map[string]any)
	if !ok || len(properties) != 0 {
		t.Fatalf("scalar properties = %#v", scalar["properties"])
	}
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), "missing schema: MissingModel") {
			t.Fatalf("MustExtractOpenAPI() panic = %v", recovered)
		}
	}()
	MustExtractOpenAPI(schema, MissingModel{})
}
