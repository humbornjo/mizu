package mizuoai

import (
	"cmp"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"slices"
	"strconv"
)

type encoder[T any] func(http.ResponseWriter, *T) error

func newEncoder[T any]() encoder[T] {
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

func (e encoder[T]) encode(w http.ResponseWriter, val *T) error {
	return e(w, val)
}

// fieldlet holds metadata about a struct field to be parsed from a
// request.
type (
	fieldBrief struct {
		index int
		name  string
	}
	fieldlet []fieldBrief
)

func newFieldlet(val reflect.Value, tagKey mizutag) fieldlet {
	fieldlet := fieldlet(make(fieldlet, 0))
	for i := range val.Type().NumField() {
		field := val.Type().Field(i)
		tagVal := field.Tag.Get(tagKey.String())
		if tagVal == "" {
			panic("empty tag value from: " + fmt.Sprintf("%+v", field))
		}
		fieldlet = append(fieldlet, fieldBrief{i, tagVal})
	}
	slices.SortFunc(fieldlet, func(a, b fieldBrief) int { return cmp.Compare(a.name, b.name) })
	return fieldlet
}

func (fl fieldlet) find(fieldName string) (fieldBrief, bool) {
	idx, ok := slices.BinarySearchFunc(fl, fieldName, func(fb fieldBrief, name string) int {
		return cmp.Compare(fb.name, name)
	})
	if ok {
		return fl[idx], true
	}
	return fieldBrief{}, false
}

// decoder is a collection of functions that perform parsing of an
// http.Request into a target struct.
type decoder[T any] func(r *http.Request, val *T) error

func (d *decoder[T]) append(fn decoder[T]) {
	if *d == nil {
		*d = fn
		return
	}

	old := *d
	*d = func(r *http.Request, val *T) error {
		if err := old(r, val); err != nil {
			return err
		}
		if err := fn(r, val); err != nil {
			return err
		}
		return nil
	}
}

func (d decoder[T]) decode(r *http.Request, val *T) error {
	return d(r, val)
}

func decode_params[T any](tag mizutag, idx int, fieldlet fieldlet) func(r *http.Request, val *T) error {
	retrieve := func(tag mizutag, r *http.Request, identifier string) string {
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

	return func(r *http.Request, val *T) error {
		var err error
		st := reflect.ValueOf(val).Elem().Field(idx)
		for _, brief := range fieldlet {
			v := retrieve(tag, r, brief.name)
			f := st.Field(brief.index)
			if e := setParamValue(f, v, f.Kind()); e != nil {
				if err == nil {
					err = fmt.Errorf("failed to decode %s: %w", brief.name, e)
				} else {
					err = fmt.Errorf("failed to decode %s: %w; %w", brief.name, e, err)
				}
			}
		}
		return err
	}
}

func decode_body[T any](idx int, _ fieldlet) func(r *http.Request, val *T) error {
	return func(r *http.Request, val *T) error {
		fieldBody := reflect.ValueOf(val).Elem().Field(idx)
		if err := setStreamValue(fieldBody, r.Body, fieldBody.Kind()); err != nil {
			return fmt.Errorf("failed to decode body: %w", err)
		}
		return nil
	}
}

func decode_form[T any](idx int, fieldlet fieldlet) func(r *http.Request, val *T) error {
	return func(r *http.Request, parentVal *T) error {
		st := reflect.ValueOf(parentVal).Elem().Field(idx)
		rx, err := consFormReader(r.Body, r.Header.Get("Content-Type"))
		if err != nil {
			return fmt.Errorf("failed to read form: %w", err)
		}
		var part *multipart.Part
		for part, err = rx.NextPart(); err == nil; part, err = rx.NextPart() {
			brief, ok := fieldlet.find(part.FormName())
			if !ok {
				continue
			}
			f := st.Field(brief.index)
			if err := setStreamValue(f, part, f.Kind()); err != nil {
				return fmt.Errorf("failed to decode form: %w", err)
			}
		}
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return err
	}
}

func newDecoder[T any]() decoder[T] {
	validate := func(tag mizutag, field *reflect.StructField) {
		switch tag {
		case _STRUCT_TAG_PATH, _STRUCT_TAG_QUERY, _STRUCT_TAG_HEADER:
			if field.Type.Kind() != reflect.Struct {
				panic("path must be a struct")
			}
		case _STRUCT_TAG_BODY, _STRUCT_TAG_FORM:
		default:
			panic("unknown mizuoai tag: " + tag.String())
		}
	}

	val := reflect.ValueOf(new(T)).Elem()
	typ := val.Type()
	hasBody, hasForm := false, false

	decoder := new(decoder[T])
	for i := range typ.NumField() {
		fieldTyp := typ.Field(i)
		t, ok := fieldTyp.Tag.Lookup("mizu")
		if !ok {
			continue
		}
		mizuTag := mizutag(t)
		validate(mizuTag, &fieldTyp)

		switch mizuTag {
		case _STRUCT_TAG_BODY:
			hasBody = true
			decoder.append(decode_body[T](i, fieldlet{}))

		case _STRUCT_TAG_FORM:
			hasForm = true
			fieldVal := val.FieldByName(fieldTyp.Name)
			fieldlet := newFieldlet(fieldVal, mizuTag)
			decoder.append(decode_form[T](i, fieldlet))

		default:
			fieldVal := val.FieldByName(fieldTyp.Name)
			fieldlet := newFieldlet(fieldVal, mizuTag)
			decoder.append(decode_params[T](mizuTag, i, fieldlet))
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

// setStreamValue sets a value to a reflect.Struct using jsonv2 decoder
func setStreamValue(value reflect.Value, stream io.ReadCloser, kind reflect.Kind) error {
	defer stream.Close() // nolint: errcheck
	switch kind {
	case reflect.Struct:
		decoder := jsontext.NewDecoder(stream)
		object := reflect.New(value.Type()).Interface()
		if err := jsonv2.UnmarshalDecode(decoder, &object); err != nil {
			return err
		}
		value.Set(reflect.ValueOf(object).Elem())
		return nil
	default:
		raw, err := io.ReadAll(stream)
		if err != nil && errors.Is(err, io.EOF) {
			return err
		}
		return setParamValue(value, string(raw), kind)
	}
}

// setParamValue sets a value to a reflect.Value based on its kind
func setParamValue(value reflect.Value, paramValue string, kind reflect.Kind) error {
	switch kind {
	case reflect.String:
		value.SetString(paramValue)
	case reflect.Bool:
		boolValue, err := strconv.ParseBool(paramValue)
		if err != nil {
			return fmt.Errorf("cannot convert %s to bool: %w", paramValue, err)
		}
		value.SetBool(boolValue)
	case reflect.Struct:
		object := reflect.New(value.Type()).Interface()
		if err := json.Unmarshal([]byte(paramValue), &object); err != nil {
			return err
		}
		value.Set(reflect.ValueOf(object).Elem())
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intValue, err := strconv.ParseInt(paramValue, 10, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", paramValue, kind, err)
		}
		value.SetInt(intValue)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintValue, err := strconv.ParseUint(paramValue, 10, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", paramValue, kind, err)
		}
		value.SetUint(uintValue)
	case reflect.Float32, reflect.Float64:
		floatValue, err := strconv.ParseFloat(paramValue, bitSize(kind))
		if err != nil {
			return fmt.Errorf("cannot convert %s to %s: %w", paramValue, kind, err)
		}
		value.SetFloat(floatValue)
	default:
		return fmt.Errorf("unsupported type %s", kind)
	}
	return nil
}
