package formx

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

// FileReader wraps an io.ReadCloser with size limiting, SHA-256
// checksum calculation, and MIME type detection.
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

// WithFileLimitBytes sets the maximum number of bytes that can be
// read from the file. Files larger than this limit return
// ErrFileTooLarge.
func WithFileLimitBytes(limit int64) FileReaderOption {
	return func(r *FileReader) {
		r.limitBytes = limit
	}
}

// NewFileReader creates a streaming file reader that calculates a
// SHA-256 checksum and detects the MIME type from the first 512 bytes.
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

// Checksum returns the SHA-256 checksum of the data read so far as a
// hex string.
func (r *FileReader) Checksum() string {
	return hex.EncodeToString(r.hash.Sum(nil))
}

// Read reads data while tracking its size and enforcing the configured
// limit.
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

// FormReader streams multipart form parts and locates a configured
// file part.
type FormReader interface {
	// NextPart returns the next multipart form part.
	NextPart() (*multipart.Part, error)

	// File advances to the configured file field. The returned purge
	// function consumes the remaining parts.
	File() (*multipart.Part, func() error, error)

	// Close releases resources owned by the reader.
	Close()
}

type formReader struct {
	fileField  string
	bufferSize int64
	message    reflect.Value
	fields     map[string]int
	buffer     []byte
	body       io.ReadCloser
	inner      *multipart.Reader
}

// FormReaderOption configures a FormReader.
type FormReaderOption func(*formReader)

// WithFormFieldLimitBytes sets the maximum number of bytes read for
// each declared non-file field. The exceeding bytes are discarded.
func WithFormFieldLimitBytes(limit int64) FormReaderOption {
	return func(r *formReader) {
		r.bufferSize = limit
	}
}

// NewFormReader creates a new FormReader for processing multipart
// form data from an HTTP request. The fileField parameter specifies
// which form field contains the file data, while other declared
// fields are mapped to message, which must be a non-nil pointer to a
// struct.
//
// WARN: If message is not nil, every part except the file field is
// consumed and mapped best-effort — unreadable or unconvertible parts
// leave the field untouched. To handle parts manually, pass a nil
// message.
func NewFormReader[T any](
	fileField string, request *http.Request, message *T, opts ...FormReaderOption,
) (FormReader, error) {
	if fileField == "" {
		return nil, errors.New("form file field is required")
	}
	if request == nil || request.Body == nil {
		return nil, errors.New("form request body is required")
	}

	_, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("form boundary not found")
	}

	rx := &formReader{
		fileField:  fileField,
		bufferSize: 4 * 1024,
		body:       request.Body,
		inner:      multipart.NewReader(request.Body, boundary),
	}

	if message != nil {
		value := reflect.ValueOf(message).Elem()
		if value.Kind() != reflect.Struct {
			return nil, fmt.Errorf("form message must point to a struct, got %s", value.Type())
		}
		rx.message = value
		rx.fields = make(map[string]int)
		for index := range value.NumField() {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				continue
			}
			name, ignored := formFieldName(field)
			if ignored {
				continue
			}
			rx.fields[name] = index
		}
	}

	for _, opt := range opts {
		opt(rx)
	}
	if rx.bufferSize <= 0 {
		return nil, fmt.Errorf("form field limit must be positive, got %d", rx.bufferSize)
	}
	rx.buffer = make([]byte, rx.bufferSize)

	return rx, nil
}

// NextPart returns the next multipart form part. It automatically
// handles declared non-file fields by mapping them to the message.
// The file field and undeclared parts are returned as-is for manual
// processing.
func (r *formReader) NextPart() (*multipart.Part, error) {
	part, err := r.inner.NextPart()
	if err != nil {
		return nil, err
	}

	if part.FormName() != r.fileField {
		r.trySetMessage(part)
	}

	return part, nil
}

// File returns the file part in the form. Fields after the file part
// can be accessed with NextPart. This function internally calls
// NextPart until the file part is found.
func (r *formReader) File() (*multipart.Part, func() error, error) {
	var fpart *multipart.Part
	var err error
	for {
		var part *multipart.Part
		part, err = r.NextPart()
		if err != nil {
			break
		}
		if part.FormName() == r.fileField {
			fpart = part
			break
		}
	}

	purge := func() error {
		for {
			_, err := r.NextPart()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		}
	}

	return fpart, purge, err
}

// Close closes the request body.
func (r *formReader) Close() {
	_ = r.body.Close()
}

func (r *formReader) trySetMessage(part *multipart.Part) {
	if !r.message.IsValid() {
		return
	}
	index, ok := r.fields[part.FormName()]
	if !ok {
		return
	}

	n, err := part.Read(r.buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	_, _ = io.Copy(io.Discard, part)

	value, err := parse(r.message.Field(index).Type(), r.buffer[:n])
	if err != nil {
		return
	}
	r.message.Field(index).Set(value)
	_ = part.Close()
}

// formFieldName resolves a struct field's form name: the form tag
// wins, then the json tag, then the Go field name. "-" in either tag
// ignores the field.
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

func parse(typ reflect.Type, raw []byte) (reflect.Value, error) {
	if typ.Kind() == reflect.Pointer {
		value, err := parse(typ.Elem(), raw)
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

	value := pointer.Elem()
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
