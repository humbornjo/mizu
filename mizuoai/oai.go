package mizuoai

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"reflect"
	"sync"
	"text/template"

	"github.com/humbornjo/mizu"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

var (
	//go:embed tmpl_stoplight.html
	_STOPLIGHT_UI_TEMPLATE_CONTENT string
	_STOPLIGHT_UI_TEMPLATE         = template.Must(template.New("oai_ui").Parse(_STOPLIGHT_UI_TEMPLATE_CONTENT))
)

type ctxkey int

const (
	_CTXKEY_OAI ctxkey = iota
)

// Initialize inject OpenAPI config into mizu.Server with the
// given path and options. openapi.json will be served at
// /{path}/openapi.json. HTML will be served at /{path}/openapi
// if enabled. {path} can be set using WithOaiServePath
func Initialize(srv *mizu.Server, title string, opts ...OaiOption) error {
	if title == "" {
		return errors.New("openapi spec title is required")
	}

	config := &oaiConfig{
		info: new(base.Info),
	}
	config.info.Title = title
	for _, opt := range opts {
		opt(config)
	}

	// Serve openapi.json
	fileName := "/openapi.yaml"
	contentType := "text/yaml"
	if config.enableJson {
		fileName = "/openapi.json"
		contentType = "application/json"
	}

	once := sync.Once{}
	mizu.Hook(srv, _CTXKEY_OAI, config, mizu.WithHookHandler(func(srv *mizu.Server) {
		once.Do(func() {
			content, err := config.render(config.enableJson)
			if err != nil {
				fmt.Printf("🚨 [ERROR] Failed to generate openapi.json: %s\n", err)
				return
			}
			srv.Get(path.Join(config.servePath, fileName), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType)
				_, _ = w.Write(content)
			})

			if !config.enableDocument {
				return
			}
			encoded, _ := json.Marshal(string(content))
			srv.Get(path.Join(config.servePath, "/openapi"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_ = _STOPLIGHT_UI_TEMPLATE.Execute(w, map[string]string{"Document": string(encoded)})
			})
		})
	}))

	return nil
}

// Path registers a new path in the OpenAPI spec. It can be used
// to set the path field that can't be accessed via Get, Post,
// etc.
//
// See: https://spec.openapis.org/oas/v3.0.4.html#path-item-object
func Path(server *mizu.Server, pattern string, opts ...PathOption) {
	config := &pathConfig{}
	for _, opt := range opts {
		opt(config)
	}

	oai := mizu.Hook[ctxkey, oaiConfig](server, _CTXKEY_OAI, nil)
	oai.paths.PathItems.Set(pattern, &config.PathItem)
}

// Rx represents the request side of an API endpoint. It provides
// access to the parsed request data and the original request
// context.
type Rx[T any] struct {
	*http.Request
	read func(*http.Request) (*T, error)
}

// Read returns the parsed input from the request. The parsing
// logic is generated based on the struct tags of the input type.
func (rx Rx[T]) MizuRead() (*T, error) {
	return rx.read(rx.Request)
}

// Tx represents the response side of an API endpoint. It
// provides methods to write the response.
type Tx[T any] struct {
	http.ResponseWriter
	write func(*T) error
}

// Write writes the JSON-encoded output to the response writer.
// It also sets the Content-Type header to "application/json".
func (tx Tx[T]) MizuWrite(data *T) error {
	return tx.write(data)
}

// tag represents the source of request data (e.g., path, body).
type tag string

func (t tag) String() string {
	return string(t)
}

const (
	_STRUCT_TAG_PATH   tag = "path"
	_STRUCT_TAG_QUERY  tag = "query"
	_STRUCT_TAG_HEADER tag = "header"
	_STRUCT_TAG_BODY   tag = "body"
	_STRUCT_TAG_FORM   tag = "form"
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
type decode[T any] func(r *http.Request, val *T) error

func (d *decode[T]) append(fn decode[T]) {
	if *d == nil {
		*d = fn
		return
	}

	old := *d
	*d = func(r *http.Request, val *T) error {
		errOld := old(r, val)
		if err := fn(r, val); err != nil {
			if errOld == nil {
				return err
			}
			return fmt.Errorf("%w; %w", err, errOld)
		}
		return errOld
	}
}

func (d *decode[T]) validate(tag tag, field *reflect.StructField) {
	switch tag {
	case _STRUCT_TAG_PATH, _STRUCT_TAG_QUERY, _STRUCT_TAG_HEADER:
		if field.Type.Kind() != reflect.Struct {
			panic("path must be a struct")
		}
	case _STRUCT_TAG_BODY, _STRUCT_TAG_FORM:
	default:
		panic("unreachable")
	}
}

func (d *decode[T]) retrieve(tag tag, r *http.Request, identifier string) string {
	switch tag {
	case _STRUCT_TAG_PATH:
		return r.PathValue(identifier)
	case _STRUCT_TAG_QUERY:
		return r.URL.Query().Get(identifier)
	case _STRUCT_TAG_HEADER:
		return r.Header.Get(identifier)
	default:
		panic("unreachable")
	}
}

// genDecode creates a parser for a given generic type T. It
// uses reflection to inspect the fields and tags of T to build a
// set of parsing functions for different parts of the request.
func genDecode[T any]() decode[T] {
	val := reflect.ValueOf(new(T)).Elem()
	typ := val.Type()
	hasBody, hasForm := false, false

	decoder := new(decode[T])
	for i := range typ.NumField() {
		fieldTyp := typ.Field(i)
		t, ok := fieldTyp.Tag.Lookup("mizu")
		if !ok {
			continue
		}
		mizuTag := tag(t)

		decoder.validate(mizuTag, &fieldTyp)
		switch mizuTag {
		case _STRUCT_TAG_BODY:
			hasBody = true
			decoder.append(func(r *http.Request, val *T) error {
				fieldBody := reflect.ValueOf(val).Elem().Field(i)
				if err := setStreamValue(fieldBody, r.Body, fieldBody.Kind()); err != nil {
					return fmt.Errorf("failed to decode body: %w", err)
				}
				return nil
			})
		case _STRUCT_TAG_FORM:
			hasForm = true
			fieldVal := val.FieldByName(fieldTyp.Name)
			notions := genNotions(fieldVal, mizuTag)
			decoder.append(func(r *http.Request, val *T) error {
				st := reflect.ValueOf(val).Elem().Field(i)
				rx, err := consFormReader(r.Body, r.Header.Get("Content-Type"))
				if err != nil {
					return fmt.Errorf("failed to read form: %w", err)
				}
				var part *multipart.Part
				for part, err = rx.NextPart(); err == nil; part, err = rx.NextPart() {
					for _, notion := range notions {
						if part.FormName() != notion.identifier {
							continue
						}
						f := st.Field(notion.fieldNumber)
						if err := setStreamValue(f, part, f.Kind()); err != nil {
							return fmt.Errorf("failed to decode form: %w", err)
						}
						break
					}
				}
				if errors.Is(err, io.EOF) {
					err = nil
				}
				return err
			})
		default:
			fieldVal := val.FieldByName(fieldTyp.Name)
			notions := genNotions(fieldVal, mizuTag)
			decoder.append(func(r *http.Request, val *T) error {
				var err error
				st := reflect.ValueOf(val).Elem().Field(i)
				for _, notion := range notions {
					v := decoder.retrieve(mizuTag, r, notion.identifier)
					f := st.Field(notion.fieldNumber)
					if e := setParamValue(f, v, f.Kind()); e != nil {
						if err == nil {
							err = fmt.Errorf("failed to decode %s: %w", notion.identifier, e)
						} else {
							err = fmt.Errorf("failed to decode %s: %w; %w", notion.identifier, e, err)
						}
					}
				}
				return err
			})
		}
	}
	if hasForm && hasBody {
		panic("cannot use both form and body")
	}
	if *decoder == nil {
		return func(r *http.Request, val *T) error { return nil }
	}

	return *decoder
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
		tx := Tx[O]{w, func(val *O) error { return encode(w, val) }}
		rx := Rx[I]{r, func(r *http.Request) (*I, error) {
			input := new(I)
			return input, decode(r, input)
		}}
		h(tx, rx)
	}
}
