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

	"github.com/humbornjo/mizu"
)

type Option func(*config)

type config struct {
	deprecated  bool
	tags        []string
	summary     string
	description string
}

func WithDeprecated() Option {
	return func(c *config) {
		c.deprecated = true
	}
}

func WithTags(tags ...string) Option {
	return func(c *config) {
		c.tags = tags
	}
}

func WithSummary(summary string) Option {
	return func(c *config) {
		c.summary = summary
	}
}

func WithDescription(description string) Option {
	return func(c *config) {
		c.description = description
	}
}

func Get[I any, O any](s *mizu.Server, pattern string, handlerOai func(Tx[O], Rx[I]), opts ...Option) {
	middleware, handler := handler[I, O](handlerOai).split()
	s.Use(middleware).Get(pattern, handler)
}

type Rx[T any] struct {
	r *http.Request
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
