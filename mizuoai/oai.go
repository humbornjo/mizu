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
	"text/template"

	"github.com/humbornjo/mizu"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
)

type Scope struct {
	path   string
	server *mizu.Server

	// TODO: This config is not yet used.
	oaiConfig *oaiConfig
}

// NewScope creates a new scope with the given path and options.
// openapi.json will be served at /{path}/openapi.json. HTML will
// be served at /{path}/openapi if enabled.
func NewScope(server *mizu.Server, path string, opts ...OaiOption) *Scope {
	// TODO: oaiConfig is not initialized from opts.
	return &Scope{
		path:   path,
		server: server,
	}
}

// Get registers a generic handler for GET requests. It uses
// reflection to parse request data into the input type `I` and
// generate OpenAPI documentation.
func Get[I any, O any](s *Scope, pattern string, oaiHandler func(Tx[O], Rx[I]), opts ...OaiOption) {
	input := new(I)
	valInput := reflect.ValueOf(input).Elem()
	typInput := valInput.Type()
	if typInput.Kind() != reflect.Struct {
		panic("input type must be a struct")
	}

	// TODO: handler-specific OaiOption are not used.
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
type parser[T any] []func(r *http.Request, val *T) []error

// genParser creates a parser for a given generic type T. It uses
// reflection to inspect the fields and tags of T to build a set
// of parsing functions for different parts of the request.
func genParser[T any]() parser[T] {
	input := new(T)
	val := reflect.ValueOf(input).Elem()
	typ := val.Type()

	hasBody := false
	hasForm := false
	p := parser[T]{}
	for i := range typ.NumField() {
		fieldTyp := typ.Field(i)
		if mizuTag, ok := fieldTyp.Tag.Lookup("mizu"); ok {
			switch tag(mizuTag) {
			case _TAG_PATH:
				if fieldTyp.Type.Kind() != reflect.Struct {
					panic("path must be a struct")
				}
				structPath := val.FieldByName(fieldTyp.Name)
				metaPath := genNotions(structPath, _TAG_PATH)
				if len(metaPath) == 0 {
					continue
				}
				p = append(p, func(r *http.Request, input *T) []error {
					errs := []error{}
					st := reflect.ValueOf(input).Elem().FieldByName(fieldTyp.Name)
					for _, meta := range metaPath {
						if val := r.PathValue(meta.identifier); val != "" {
							if err := setField(st.Field(meta.fieldNumber), strings.NewReader(val)); err != nil {
								errs = append(errs, fmt.Errorf("path param '%s': %w", meta.identifier, err))
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
				metaQuery := genNotions(structQuery, _TAG_QUERY)
				if len(metaQuery) == 0 {
					continue
				}
				p = append(p, func(r *http.Request, val *T) []error {
					errs := []error{}
					st := reflect.ValueOf(val).Elem().FieldByName(fieldTyp.Name)
					for _, meta := range metaQuery {
						if val := r.URL.Query().Get(meta.identifier); val != "" {
							if err := setField(st.Field(meta.fieldNumber), strings.NewReader(val)); err != nil {
								errs = append(errs, fmt.Errorf("query param '%s': %w", meta.identifier, err))
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
				metaHeader := genNotions(structHeader, _TAG_HEADER)
				if len(metaHeader) == 0 {
					continue
				}
				p = append(p, func(r *http.Request, val *T) []error {
					errs := []error{}
					st := reflect.ValueOf(val).Elem().FieldByName(fieldTyp.Name)
					for _, meta := range metaHeader {
						if val := r.Header.Get(meta.identifier); val != "" {
							if err := setField(st.Field(meta.fieldNumber), strings.NewReader(val)); err != nil {
								errs = append(errs, fmt.Errorf("header '%s': %w", meta.identifier, err))
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
				p = append(p, func(r *http.Request, val *T) []error {
					if err := setField(reflect.ValueOf(val).Elem().FieldByName(fieldTyp.Name), r.Body); err != nil {
						return []error{fmt.Errorf("body: %w", err)}
					}
					return nil
				})
			}
		}
	}
	return p
}

// handler is a generic type for user-provided API logic.
type handler[I any, O any] func(Tx[O], Rx[I])

// genHandler wraps the user-provided handler with request
// parsing logic.
func (h handler[I, O]) genHandler() http.HandlerFunc {
	// Pre-compile the parser for efficiency. This is safe to do once as the type I is known at compile time.
	parser := genParser[I]()

	// Return the actual http.HandlerFunc.
	return func(w http.ResponseWriter, r *http.Request) {
		// Lazily parse the request data.
		rx := Rx[I]{r: r, read: func(r *http.Request) *I {
			input := new(I)
			for _, p := range parser {
				p(r, input)
			}
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

// goTypeToSchema converts a Go type into a corresponding OpenAPI Schema.
// It handles basic types (string, int, float, bool), structs, and slices.
// For structs, it uses the `json` tag to determine property names.
func goTypeToSchema(typ reflect.Type) *base.SchemaProxy {
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
				continue // Skip fields without a json tag or explicitly ignored
			}

			fieldSchema := goTypeToSchema(field.Type)
			if fieldSchema != nil {
				schema.Properties.Set(jsonTag, fieldSchema)
			}
		}
	case reflect.Slice:
		schema.Type = []string{"array"}
		schema.Items = &base.DynamicValue[*base.SchemaProxy, bool]{
			A: goTypeToSchema(typ.Elem()),
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

