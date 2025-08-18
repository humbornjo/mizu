package mizuoai

import (
	"context"
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
	"sync/atomic"
	"text/template"

	"github.com/humbornjo/mizu"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

var (
	ErrOaiVersion = errors.New("unsupported openapi version")

	//go:embed tmpl_stoplight.html
	_STOPLIGHT_UI_TEMPLATE_CONTENT string
	_STOPLIGHT_UI_TEMPLATE         = template.Must(template.New("oai_ui").Parse(_STOPLIGHT_UI_TEMPLATE_CONTENT))
)

type oai struct {
	path      string
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
func NewOai(server *mizu.Server, pattern string, opts ...OaiOption) *oai {
	config := &oaiConfig{
		webhooks:                 orderedmap.New[string, *v3.PathItem](),
		componentSecuritySchemas: orderedmap.New[string, *v3.SecurityScheme](),
	}
	for _, opt := range opts {
		opt(config)
	}

	server.InjectContext(func(ctx context.Context) context.Context {
		once := ctx.Value(_CTXKEY_OAI_INITIALIZED)
		if once == nil {
			ctx = context.WithValue(ctx, _CTXKEY_OAI_INITIALIZED, &atomic.Bool{})
		}

		value := ctx.Value(_CTXKEY_OAI_CONFIG)
		if value == nil {
			return context.WithValue(ctx, _CTXKEY_OAI_CONFIG, config)
		}
		return ctx
	})

	enableDoc := config.enableDoc
	server.HookOnExtractHandler(func(ctx context.Context, srv *mizu.Server) {
		once, _ := ctx.Value(_CTXKEY_OAI_INITIALIZED).(*atomic.Bool)
		if once != nil && once.CompareAndSwap(true, false) {
			return
		}

		oaiYaml, err := renderYaml(config)
		if err != nil {
			log.Printf("failed to generate openapi.json: %s", err)
			return
		}

		// Serve openapi.yaml
		srv.Get(path.Join(pattern, "/openapi.yml"), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/yaml")
			_, _ = w.Write(oaiYaml)
		})

		// Serve Swagger UI
		if enableDoc {
			srv.Get(path.Join(pattern, "/openapi"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_ = _STOPLIGHT_UI_TEMPLATE.Execute(w, map[string]string{"Path": path.Join(pattern, "/openapi.yml")})
			})
		}
	})

	return &oai{path: pattern, server: server, oaiConfig: config}
}

// Get registers a generic handler for GET requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Get[I any, O any](oai *oai, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OperationOption) {
	if oai == nil {
		panic("scope is nil")
	}

	config := &operationConfig{
		path:                       pattern,
		method:                     http.MethodGet,
		responseHeaders:            orderedmap.New[string, *v3.Header](),
		responseLinks:              orderedmap.New[string, *v3.Link](),
		extraResponses:             map[int]*v3.Response{},
		callbacks:                  orderedmap.New[string, *v3.Callback](),
		getComponentSecuritySchema: oai.oaiConfig.componentSecuritySchemas.Get,
	}
	enrichOperation[I, O](config)
	for _, opt := range opts {
		opt(config)
	}
	oai.oaiConfig.handlers = append(oai.oaiConfig.handlers, config)

	oai.server.Get(pattern, handler[I, O](oaiHandler).genHandler())
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
}

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

// parser is a collection of functions that perform parsing of an
// http.Request into a target struct.
type parser[T any] func(r *http.Request, val *T) []error

func (p *parser[T]) append(f parser[T]) {
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

// genParser creates a parser for a given generic type T. It uses
// reflection to inspect the fields and tags of T to build a set
// of parsing functions for different parts of the request.
func genParser[T any]() parser[T] {
	input := new(T)
	val := reflect.ValueOf(input).Elem()
	typ := val.Type()

	hasBody := false
	hasForm := false

	p := new(parser[T])
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
				// TODO: Implement form parsing.
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

// handler is a generic type for user-provided API logic.
type handler[I any, O any] func(Tx[O], Rx[I])

// genHandler wraps the user-provided handler with request
// parsing logic.
func (h handler[I, O]) genHandler() http.HandlerFunc {
	parser := genParser[I]()

	// Return the actual http.HandlerFunc.
	return func(w http.ResponseWriter, r *http.Request) {
		// Lazily parse the request data.
		rx := Rx[I]{Request: r, read: func(r *http.Request) *I {
			input := new(I)
			parser(r, input)
			return input
		}}
		// The response object (tx) wraps the original http.ResponseWriter.
		tx := Tx[O]{ResponseWriter: w}

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
				param := &v3.Parameter{
					Name:        subTag,
					In:          mizuTag,
					Description: subfield.Tag.Get("desc"),
					Deprecated:  subfield.Tag.Get("deprecated") == "true",
					Schema:      createSchemaProxy(subfield.Type),
				}
				config.Parameters = append(config.Parameters, param)
			}
		case _TAG_BODY:
			config.RequestBody = &v3.RequestBody{
				Description: field.Tag.Get("desc"),
				Content:     orderedmap.New[string, *v3.MediaType](),
			}
			var contentType string
			switch field.Type.Kind() {
			case reflect.String:
				contentType = "plain/text"
			default:
				contentType = "application/json"
			}
			config.RequestBody.Content.Set(contentType, &v3.MediaType{Schema: createSchemaProxy(field.Type)})
		case _TAG_FORM:
			config.RequestBody = &v3.RequestBody{
				Description: field.Tag.Get("desc"),
				Content:     orderedmap.New[string, *v3.MediaType](),
			}
			config.RequestBody.Content.Set("application/x-www-form-urlencoded", &v3.MediaType{
				Schema: createSchemaProxy(field.Type),
			})
		}
	}

	output := new(O)
	valOutput := reflect.ValueOf(output).Elem()
	typOutput := valOutput.Type()

	// Process output type (response body)
	if config.Responses == nil {
		config.Responses = &v3.Responses{
			Codes: orderedmap.New[string, *v3.Response](),
		}
	}

	// Set default response
	response := &v3.Response{
		Links:       config.responseLinks,
		Headers:     config.responseHeaders,
		Description: config.responseDescription,
		Content:     orderedmap.New[string, *v3.MediaType](),
	}
	response.Content.Set("application/json", &v3.MediaType{
		Schema: createSchemaProxy(typOutput),
	})
	defaultCode := 200
	if config.responseCode != 0 {
		defaultCode = config.responseCode
	}
	defaultContentKey := strconv.Itoa(defaultCode)
	config.Responses.Codes.Set(defaultContentKey, response)

	// Set extra Responses
	for code, response := range config.extraResponses {
		config.Responses.Codes.Set(strconv.Itoa(code), response)
	}
}

// createSchemaProxy creates a *base.SchemaProxy from a reflect.Type
func createSchemaProxy(typ reflect.Type) *base.SchemaProxy {
	// Dereference pointer types to get the underlying type.
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	schema := &base.Schema{}
	switch typ.Kind() {
	case reflect.String:
		schema.Type = []string{"string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = []string{"integer"}
	case reflect.Float32, reflect.Float64:
		schema.Type = []string{"number"}
	case reflect.Bool:
		schema.Type = []string{"boolean"}
	case reflect.Struct:
		schema.Type = []string{"object"}
		schema.Properties = orderedmap.New[string, *base.SchemaProxy]()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
			if jsonTag == "" || jsonTag == "-" {
				continue
			}

			fieldSchema := createSchemaProxy(field.Type)
			if fieldSchema != nil {
				schema.Properties.Set(jsonTag, fieldSchema)
			}
		}
	case reflect.Slice:
		schema.Type = []string{"array"}
		schema.Items = &base.DynamicValue[*base.SchemaProxy, bool]{
			A: createSchemaProxy(typ.Elem()),
		}
	default:
		// Unsupported types will result in a nil schema.
		return nil
	}

	return base.CreateSchemaProxy(schema)
}

func renderYaml(c *oaiConfig) ([]byte, error) {
	preLoaded := []byte("{\"openapi\": \"3.1.0\"}")
	if c.preLoaded != nil {
		preLoaded = c.preLoaded
	}
	doc, err := libopenapi.NewDocument(preLoaded)
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(doc.GetVersion(), "3.") {
		return nil, ErrOaiVersion
	}

	modelv3, errs := doc.BuildV3Model()
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	modelv3.Model.Info = c.info
	modelv3.Model.Tags = c.tags
	modelv3.Model.Servers = c.servers
	modelv3.Model.Security = c.securities
	modelv3.Model.ExternalDocs = c.externalDocs
	modelv3.Model.JsonSchemaDialect = c.jsonSchemaDialect

	if modelv3.Model.Paths == nil {
		modelv3.Model.Paths = &v3.Paths{
			PathItems: orderedmap.New[string, *v3.PathItem](),
		}
	}
	for _, handler := range c.handlers {
		pathItem := v3.PathItem{}
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
		modelv3.Model.Paths.PathItems.Set(handler.path, &pathItem)
	}

	return modelv3.Model.Render()
}
