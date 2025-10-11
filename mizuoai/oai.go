package mizuoai

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/humbornjo/mizu"
)

var (
	ErrOaiVersion = errors.New("unsupported openapi version")

	//go:embed tmpl_stoplight.html
	_STOPLIGHT_UI_TEMPLATE_CONTENT string
	_STOPLIGHT_UI_TEMPLATE         = template.Must(template.New("oai_ui").Parse(_STOPLIGHT_UI_TEMPLATE_CONTENT))
)

type oai struct {
	server    *mizu.Server
	oaiConfig *oaiConfig
}

type ctxkey int

const (
	_CTXKEY_OAI_INITIALIZED ctxkey = iota
	_CTXKEY_OAI_CONFIG
)

// NewOai creates a new scope with the given path and options.
// openapi.json will be served at /{path}/openapi.json. HTML will
// be served at /{path}/openapi if enabled.
func NewOai(server *mizu.Server, title string, opts ...OaiOption) *oai {
	if title == "" {
		panic("title is required")
	}

	config := new(oaiConfig)
	config.info.Title = title
	for _, opt := range opts {
		opt(config)
	}

	if config.enableDoc {
		once := sync.Once{}
		mizu.Hook(server, _CTXKEY_OAI_INITIALIZED, &once, mizu.WithHookHandler(func(srv *mizu.Server) {
			oaiJson, err := renderJson(config)
			if err != nil {
				log.Printf("🚨 [ERROR] Failed to generate openapi.json: %s\n", err)
				return
			}

			// Serve openapi.json
			srv.Get(path.Join(config.pathDoc, "/openapi.json"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_, _ = w.Write(oaiJson)
			})
			srv.Get(path.Join(config.pathDoc, "/openapi"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_ = _STOPLIGHT_UI_TEMPLATE.Execute(w, map[string]string{"Path": path.Join(config.pathDoc, "/openapi.json")})
			})
		}))
	}

	return &oai{server: server, oaiConfig: config}
}

// Path registers a new path in the OpenAPI spec. It can be used
// to set the path field that can't be accessed via Get, Post,
// etc.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func (oai *oai) Path(pattern string, opts ...PathOption) {
	config := new(pathConfig)
	for _, opt := range opts {
		opt(config)
	}
	oai.oaiConfig.paths.Set(pattern, &config.PathItem)
}

// Rx represents the request side of an API endpoint. It provides
// access to the parsed request data and the original request
// context.
type Rx[T any] struct {
	*http.Request
	read func(*http.Request) *T
}

// Read returns the parsed input from the request. The parsing
// logic is generated based on the struct tags of the input type.
func (rx Rx[T]) MizuRead() *T {
	return rx.read(rx.Request)
}

// Tx represents the response side of an API endpoint. It
// provides methods to write the response.
type Tx[T any] struct {
	http.ResponseWriter
	write func(*T) error
}

// TODO: this should also use inner write method
//
// Write writes the JSON-encoded output to the response writer.
// It also sets the Content-Type header to "application/json;
// charset=utf-8".
func (tx Tx[T]) MizuWrite(data *T) error {
	tx.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(tx.ResponseWriter).Encode(data)
}

// tag represents the source of request data (e.g., path, body).
type tag string

func (t tag) String() string {
	return string(t)
}

const (
	_TAG_PATH   tag = "path"
	_TAG_QUERY  tag = "query"
	_TAG_HEADER tag = "header"
	_TAG_BODY   tag = "body"
	_TAG_FORM   tag = "form"
)

// notion holds metadata about a struct field to be parsed from a
// request.
type notion struct {
	fieldNumber int
	identifier  string
}

// genNotions extracts metadata for fields within a struct that
// are tagged with a specific data source tag as notion.
func genNotions(val reflect.Value, typ tag) []notion {
	notions := []notion{}
	for i := range val.Type().NumField() {
		field := val.Type().Field(i)
		tagVal := field.Tag.Get(typ.String())
		if tagVal == "" {
			tagVal = field.Name
		}
		notions = append(notions, notion{fieldNumber: i, identifier: tagVal})
	}
	return notions
}

// decode is a collection of functions that perform parsing of an
// http.Request into a target struct.
type decode[T any] func(r *http.Request, val *T) []error

func (p *decode[T]) append(f decode[T]) {
	if *p == nil {
		*p = f
		return
	}

	old := *p
	*p = func(r *http.Request, val *T) []error {
		errs := old(r, val)
		return append(errs, f(r, val)...)
	}
}

// genDecode creates a parser for a given generic type T. It
// uses reflection to inspect the fields and tags of T to build a
// set of parsing functions for different parts of the request.
//
// nolint: gocyclo
func genDecode[T any]() decode[T] {
	input := new(T)
	val := reflect.ValueOf(input).Elem()
	typ := val.Type()

	hasBody := false
	hasForm := false

	p := new(decode[T])
	for i := range typ.NumField() {
		fieldTyp := typ.Field(i)
		if mizuTag, ok := fieldTyp.Tag.Lookup("mizu"); ok {
			switch tag(mizuTag) {
			case _TAG_PATH:
				if fieldTyp.Type.Kind() != reflect.Struct {
					panic("path must be a struct")
				}
				structPath := val.FieldByName(fieldTyp.Name)
				notionPath := genNotions(structPath, _TAG_PATH)
				if len(notionPath) == 0 {
					continue
				}
				p.append(func(r *http.Request, val *T) []error {
					errs := []error{}
					st := reflect.ValueOf(val).Elem().Field(i)
					for _, notion := range notionPath {
						if v := r.PathValue(notion.identifier); v != "" {
							if err := setField(st.Field(notion.fieldNumber), strings.NewReader(v)); err != nil {
								errs = append(errs, fmt.Errorf("path param '%s': %w", notion.identifier, err))
							}
						}
					}
					return errs
				})
			case _TAG_QUERY:
				if fieldTyp.Type.Kind() != reflect.Struct {
					panic("query must be a struct")
				}
				structQuery := val.FieldByName(fieldTyp.Name)
				notionQuery := genNotions(structQuery, _TAG_QUERY)
				if len(notionQuery) == 0 {
					continue
				}
				p.append(func(r *http.Request, val *T) []error {
					errs := []error{}
					st := reflect.ValueOf(val).Elem().Field(i)
					for _, notion := range notionQuery {
						if v := r.URL.Query().Get(notion.identifier); v != "" {
							if err := setField(st.Field(notion.fieldNumber), strings.NewReader(v)); err != nil {
								errs = append(errs, fmt.Errorf("query param '%s': %w", notion.identifier, err))
							}
						}
					}
					return errs
				})
			case _TAG_HEADER:
				if fieldTyp.Type.Kind() != reflect.Struct {
					panic("header must be a struct")
				}
				structHeader := val.FieldByName(fieldTyp.Name)
				notionHeader := genNotions(structHeader, _TAG_HEADER)
				if len(notionHeader) == 0 {
					continue
				}
				p.append(func(r *http.Request, val *T) []error {
					errs := []error{}
					st := reflect.ValueOf(val).Elem().Field(i)
					for _, notion := range notionHeader {
						if v := r.Header.Get(notion.identifier); v != "" {
							if err := setField(st.Field(notion.fieldNumber), strings.NewReader(v)); err != nil {
								errs = append(errs, fmt.Errorf("header '%s': %w", notion.identifier, err))
							}
						}
					}
					return errs
				})
			case _TAG_FORM:
				if hasBody {
					panic("cannot use both form and body")
				}
				if hasForm {
					panic("cannot use multiple form")
				}
				hasForm = true
				structForm := val.FieldByName(fieldTyp.Name)
				notionForm := genNotions(structForm, _TAG_FORM)
				p.append(func(r *http.Request, val *T) []error {
					errs := []error{}
					if err := r.ParseForm(); err != nil {
						errs = append(errs, err)
					}
					st := reflect.ValueOf(val).Elem().Field(i)
					for _, notion := range notionForm {
						if v := r.FormValue(notion.identifier); v != "" {
							if err := setField(st.Field(notion.fieldNumber), strings.NewReader(v)); err != nil {
								errs = append(errs, fmt.Errorf("form param '%s': %w", notion.identifier, err))
							}
						}
					}
					return errs
				})
			case _TAG_BODY:
				if hasForm {
					panic("cannot use both form and body")
				}
				if hasBody {
					panic("cannot use multiple body")
				}
				hasBody = true
				p.append(func(r *http.Request, val *T) []error {
					if err := setField(reflect.ValueOf(val).Elem().Field(i), r.Body); err != nil {
						return []error{fmt.Errorf("body: %w", err)}
					}
					return nil
				})
			}
		}
	}

	if *p == nil {
		return func(r *http.Request, val *T) []error { return nil }
	}
	return *p
}

type encode[T any] func(http.ResponseWriter, *T) error

func genEncode[T any]() encode[T] {
	v := new(T)
	field := reflect.ValueOf(v).Elem()
	switch field.Kind() {
	case reflect.String:
		return func(w http.ResponseWriter, val *T) error {
			w.Header().Set("Content-Type", "text/plain")
			_, err := w.Write([]byte(any(*val).(string)))
			return err
		}
	default:
		return func(w http.ResponseWriter, val *T) error {
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(val)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return err
		}
	}
}

// handler is a generic type for user-provided API logic.
type handler[I any, O any] func(Tx[O], Rx[I])

// genHandler wraps the user-provided handler with request
// parsing logic.
func (h handler[I, O]) genHandler() http.HandlerFunc {
	encode := genEncode[O]()
	decode := genDecode[I]()

	// Return the actual http.HandlerFunc.
	return func(w http.ResponseWriter, r *http.Request) {
		// Lazily parse the request data.
		tx := Tx[O]{ResponseWriter: w, write: func(val *O) error {
			return encode(w, val)
		}}
		rx := Rx[I]{Request: r, read: func(r *http.Request) *I {
			input := new(I)
			decode(r, input)
			return input
		}}
		h(tx, rx)
	}
}

// setField populates a reflect.Value from an io.Reader. It
// handles primitive types by parsing them from strings, and
// struct types by using a JSON decoder.
func setField(field reflect.Value, reader io.Reader) error {
	if !field.CanSet() {
		return fmt.Errorf("cannot set field")
	}

	typ := field.Type()
	// For struct types, assume JSON body and decode into a new instance.
	if typ.Kind() == reflect.Struct {
		obj := reflect.New(typ).Interface()
		if err := json.NewDecoder(reader).Decode(&obj); err != nil {
			return err
		}
		field.Set(reflect.ValueOf(obj).Elem())
		return nil
	}

	// For primitive types, read the value as a string and parse it.
	valueb, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	value := string(valueb)

	switch typ.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid integer: %w", err)
		}
		field.SetInt(intVal)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid unsigned integer: %w", err)
		}
		field.SetUint(uintVal)
	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean: %w", err)
		}
		field.SetBool(boolVal)
	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float: %w", err)
		}
		field.SetFloat(floatVal)
	default:
		return fmt.Errorf("unsupported field type %s", field.Type())
	}
	return nil
}

func enrichOperation[I any, O any](config *operationConfig) {
	input := new(I)
	valInput := reflect.ValueOf(input).Elem()
	typInput := valInput.Type()
	if typInput.Kind() != reflect.Struct {
		panic("input type must be a struct")
	}

	// Process input type (request parameters/body)
	for i := range typInput.NumField() {
		field := typInput.Field(i)
		mizuTag, ok := field.Tag.Lookup("mizu")
		if !ok {
			continue
		}
		switch tag(mizuTag) {
		case _TAG_PATH, _TAG_QUERY, _TAG_HEADER:
			for i := range field.Type.NumField() {
				subfield := field.Type.Field(i)
				subTag := subfield.Tag.Get(mizuTag)
				if subTag == "" || subTag == "-" {
					continue
				}
				param := &openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:        subTag,
						In:          mizuTag,
						Description: subfield.Tag.Get("desc"),
						Deprecated:  subfield.Tag.Get("deprecated") == "true",
						Schema:      openapi3.NewSchemaRef("", createSchema(subfield.Type)),
					},
				}
				config.Parameters = append(config.Parameters, param)
			}
		case _TAG_BODY:
			config.RequestBody = &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Description: field.Tag.Get("desc"),
					Required:    field.Tag.Get("required") == "true",
				},
			}
			var contentType string
			switch field.Type.Kind() {
			case reflect.String:
				contentType = "plain/text"
			default:
				contentType = "application/json"
			}
			config.RequestBody.Value.WithSchema(createSchema(field.Type), []string{contentType})
		case _TAG_FORM:
			config.RequestBody = &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Description: field.Tag.Get("desc"),
				},
			}
			contentType := "application/x-www-form-urlencoded"
			config.RequestBody.Value.WithSchema(createSchema(field.Type), []string{contentType})
		}
	}

	output := new(O)
	valOutput := reflect.ValueOf(output).Elem()
	typOutput := valOutput.Type()

	// Process output type (response body)
	if config.Responses == nil {
		config.Responses = &openapi3.Responses{}
	}

	// Set default response
	response := &openapi3.Response{Content: make(openapi3.Content)}
	response.Links = config.responseLinks
	response.Headers = config.responseHeaders

	var contentType string
	switch valOutput.Kind() {
	case reflect.String:
		contentType = "plain/text"
	default:
		contentType = "application/json"
	}
	response.Content[contentType] = &openapi3.MediaType{
		Schema: openapi3.NewSchemaRef("", createSchema(typOutput)),
	}
	defaultKey := "200"
	if config.responseCode != nil {
		defaultKey = strconv.Itoa(*config.responseCode)
	}
	config.Responses.Set(defaultKey, &openapi3.ResponseRef{Value: response})
}

// createSchema creates a *base.SchemaProxy from a reflect.Type
func createSchema(typ reflect.Type) *openapi3.Schema {
	// Dereference pointer types to get the underlying type.
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	var typs openapi3.Types
	schema := openapi3.NewSchema()
	switch typ.Kind() {
	case reflect.String:
		typs = openapi3.Types([]string{"string"})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		typs = openapi3.Types([]string{"integer"})
	case reflect.Float32, reflect.Float64:
		typs = openapi3.Types([]string{"number"})
	case reflect.Bool:
		typs = openapi3.Types([]string{"boolean"})
	case reflect.Struct:
		typs = openapi3.Types([]string{"object"})
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
			if jsonTag == "" || jsonTag == "-" {
				continue
			}

			fieldSchema := createSchema(field.Type)
			if fieldSchema != nil {
				schema.WithProperty(jsonTag, fieldSchema)
			}
		}
	case reflect.Slice:
		typs = openapi3.Types([]string{"array"})
		schema.WithItems(createSchema(typ.Elem()))
	default:
		// Unsupported types will result in a nil schema.
		return nil
	}
	schema.Type = &typs
	return schema
}

func renderJson(c *oaiConfig) ([]byte, error) {
	defaultPreLoaded := []byte("openapi: 3.0.4")
	if c.preLoaded == nil {
		loader := openapi3.Loader{}
		preLoaded, err := loader.LoadFromData(defaultPreLoaded)
		if err != nil {
			return nil, err
		}
		c.preLoaded = preLoaded
	}

	// Merge with Pre Loaded OpenAPI Object
	modelv3 := c.preLoaded
	if modelv3.Info == nil {
		modelv3.Info = &c.info
	} else {
		if c.info.Contact != nil {
			modelv3.Info.Contact = c.info.Contact
		}
		if c.info.Description != "" {
			modelv3.Info.Description = c.info.Description
		}
		if c.info.License != nil {
			modelv3.Info.License = c.info.License
		}
		if c.info.Title != "" {
			modelv3.Info.Title = c.info.Title
		}
		if c.info.Version != "" {
			modelv3.Info.Version = c.info.Version
		}
		if c.info.TermsOfService != "" {
			modelv3.Info.TermsOfService = c.info.TermsOfService
		}
	}
	modelv3.Tags = append(modelv3.Tags, c.tags...)
	modelv3.Servers = append(modelv3.Servers, c.servers...)
	modelv3.Security = append(modelv3.Security, c.security...)
	if modelv3.ExternalDocs != nil {
		modelv3.ExternalDocs = c.externalDocs
	}
	if modelv3.Extensions == nil {
		modelv3.Extensions = c.extensions
	} else {
		for k, v := range c.extensions {
			if _, ok := modelv3.Extensions[k]; !ok {
				modelv3.Extensions[k] = v
			}
		}
	}

	if !strings.HasPrefix(modelv3.OpenAPI, "3.0") {
		return nil, ErrOaiVersion
	}

	if modelv3.Paths == nil {
		modelv3.Paths = openapi3.NewPaths()
	}

	for _, handler := range c.handlers {
		pathItem := modelv3.Paths.Find(handler.path)
		if pathItem == nil {
			pathItem = &openapi3.PathItem{}
		}
		switch handler.method {
		case http.MethodGet:
			pathItem.Get = &handler.Operation
		case http.MethodPost:
			pathItem.Post = &handler.Operation
		case http.MethodPut:
			pathItem.Put = &handler.Operation
		case http.MethodDelete:
			pathItem.Delete = &handler.Operation
		case http.MethodPatch:
			pathItem.Patch = &handler.Operation
		case http.MethodHead:
			pathItem.Head = &handler.Operation
		case http.MethodOptions:
			pathItem.Options = &handler.Operation
		case http.MethodTrace:
			pathItem.Trace = &handler.Operation
		default:
			panic("unreachable")
		}
		modelv3.Paths.Set(handler.path, pathItem)
	}

	return modelv3.MarshalJSON()
}
