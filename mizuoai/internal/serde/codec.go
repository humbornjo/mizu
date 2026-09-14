package serde

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/humbornjo/mizu"
)

type TagType string

var _ fmt.Stringer = (*TagType)(nil)

const (
	TAG_PATH   TagType = "path"
	TAG_QUERY  TagType = "query"
	TAG_HEADER TagType = "header"
	TAG_BODY   TagType = "body"
	TAG_FORM   TagType = "form"
)

func (t TagType) String() string {
	return string(t)
}

// JsonField parses the json struct tag into name, options, and the
// ignored flag ("-").
func JsonField(field reflect.StructField) (string, []string) {
	value, ok := field.Tag.Lookup("json")
	if !ok {
		return "", nil
	}
	parts := strings.Split(value, ",")
	if parts[0] == "-" {
		return "", nil
	}
	return parts[0], parts[1:]
}

// Rx represents the request side of an API endpoint. It provides
// access to the parsed request data and the original request context.
type Rx[T any] struct {
	*http.Request
	decodeFunc func(*http.Request) (T, error)
}

// Read returns the parsed input from the request. The parsing logic
// is generated based on the struct tags of the input type.
func (rx Rx[T]) Xread() (T, error) {
	return rx.decodeFunc(rx.Request)
}

// Tx represents the response side of an API endpoint. It provides
// methods to write the response.
type Tx[T any] struct {
	http.ResponseWriter
	encodeFunc func(*T) error
}

// MizuWrite writes the JSON-encoded output to the response writer. It
// also sets the Content-Type header to "application/json".
func (tx Tx[T]) Xwrite(data *T) error {
	return tx.encodeFunc(data)
}

// MizuError writes an JSON-encoded error response to the response
// writer.
func (tx Tx[T]) Error(statusCode int, err error, details ...*mizu.ErrorDetail) error {
	return mizu.ResponseError(tx, mizu.NewError(statusCode, err, details...))
}

type Codec[I, O any] interface {
	Decode(*http.Request, *I) error

	Encode(http.ResponseWriter, *O) error

	Split(http.ResponseWriter, *http.Request) (Tx[O], Rx[I])
}

var _ Codec[any, any] = (*codec[any, any])(nil)

type codec[I, O any] struct {
	decodeFuncs []func(*http.Request, *I) error
}

func NewCodec[I, O any]() (Codec[I, O], error) {
	codec := &codec[I, O]{}
	if err := codec.Validate(); err != nil {
		return nil, err
	}

	typ := reflect.TypeOf((*I)(nil)).Elem()
	for i := range typ.NumField() {
		jsonTagValue, _ := JsonField(typ.Field(i))
		switch t := TagType(jsonTagValue); t {
		case TAG_BODY:
			codec.decodeFuncs = append(codec.decodeFuncs, codec.DecodeBodyFunc(i))
		case TAG_FORM:
			codec.decodeFuncs = append(codec.decodeFuncs, codec.DecodeFormFunc(i))
		case TAG_PATH, TAG_QUERY, TAG_HEADER:
			codec.decodeFuncs = append(codec.decodeFuncs, codec.DecodeParamsFunc(i, t))
		}
	}
	return codec, nil
}

func (c codec[I, O]) Decode(rx *http.Request, v *I) error {
	for _, decodef := range c.decodeFuncs {
		if err := decodef(rx, v); err != nil {
			return err
		}
	}
	return nil
}

func (c codec[I, O]) Encode(tx http.ResponseWriter, v *O) error {
	field := reflect.ValueOf(v).Elem()
	switch field.Kind() {
	case reflect.String:
		tx.Header().Set("Content-Type", "text/plain")
	default:
		tx.Header().Set("Content-Type", "application/json")
	}
	return json.NewEncoder(tx).Encode(v)
}

func (c codec[I, O]) Split(tx http.ResponseWriter, rx *http.Request) (Tx[O], Rx[I]) {
	encoder := func(v *O) error {
		return c.Encode(tx, v)
	}
	decoder := func(r *http.Request) (input I, err error) {
		return input, c.Decode(r, &input)
	}
	return Tx[O]{tx, encoder}, Rx[I]{rx, decoder}
}

func (c codec[I, O]) Validate() error {
	val := reflect.ValueOf(new(I)).Elem()
	hasBody, hasForm := false, false

	for field := range val.Type().Fields() {
		jsonTagValue, _ := JsonField(field)
		switch t := TagType(jsonTagValue); t {
		case TAG_BODY:
			hasBody = true
		case TAG_FORM:
			hasForm = true
		case TAG_PATH, TAG_QUERY, TAG_HEADER:
			typ := field.Type
			for typ.Kind() == reflect.Pointer {
				typ = typ.Elem()
			}
			if typ.Kind() != reflect.Struct {
				return errors.New(t.String() + " must be a struct")
			}
		}
	}

	if hasBody && hasForm {
		return errors.New("cannot have both body and form")
	}
	return nil
}

func (c codec[I, O]) DecodeBodyFunc(numField int) func(*http.Request, *I) error {
	return func(r *http.Request, val *I) error {
		field := reflect.ValueOf(val).Elem().Field(numField)
		return SetStreamFieldValue(field, r.Body)
	}
}

func (c codec[I, O]) DecodeFormFunc(numField int) func(*http.Request, *I) error {
	field := reflect.TypeOf((*I)(nil)).Elem().Field(numField)
	fieldlet := NewFieldlet(field.Type)

	return func(r *http.Request, val *I) error {
		st := reflect.ValueOf(val).Elem().Field(numField)
		for st.Kind() == reflect.Pointer {
			if st.IsNil() {
				st.Set(reflect.New(st.Type().Elem()))
			}
			st = st.Elem()
		}

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			return fmt.Errorf("failed to read form: %w", err)
		}

		if mediaType == "application/x-www-form-urlencoded" {
			if err := r.ParseForm(); err != nil {
				return fmt.Errorf("failed to read form: %w", err)
			}
			for num, name := range fieldlet.Fields() {
				values, ok := r.PostForm[name]
				if !ok || len(values) == 0 {
					continue
				}
				if err := SetFieldValue(st.Field(num), values[0]); err != nil {
					return fmt.Errorf("failed to decode form field %s: %w", name, err)
				}
			}
			return nil
		}

		boundary := params["boundary"]
		if !strings.HasPrefix(mediaType, "multipart/") || boundary == "" {
			return fmt.Errorf("failed to read form: unsupported content type %s", mediaType)
		}
		rx := multipart.NewReader(r.Body, boundary)
		for {
			part, err := rx.NextPart()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed to read form: %w", err)
			}
			num, _, ok := fieldlet.Find(part.FormName())
			if !ok {
				continue
			}
			if err := SetStreamFieldValue(st.Field(num), part); err != nil {
				return fmt.Errorf("failed to decode form field %s: %w", part.FormName(), err)
			}
		}
	}
}

func (c codec[I, O]) DecodeParamsFunc(numField int, tt TagType) func(r *http.Request, val *I) error {
	retrieve := func(rx *http.Request, name string) (string, bool) {
		switch tt {
		case TAG_PATH:
			return rx.PathValue(name), true
		case TAG_QUERY:
			values, ok := rx.URL.Query()[name]
			if !ok || len(values) == 0 {
				return "", false
			}
			return values[0], true
		case TAG_HEADER:
			// Replace all underline with hyphens for Canonical purposes
			values := rx.Header.Values(strings.ReplaceAll(name, "_", "-"))
			if len(values) == 0 {
				return "", false
			}
			return values[0], true
		default:
			panic("unreachable")
		}
	}

	field := reflect.TypeOf((*I)(nil)).Elem().Field(numField)
	fieldlet := NewFieldlet(field.Type)

	return func(r *http.Request, val *I) error {
		var err error
		st := reflect.ValueOf(val).Elem().Field(numField)
		for st.Kind() == reflect.Pointer {
			if st.IsNil() {
				st.Set(reflect.New(st.Type().Elem()))
			}
			st = st.Elem()
		}

		for num, name := range fieldlet.Fields() {
			value, present := retrieve(r, name)
			if !present {
				continue
			}
			f := st.Field(num)
			if e := SetFieldValue(f, value); e != nil {
				if err == nil {
					err = fmt.Errorf("failed to decode %s: %w", name, e)
				} else {
					err = fmt.Errorf("failed to decode %s: %w; %w", name, e, err)
				}
			}
		}
		return err
	}
}

// SetFieldValue sets a value to a reflect.Value based on its kind
func SetFieldValue(field reflect.Value, surface string) error {
	bitSize := func(kind reflect.Kind) int {
		switch kind {
		case reflect.Uint8, reflect.Int8:
			return 8
		case reflect.Uint16, reflect.Int16:
			return 16
		case reflect.Uint32, reflect.Int32, reflect.Float32:
			return 32
		case reflect.Uint, reflect.Int:
			return strconv.IntSize
		}
		return 64
	}

	kind := field.Kind()
	for kind == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
		kind = field.Kind()
	}

	switch kind {
	case reflect.String:
		field.SetString(surface)
	case reflect.Bool:
		boolValue, err := strconv.ParseBool(surface)
		if err != nil {
			return fmt.Errorf("cannot convert %s to bool: %w", surface, err)
		}
		field.SetBool(boolValue)
	case reflect.Struct, reflect.Map, reflect.Slice:
		if kind == reflect.Slice && field.Type().Elem().Kind() == reflect.Uint8 {
			// Keep raw-bytes semantics; json.Unmarshal would expect base64.
			field.SetBytes([]byte(surface))
			return nil
		}
		object := reflect.New(field.Type()).Interface()
		if err := json.Unmarshal([]byte(surface), &object); err != nil {
			return err
		}
		field.Set(reflect.ValueOf(object).Elem())
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intValue, err := strconv.ParseInt(surface, 10, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", surface, kind, err)
		}
		field.SetInt(intValue)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintValue, err := strconv.ParseUint(surface, 10, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", surface, kind, err)
		}
		field.SetUint(uintValue)
	case reflect.Float32, reflect.Float64:
		floatValue, err := strconv.ParseFloat(surface, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", surface, kind, err)
		}
		field.SetFloat(floatValue)
	default:
		return fmt.Errorf("unsupported type %s", kind)
	}
	return nil
}

func SetStreamFieldValue(field reflect.Value, stream io.ReadCloser) error {
	defer stream.Close() // nolint: errcheck

	kind := field.Kind()
	for kind == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
		kind = field.Kind()
	}
	switch kind {
	case reflect.Slice:
		if field.Type().Elem().Kind() != reflect.Uint8 {
			return fmt.Errorf("unsupported stream slice type %s", field.Type())
		}
		raw, err := io.ReadAll(stream)
		if err != nil {
			return err
		}
		field.SetBytes(raw)
		return nil
	case reflect.Struct:
		decoder := jsontext.NewDecoder(stream)
		object := reflect.New(field.Type()).Interface()
		if err := jsonv2.UnmarshalDecode(decoder, &object); err != nil {
			return err
		}
		field.Set(reflect.ValueOf(object).Elem())
		return nil
	default:
		raw, err := io.ReadAll(stream)
		if err != nil && errors.Is(err, io.EOF) {
			return err
		}
		return SetFieldValue(field, string(raw))
	}
}
