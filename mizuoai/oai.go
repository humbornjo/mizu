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

	oaiConfig *oaiConfig
}

// NewScope creates a new scope with the given path and options.
// openapi.json will be served at /{path}/openapi.json. HTML will
// be served at /{path}/openapi if enabled.
func NewScope(server *mizu.Server, path string, opts ...OaiOption) *Scope {

	return &Scope{
		path:   path,
		server: server,
	}
}

func Get[I any, O any](s *Scope, pattern string, handlerOai func(Tx[O], Rx[I]), opts ...OaiOption) {
	middleware, handler := handler[I, O](handlerOai).split()
	s.server.Use(middleware).Get(pattern, handler)
}

type Rx[T any] struct {
	r *http.Request
}

func (r Rx[T]) Request() *http.Request {
	return r.r
}

func (rx Rx[T]) Read() *T {
	input := new(T)
	val := reflect.ValueOf(input).Elem()
	typ := val.Type()

	if typ.Kind() != reflect.Struct {
		if rx.r.Body != nil && rx.r.Body != http.NoBody {
			if typ.Kind() == reflect.String {
				bodyBytes, err := io.ReadAll(rx.r.Body)
				if err == nil {
					val.SetString(string(bodyBytes))
				}
			} else {
				json.NewDecoder(rx.r.Body).Decode(input)
			}
		}
		return input
	}

	contentType := rx.r.Header.Get("Content-Type")
	isJSONBody := strings.Contains(contentType, "application/json")
	isForm := strings.Contains(contentType, "application/x-www-form-urlencoded") ||
		strings.Contains(contentType, "multipart/form-data")

	if isForm {
		rx.r.ParseForm()
	}

	for i := 0; i < typ.NumField(); i++ {
		fieldTyp := typ.Field(i)
		fieldVal := val.Field(i)

		if mizuTag, ok := fieldTyp.Tag.Lookup("mizu"); ok {
			if fieldVal.Kind() != reflect.Struct {
				continue
			}

			switch mizuTag {
			case "query":
				parseRequest(fieldVal, "query", func(key string) string {
					return rx.r.URL.Query().Get(key)
				})
			case "header":
				parseRequest(fieldVal, "header", func(key string) string {
					return rx.r.Header.Get(key)
				})
			case "form":
				if isForm {
					parseRequest(fieldVal, "form", func(key string) string {
						return rx.r.FormValue(key)
					})
				}
			case "path":
				parseRequest(fieldVal, "path", func(key string) string {
					return rx.r.PathValue(key)
				})
			case "body":
				if isJSONBody && rx.r.Body != nil && rx.r.Body != http.NoBody {
					if fieldVal.CanAddr() {
						json.NewDecoder(rx.r.Body).Decode(fieldVal.Addr().Interface())
					}
				}
			}
		}
	}

	return input
}

func (rx Rx[T]) Context() context.Context {
	return rx.r.Context()
}

type Tx[T any] struct {
	w http.ResponseWriter
}

func (tx Tx[T]) Write(data *T) error {
	return json.NewEncoder(tx.w).Encode(nil)
}

type ctxKey int

const (
	_CTXKEY_RX ctxKey = iota
	_CTXKEY_TX
)

type handler[I any, O any] func(Tx[O], Rx[I])

func (h handler[I, O]) split() (func(http.Handler) http.Handler, http.HandlerFunc) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		rx, tx := r.Context().Value(_CTXKEY_RX).(Rx[I]), r.Context().Value(_CTXKEY_TX).(Tx[O])
		h(tx, rx)
	}

	// The middleware should read the data from request into r.Context as T, and write the data to the response as O
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			rx, tx := Rx[I]{r}, Tx[O]{w}
			ctx = context.WithValue(ctx, _CTXKEY_RX, rx)
			ctx = context.WithValue(ctx, _CTXKEY_TX, tx)
			rr := r.WithContext(ctx)

			next.ServeHTTP(w, rr)
		})
	}

	return middleware, handler
}

func parseRequest(fieldVal reflect.Value, tagName string, getter func(string) string) {
	structType := fieldVal.Type()
	for i := 0; i < fieldVal.NumField(); i++ {
		field := structType.Field(i)
		if tag, ok := field.Tag.Lookup(tagName); ok {
			value := getter(tag)
			if value != "" {
				setField(fieldVal.Field(i), value)
			}
		}
	}
}

func setField(field reflect.Value, value string) error {
	if !field.CanSet() {
		return fmt.Errorf("cannot set field")
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			field.SetInt(intVal)
		} else {
			return err
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if uintVal, err := strconv.ParseUint(value, 10, 64); err == nil {
			field.SetUint(uintVal)
		} else {
			return err
		}
	case reflect.Bool:
		if boolVal, err := strconv.ParseBool(value); err == nil {
			field.SetBool(boolVal)
		} else {
			return err
		}
	case reflect.Float32, reflect.Float64:
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			field.SetFloat(floatVal)
		} else {
			return err
		}
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
