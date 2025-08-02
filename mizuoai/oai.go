package mizuoai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"text/template"

	"github.com/humbornjo/mizu"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

type Scope struct {
	path   string
	server *mizu.Server

	oaiConfig *oaiConfig
}

type ctxkey int

const (
	_CTXKEY_OAI_INITIALIZED ctxkey = iota
	_CTXKEY_OAI_CONFIG
)

// NewScope creates a new scope with the given path and options.
// openapi.json will be served at /{path}/openapi.json. HTML will
// be served at /{path}/openapi if enabled.
func NewScope(server *mizu.Server, path string, opts ...OaiOption) *Scope {
	config := new(oaiConfig)
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
		if once != nil && once.CompareAndSwap(false, true) {
			return
		}

		// value := ctx.Value(_CTXKEY_OAI_CONFIG)
		// if value == nil {
		// 	return
		// }
		//
		// oaiConfig, ok := value.(*oaiConfig)
		// if !ok {
		// 	return
		// }

		// Serve openapi.json
		srv.Get(path+"/openapi.json", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, path+"/openapi.json")
		})

		// Serve Swagger UI
		if enableDoc {
			srv.Get(path+"/openapi", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_ = _SWAGGER_UI_TEMPLATE.Execute(w, map[string]string{"Path": path + "/openapi.json"})
			})
		}
	})

	return &Scope{path: path, server: server, oaiConfig: config}
}

// Get registers a generic handler for GET requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Get[I any, O any](s *Scope, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...HandlerOption) {
	if s == nil {
		panic("scope is nil")
	}

	config := new(handlerConfig)
	enrichHandler[I, O](config)
	for _, opt := range opts {
		opt(config)
	}
	s.oaiConfig.handlers = append(s.oaiConfig.handlers, config)

	s.server.Get(pattern, handler[I, O](oaiHandler).genHandler())
}

// Rx represents the request side of an API endpoint. It provides
// access to the parsed request data and the original request
// context.
type Rx[T any] struct {
	r    *http.Request
	read func(*http.Request) *T
}

// Read returns the parsed input from the request. The parsing
// logic is generated based on the struct tags of the input type.
func (rx Rx[T]) Read() *T {
	return rx.read(rx.r)
}

// Context returns the context of the original request.
func (rx Rx[T]) Context() context.Context {
	return rx.r.Context()
}

// WithContext sets the context of the original request.
func (rx Rx[T]) WithContext(ctx context.Context) {
	_ = rx.r.WithContext(ctx)
}

// Request returns the original http.Request.
func (r Rx[T]) Request() *http.Request {
	return r.r
}

// Tx represents the response side of an API endpoint. It
// provides methods to write the response.
type Tx[T any] struct {
	w http.ResponseWriter
}

// Write writes the JSON-encoded output to the response writer.
// It also sets the Content-Type header to "application/json;
// charset=utf-8".
func (tx Tx[T]) Write(data *T) error {
	tx.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(tx.w).Encode(data)
}

// ResponseWriter returns the original http.ResponseWriter.
func (tx Tx[T]) ResponseWriter() http.ResponseWriter {
	return tx.w
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
		rx := Rx[I]{r: r, read: func(r *http.Request) *I {
			input := new(I)
			parser(r, input)
			return input
		}}
		// The response object (tx) wraps the original http.ResponseWriter.
		tx := Tx[O]{w: w}

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

func enrichHandler[I any, O any](config *handlerConfig) {
	input := new(I)
	valInput := reflect.ValueOf(input).Elem()
	typInput := valInput.Type()
	if typInput.Kind() != reflect.Struct {
		panic("input type must be a struct")
	}

	output := new(O)
	valOutput := reflect.ValueOf(output).Elem()
	typOutput := valOutput.Type()

	// Process input type (request parameters/body)
	for i := 0; i < typInput.NumField(); i++ {
		field := typInput.Field(i)

		// Check mizu tags first for parameter location
		if mizuTag, ok := field.Tag.Lookup("mizu"); ok {
			switch tag(mizuTag) {
			case _TAG_PATH, _TAG_QUERY, _TAG_HEADER:
				// Handle as parameter
				jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
				if jsonTag == "" || jsonTag == "-" {
					jsonTag = field.Name
				}

				param := &v3.Parameter{
					Name:        jsonTag,
					In:          string(tag(mizuTag)),
					Description: "Generated from struct field: " + field.Name,
					Schema:      createSchemaProxy(field.Type),
				}
				config.Parameters = append(config.Parameters, param)
			case _TAG_BODY:
				// Handle as request body
				if config.RequestBody == nil {
					config.RequestBody = &v3.RequestBody{
						Description: "Request body",
						Content:     orderedmap.New[string, *v3.MediaType](),
					}
				}
				config.RequestBody.Content.Set("application/json", &v3.MediaType{
					Schema: createSchemaProxy(field.Type),
				})
			case _TAG_FORM:
				// Handle as form data
				if config.RequestBody == nil {
					config.RequestBody = &v3.RequestBody{
						Description: "Form data",
						Content:     orderedmap.New[string, *v3.MediaType](),
					}
				}
				config.RequestBody.Content.Set("application/x-www-form-urlencoded", &v3.MediaType{
					Schema: createSchemaProxy(field.Type),
				})
			}
		} else {
			// Default to query parameter for simple fields
			jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
			if jsonTag == "" || jsonTag == "-" {
				jsonTag = field.Name
			}

			param := &v3.Parameter{
				Name:        jsonTag,
				In:          "query",
				Description: "Generated from struct field: " + field.Name,
				Schema:      createSchemaProxy(field.Type),
			}
			config.Parameters = append(config.Parameters, param)
		}
	}

	// Process output type (response body)
	if config.Responses == nil {
		config.Responses = &v3.Responses{
			Codes: orderedmap.New[string, *v3.Response](),
		}
	}

	response := &v3.Response{
		Description: "Successful response",
		Content:     orderedmap.New[string, *v3.MediaType](),
	}
	response.Content.Set("application/json", &v3.MediaType{
		Schema: createSchemaProxy(typOutput),
	})
	config.Responses.Codes.Set("200", response)
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

var _SWAGGER_UI_TEMPLATE = template.Must(template.New("swagger_ui").Parse(_SWAGGER_UI_TEMPLATE_CONTENT))

const _SWAGGER_UI_TEMPLATE_CONTENT = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="description" content="SwaggerUI" />
  <title>SwaggerUI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin></script>
<script>
  window.onload = () => {
    window.ui = SwaggerUIBundle({
      url: '{{ .Path }}',
      dom_id: '#swagger-ui',
    });
  };
</script>
</body>
</html>`
