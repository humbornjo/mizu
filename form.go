package mizu

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

var ErrFileTooLarge = errors.New("file too large")

// FileReader wraps an io.ReadCloser with size limiting, SHA-256 checksum
// calculation, and MIME type detection.
type FileReader struct {
	readBytes  int64
	limitBytes int64

	large       bool
	hash        hash.Hash
	inner       io.Reader
	closer      io.Closer
	sniffSize   int
	mimeSniffer [512]byte
}

// FileReaderOption configures a FileReader.
type FileReaderOption func(*FileReader)

// WithFileLimitBytes sets the maximum number of bytes that can be read from
// the file. Files larger than this limit return ErrFileTooLarge.
func WithFileLimitBytes(limit int64) FileReaderOption {
	return func(r *FileReader) {
		r.limitBytes = limit
	}
}

// NewFileReader creates a streaming file reader that calculates a SHA-256
// checksum and detects the MIME type from the first 512 bytes.
func NewFileReader(rx io.ReadCloser, opts ...FileReaderOption) *FileReader {
	hash := sha256.New()
	reader := &FileReader{
		inner:  io.TeeReader(rx, hash),
		hash:   hash,
		closer: rx,
	}

	for _, opt := range opts {
		opt(reader)
	}

	if reader.limitBytes <= 0 {
		reader.limitBytes = math.MaxInt64
	}

	n, _ := reader.inner.Read(reader.mimeSniffer[:])
	if reader.sniffSize = n; n > 0 {
		reader.inner = io.MultiReader(bytes.NewReader(reader.mimeSniffer[:n]), reader.inner)
	}

	return reader
}

// Checksum returns the SHA-256 checksum of the data read so far as a hex
// string.
func (r *FileReader) Checksum() string {
	return hex.EncodeToString(r.hash.Sum(nil))
}

// Read reads data while tracking its size and enforcing the configured limit.
func (r *FileReader) Read(p []byte) (int, error) {
	if r.large {
		return 0, fmt.Errorf("%w: %d > %d", ErrFileTooLarge, r.readBytes, r.limitBytes)
	}

	nbyte, err := r.inner.Read(p)
	r.readBytes += int64(nbyte)

	if r.readBytes > r.limitBytes {
		r.large = true
		return nbyte, fmt.Errorf("%w: %d > %d", ErrFileTooLarge, r.readBytes, r.limitBytes)
	}
	return nbyte, err
}

// ContentType returns the MIME type detected from the first 512 bytes.
func (r *FileReader) ContentType() string {
	return http.DetectContentType(r.mimeSniffer[:r.sniffSize])
}

// MimeSniffer returns a copy of the bytes used for MIME detection.
func (r *FileReader) MimeSniffer() []byte {
	return slices.Clone(r.mimeSniffer[:r.sniffSize])
}

// ReadSize returns the total number of bytes read so far.
func (r *FileReader) ReadSize() int64 {
	return r.readBytes
}

// Close closes the underlying reader.
func (r *FileReader) Close() error {
	return r.closer.Close()
}

// FormReader streams multipart form parts and locates a configured file part.
type FormReader interface {
	// NextPart returns the next multipart form part.
	NextPart() (*multipart.Part, error)

	// File advances to the configured file field. The returned purge function
	// consumes the remaining parts.
	File() (*multipart.Part, func() error, error)

	// Close releases resources owned by the reader.
	Close()
}

// FormReaderOption configures a FormReader.
type FormReaderOption func(*formReader)

// WithFormFieldLimitBytes sets the maximum bytes read into each declared
// non-file field. The default is 4 KiB.
func WithFormFieldLimitBytes(limit int64) FormReaderOption {
	return func(r *formReader) {
		r.fieldLimitBytes = limit
	}
}

type formReader struct {
	fileField       string
	fieldLimitBytes int64
	body            io.ReadCloser
	inner           *multipart.Reader
	message         reflect.Value
	fields          map[string]*formField
	fieldOrder      []string
	fileSeen        bool
	closed          bool
	complete        bool
	completeErr     error
}

type formField struct {
	index    int
	name     string
	typ      reflect.Type
	required bool
	seen     int
}

// NewFormReader creates a typed multipart/form-data reader for an HTTP
// request. message must be a non-nil pointer to a struct. NextPart consumes
// and decodes declared non-file fields while leaving unknown parts untouched.
func NewFormReader[T any](
	fileField string, request *http.Request, message *T, opts ...FormReaderOption,
) (FormReader, error) {
	if fileField == "" {
		return nil, errors.New("form file field is required")
	}
	if request == nil || request.Body == nil {
		return nil, errors.New("form request body is required")
	}
	if message == nil {
		return nil, errors.New("form message is nil")
	}

	value := reflect.ValueOf(message).Elem()
	if value.Kind() != reflect.Struct {
		return nil, fmt.Errorf("form message must point to a struct, got %s", value.Type())
	}

	mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("parse form content type: %w", err)
	}
	if mediaType != "multipart/form-data" {
		return nil, fmt.Errorf("expected multipart/form-data, got %s", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("form boundary not found")
	}
	if err := validateFormBoundary(boundary); err != nil {
		return nil, fmt.Errorf("invalid form boundary: %w", err)
	}

	reader := &formReader{
		fileField:       fileField,
		fieldLimitBytes: 4 * 1024,
		body:            request.Body,
		inner:           multipart.NewReader(request.Body, boundary),
		message:         value,
		fields:          make(map[string]*formField),
	}
	for _, opt := range opts {
		opt(reader)
	}
	if reader.fieldLimitBytes <= 0 {
		return nil, fmt.Errorf("form field limit must be positive, got %d", reader.fieldLimitBytes)
	}

	for index := range value.NumField() {
		field := value.Type().Field(index)
		if field.PkgPath != "" {
			continue
		}
		name, ignored := formFieldName(field)
		if ignored {
			continue
		}
		if name == fileField {
			return nil, fmt.Errorf("form file field %q conflicts with message field", fileField)
		}
		if previous := reader.fields[name]; previous != nil {
			return nil, fmt.Errorf("duplicate form field name %q on %s and %s", name, previous.name, field.Name)
		}
		required := false
		if raw, ok := field.Tag.Lookup("required"); ok {
			required, err = strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("parse required tag on %s: %w", field.Name, err)
			}
		}
		reader.fields[name] = &formField{
			index: index, name: field.Name, typ: field.Type, required: required,
		}
		reader.fieldOrder = append(reader.fieldOrder, name)
	}

	return reader, nil
}

func validateFormBoundary(boundary string) error {
	if len(boundary) < 1 || len(boundary) > 70 {
		return errors.New("invalid boundary length")
	}
	end := len(boundary) - 1
	for index, char := range boundary {
		if 'A' <= char && char <= 'Z' || 'a' <= char && char <= 'z' || '0' <= char && char <= '9' {
			continue
		}
		switch char {
		case '\'', '(', ')', '+', '_', ',', '-', '.', '/', ':', '=', '?':
			continue
		case ' ':
			if index != end {
				continue
			}
		}
		return errors.New("invalid boundary character")
	}
	return nil
}

func formFieldName(field reflect.StructField) (string, bool) {
	if raw, ok := field.Tag.Lookup("form"); ok {
		name, _, _ := strings.Cut(raw, ",")
		if name == "-" {
			return "", true
		}
		if name != "" {
			return name, false
		}
	}
	if raw, ok := field.Tag.Lookup("json"); ok {
		name, _, _ := strings.Cut(raw, ",")
		if name == "-" {
			return "", true
		}
		if name != "" {
			return name, false
		}
	}
	return field.Name, false
}

func (r *formReader) NextPart() (*multipart.Part, error) {
	if r.closed {
		return nil, errors.New("form reader is closed")
	}
	part, err := r.inner.NextPart()
	if err != nil {
		if errors.Is(err, io.EOF) {
			if validateErr := r.completeFields(); validateErr != nil {
				return nil, validateErr
			}
		}
		return nil, err
	}

	name := part.FormName()
	if name == r.fileField {
		if r.fileSeen {
			_ = part.Close()
			return nil, fmt.Errorf("duplicate form file field %q", name)
		}
		r.fileSeen = true
		return part, nil
	}

	field := r.fields[name]
	if field == nil {
		return part, nil
	}
	if field.seen > 0 && !isRepeatedFormField(field.typ) {
		_ = part.Close()
		return nil, fmt.Errorf("duplicate form field %q", name)
	}

	readLimit := r.fieldLimitBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	raw, err := io.ReadAll(io.LimitReader(part, readLimit))
	closeErr := part.Close()
	if err != nil {
		return nil, fmt.Errorf("read form field %q: %w", name, err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close form field %q: %w", name, closeErr)
	}
	if int64(len(raw)) > r.fieldLimitBytes {
		return nil, fmt.Errorf("form field %q exceeds %d bytes", name, r.fieldLimitBytes)
	}
	if err := decodeFormField(r.message.Field(field.index), raw); err != nil {
		return nil, fmt.Errorf("decode form field %q: %w", name, err)
	}
	field.seen++
	return part, nil
}

func (r *formReader) File() (*multipart.Part, func() error, error) {
	purge := func() error {
		for {
			_, err := r.NextPart()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}

	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, purge, fmt.Errorf("form file field %q is required", r.fileField)
		}
		if err != nil {
			return nil, purge, err
		}
		if part.FormName() == r.fileField {
			return part, purge, nil
		}
	}
}

func (r *formReader) Close() {
	if r.closed {
		return
	}
	r.closed = true
	body := r.body
	r.body = nil
	r.inner = nil
	r.message = reflect.Value{}
	r.fields = nil
	r.fieldOrder = nil
	_ = body.Close()
}

func (r *formReader) completeFields() error {
	if r.complete {
		return r.completeErr
	}
	r.complete = true
	for _, name := range r.fieldOrder {
		field := r.fields[name]
		if field.required && field.seen == 0 {
			r.completeErr = fmt.Errorf("required form field %q is missing", name)
			return r.completeErr
		}
	}
	return nil
}

func isRepeatedFormField(typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ.Kind() == reflect.Slice && typ.Elem().Kind() != reflect.Uint8
}

func decodeFormField(target reflect.Value, raw []byte) error {
	typ := target.Type()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Slice && typ.Elem().Kind() != reflect.Uint8 {
		container := target
		for container.Kind() == reflect.Pointer {
			if container.IsNil() {
				container.Set(reflect.New(container.Type().Elem()))
			}
			container = container.Elem()
		}
		value, err := parseFormValue(container.Type().Elem(), raw)
		if err != nil {
			return err
		}
		container.Set(reflect.Append(container, value))
		return nil
	}

	value, err := parseFormValue(target.Type(), raw)
	if err != nil {
		return err
	}
	target.Set(value)
	return nil
}

func parseFormValue(typ reflect.Type, raw []byte) (reflect.Value, error) {
	if typ.Kind() == reflect.Pointer {
		value, err := parseFormValue(typ.Elem(), raw)
		if err != nil {
			return reflect.Value{}, err
		}
		pointer := reflect.New(typ.Elem())
		pointer.Elem().Set(value)
		return pointer, nil
	}

	pointer := reflect.New(typ)
	if pointer.Type().Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		if err := pointer.Interface().(encoding.TextUnmarshaler).UnmarshalText(raw); err != nil {
			return reflect.Value{}, err
		}
		return pointer.Elem(), nil
	}

	value := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.String:
		value.SetString(string(raw))
	case reflect.Bool:
		parsed, err := strconv.ParseBool(string(raw))
		if err != nil {
			return reflect.Value{}, err
		}
		value.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(string(raw), 10, typ.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		value.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(string(raw), 10, typ.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		value.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(string(raw), typ.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		value.SetFloat(parsed)
	case reflect.Slice:
		if typ.Elem().Kind() != reflect.Uint8 {
			return reflect.Value{}, fmt.Errorf("unsupported form field type %s", typ)
		}
		value.SetBytes(slices.Clone(raw))
	default:
		return reflect.Value{}, fmt.Errorf("unsupported form field type %s", typ)
	}
	return value, nil
}
