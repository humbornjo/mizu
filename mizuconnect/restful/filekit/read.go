package filekit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/api/httpbody"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	_ "google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var ErrFileTooLarge = errors.New("file too large")

// FileReader wraps an io.ReadCloser to provide file upload
// functionality with size limiting, checksum calculation, and MIME
// type detection. It tracks read bytes and enforces size limits while
// calculating SHA256 checksum.
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
// read from the file. Files larger than this limit will result in
// ErrFileTooLarge. Default math.MaxInt64 (no limit).
func WithFileLimitBytes(limit int64) FileReaderOption {
	return func(r *FileReader) {
		r.limitBytes = limit
	}
}

// NewFileReader creates a new FileReader that wraps the given
// ReadCloser. It calculates SHA256 checksum while reading and can
// enforce size limits. Options can be provided to configure behavior
// like size limits.
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

// Checksum returns the SHA256 checksum of the data read so far as a
// hex string.
func (r *FileReader) Checksum() string {
	return hex.EncodeToString(r.hash.Sum(nil))
}

// Read implements io.Reader. It reads data while tracking bytes read
// and calculating checksum. If size limit is exceeded, it returns
// ErrFileTooLarge.
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

// ContentType returns the detected MIME type of the file content
// based on the first 512 bytes read. Uses http.DetectContentType for
// detection.
func (r *FileReader) ContentType() string {
	return http.DetectContentType(r.mimeSniffer[:r.sniffSize])
}

// MimeSniffer returns the first up to 512 bytes read from the file.
// (Refer to http.DetectContentType for details.)
func (r *FileReader) MimeSniffer() []byte {
	return slices.Clone(r.mimeSniffer[:r.sniffSize])
}

// ReadSize returns the total number of bytes read so far.
func (r *FileReader) ReadSize() int64 {
	return r.readBytes
}

// Close closes the underlying ReadCloser.
func (r *FileReader) Close() error {
	return r.closer.Close()
}

// HttpForm represents a protobuf message that contains HTTP form data.
// It must implement proto.Message and provide access to HttpBody content.
type HttpForm interface {
	proto.Message
	GetForm() *httpbody.HttpBody
}

// StreamForm represents a Connect RPC client stream that can receive
// HttpForm messages. It embeds the standard Connect stream interface
// methods while ensuring the message type satisfies the HttpForm
// interface for HTTP body content handling.
type StreamForm[T HttpForm] interface {
	Msg() T
	Err() error
	Receive() bool
	Peer() connect.Peer
	Spec() connect.Spec
	RequestHeader() http.Header
	Conn() connect.StreamingHandlerConn
}

// FormReader provides an interface for reading multipart form parts
// from HTTP form data. It abstracts the multipart.Reader
// functionality for processing form uploads.
type FormReader interface {
	// NextPart returns the next multipart form part. It automatically
	// handles non-file fields by attempting to map them to the provided
	// proto.Message. File field data is returned as-is for processing.
	//
	// WARN: If msg in NewFormReader is not nil, all the part except
	// file will be automatically consumed and mapped to msg. Comsume
	// the part will trigger error on setting msg. If you want to
	// manually handle the part, pass a nil value to msg when creating
	// FormReader.
	NextPart() (part *multipart.Part, err error)

	// File returns the file part in the form. Fields after file part
	// can either be accessed with NextPart or be drained by calling the
	// purge function. This function internally calls NextPart until the
	// file part is found.
	//
	// purge internally calls NextPart and return nil on EOF. Thus msg
	// still will be filled if none nil (see comment of NextPart).
	File() (filePart *multipart.Part, purge func() error, err error)

	// Close put back the *bufio.Reader to the pool. It must be called
	// after the form reader is done.
	Close()
}

type formReader[T HttpForm] struct {
	fileField  string
	bufferSize int64
	stream     StreamForm[T]
	close      func()
	message    proto.Message
	inner      *multipart.Reader
	detect     func(protoreflect.MessageDescriptor, string) protoreflect.FieldDescriptor
}

type enumProtoDetectMode int

const (
	// MODE_PROTO_TEXT is the default mode for field detection.
	MODE_PROTO_TEXT enumProtoDetectMode = iota
	// MODE_PROTO_JSON uses JSON name for proto field detection.
	MODE_PROTO_JSON
	// MODE_PROTO_HYBRID uses Text name if available, if got nil, will
	// return result by JSON name.
	MODE_PROTO_HYBRID
)

type FormReaderOption[T HttpForm] func(*formReader[T])

// WithFormProtoMode sets the mode for proto field detection. Default
// is MODE_PROTO_TEXT.
func WithFormProtoMode[T HttpForm](mode enumProtoDetectMode) FormReaderOption[T] {
	return func(rx *formReader[T]) {
		switch mode {
		case MODE_PROTO_TEXT:
			rx.detect = func(md protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
				return md.Fields().ByTextName(name)
			}
		case MODE_PROTO_JSON:
			rx.detect = func(md protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
				return md.Fields().ByJSONName(name)
			}
		case MODE_PROTO_HYBRID:
			rx.detect = func(md protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
				nameText := md.Fields().ByTextName(name)
				if nameText == nil {
					return md.Fields().ByJSONName(name)
				}
				return nameText
			}
		}
	}
}

// WithFormFieldLimitBytes sets the maximum number of bytes that can
// be allocated for read the field other than file field. The
// exceeding bytes will be discarded.
func WithFormFieldLimitBytes[T HttpForm](limit int64) FormReaderOption[T] {
	return func(rx *formReader[T]) {
		rx.bufferSize = limit
	}
}

// NewFormReader creates a new FormReader for processing multipart
// form data from a Connect RPC stream. It validates the stream and
// message types, extracts the content type and boundary from the
// first HttpForm message, and sets up a multipart reader. The
// fileField parameter specifies which form field contains the file
// data, while other fields can be mapped to the provided proto.Message.
func NewFormReader[T HttpForm](fileField string, stream StreamForm[T], msg proto.Message, opts ...FormReaderOption[T],
) (FormReader, error) {
	if ok := stream.Receive(); !ok {
		return nil, stream.Err()
	}

	if msg != nil {
		var temp T
		cur := msg.ProtoReflect().Descriptor()
		if name, want := cur.FullName(), temp.ProtoReflect().Descriptor().FullName(); name != want {
			return nil, fmt.Errorf("expected proto %s, got proto %s", want, name)
		}
	}

	// gRPC-gateway compatibility
	contentType := stream.RequestHeader().Get("Grpcgateway-Content-Type")
	if contentType == "" {
		contentType = stream.RequestHeader().Get("Content-Type")
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("form boundary not found")
	}

	prologue := stream.Msg()
	sr := &streamReader[T]{stream, prologue.GetForm().GetData()}
	rxPool := readerPool.Get()
	rxPool.Reset(sr)
	rx := &formReader[T]{
		fileField:  fileField,
		bufferSize: 4 * 1024,
		message:    msg,
		stream:     stream,
		close: func() {
			rxPool.Reset(nil)
			readerPool.Put(rxPool)
		},
		inner: multipart.NewReader(rxPool, boundary),
		detect: func(md protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
			return md.Fields().ByTextName(name)
		},
	}

	for _, opt := range opts {
		opt(rx)
	}

	if _, ok := fieldPools[rx.bufferSize]; !ok {
		fieldMutex.Lock()
		defer fieldMutex.Unlock()
		fieldPools[rx.bufferSize] = newpool(func() []byte { return make([]byte, rx.bufferSize) })
	}

	return rx, nil
}

// NextPart returns the next multipart form part. It automatically
// handles non-file fields by attempting to map them to the provided
// proto.Message. File field data is returned as-is for processing.
//
// WARN: If msg in NewFormReader is not nil, all the part except file
// will be automatically consumed and mapped to msg. Comsume the part
// will trigger error on setting msg. If you want to manually handle
// the part, pass a nil value to msg when creating FormReader.
func (r *formReader[T]) NextPart() (*multipart.Part, error) {
	part, err := r.inner.NextPart()
	if err != nil {
		return nil, err
	}

	if part.FormName() != r.fileField {
		r.trySetMessage(r.message, part)
	}

	return part, nil
}

// File returns the file part in the form. Fields after file part can
// be accessed with NextPart. This function internally calls NextPart
// until the file part is found.
func (r *formReader[T]) File() (*multipart.Part, func() error, error) {
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

func (r *formReader[T]) Close() {
	r.close()
}

type streamReader[T HttpForm] struct {
	stream StreamForm[T]
	buffer []byte
}

func (r *streamReader[T]) Read(p []byte) (int, error) {
	if len(r.buffer) == 0 {
		return 0, io.EOF
	}

	pLen := len(p)
	nbyte := copy(p, r.buffer)
	r.buffer = r.buffer[nbyte:]

	var n int
	for nbyte < pLen {
		if !r.stream.Receive() {
			if err := r.stream.Err(); err != nil {
				return nbyte, err
			}
			return nbyte, io.EOF
		}
		msg := r.stream.Msg()
		r.buffer = msg.GetForm().GetData()
		n = copy(p[nbyte:], r.buffer)
		nbyte += n
	}
	r.buffer = r.buffer[n:]

	return nbyte, nil
}

func (r *formReader[T]) trySetMessage(msg proto.Message, rx *multipart.Part) {
	if msg == nil {
		return
	}
	fd := r.detect(msg.ProtoReflect().Descriptor(), rx.FormName())
	if fd == nil {
		return
	}

	fieldMutex.RLock()
	buffer := fieldPools[r.bufferSize].Get()
	fieldMutex.RUnlock()
	defer fieldPools[r.bufferSize].Put(buffer)
	n, err := rx.Read(buffer)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(rx, int64(n)))
	}

	val, err := parse(fd, buffer[:n])
	if err != nil {
		return
	}
	msg.ProtoReflect().Set(fd, val)
	_ = rx.Close()
}

// nolint: gocyclo
func parse(fd protoreflect.FieldDescriptor, raw []byte) (protoreflect.Value, error) {
	switch kind := fd.Kind(); kind {
	case protoreflect.BoolKind:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return protoreflect.ValueOf(nil), err
		}
		return protoreflect.ValueOfBool(b), nil

	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		var x int32
		if err := json.Unmarshal(raw, &x); err != nil {
			return protoreflect.ValueOf(nil), err
		}
		return protoreflect.ValueOfInt32(x), nil

	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		var x int64
		if err := json.Unmarshal(raw, &x); err != nil {
			return protoreflect.ValueOf(nil), err
		}
		return protoreflect.ValueOfInt64(x), nil

	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		var x uint32
		if err := json.Unmarshal(raw, &x); err != nil {
			return protoreflect.ValueOf(nil), err
		}
		return protoreflect.ValueOfUint32(x), nil

	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		var x uint64
		if err := json.Unmarshal(raw, &x); err != nil {
			return protoreflect.ValueOf(nil), err
		}
		return protoreflect.ValueOfUint64(x), nil

	case protoreflect.FloatKind:
		var x float32
		if err := json.Unmarshal(raw, &x); err != nil {
			return protoreflect.ValueOf(nil), err
		}
		return protoreflect.ValueOfFloat32(x), nil

	case protoreflect.DoubleKind:
		var x float64
		if err := json.Unmarshal(raw, &x); err != nil {
			return protoreflect.ValueOf(nil), err
		}
		return protoreflect.ValueOfFloat64(x), nil

	case protoreflect.StringKind:
		return protoreflect.ValueOfString(string(raw)), nil

	case protoreflect.BytesKind:
		enc := base64.StdEncoding
		if bytes.ContainsAny(raw, "-_") {
			enc = base64.URLEncoding
		}
		if len(raw)%4 != 0 {
			enc = enc.WithPadding(base64.NoPadding)
		}

		dst := make([]byte, enc.DecodedLen(len(raw)))
		n, err := enc.Decode(dst, raw)
		if err != nil {
			return protoreflect.ValueOf(nil), err
		}
		return protoreflect.ValueOfBytes(dst[:n]), nil

	case protoreflect.EnumKind:
		var x int32
		if err := json.Unmarshal(raw, &x); err == nil {
			return protoreflect.ValueOfEnum(protoreflect.EnumNumber(x)), nil
		}

		s := string(raw)
		if isNullValue(fd) && s == "null" {
			return protoreflect.ValueOfEnum(0), nil
		}

		enumVal := fd.Enum().Values().ByName(protoreflect.Name(s))
		if enumVal == nil {
			return protoreflect.ValueOf(nil), fmt.Errorf("unexpected enum %s", raw)
		}
		return protoreflect.ValueOfEnum(enumVal.Number()), nil

	case protoreflect.MessageKind:
		// Well known JSON scalars are decoded to message types.
		md := fd.Message()
		name := string(md.FullName())
		if strings.HasPrefix(name, "google.protobuf.") {
			switch md.FullName()[16:] {
			case "Timestamp":
				var msg timestamppb.Timestamp
				if err := protojson.Unmarshal(quote(raw), &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "Duration":
				var msg durationpb.Duration
				if err := protojson.Unmarshal(quote(raw), &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "BoolValue":
				var msg wrapperspb.BoolValue
				if err := protojson.Unmarshal(raw, &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "Int32Value":
				var msg wrapperspb.Int32Value
				if err := protojson.Unmarshal(raw, &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "Int64Value":
				var msg wrapperspb.Int64Value
				if err := protojson.Unmarshal(raw, &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "UInt32Value":
				var msg wrapperspb.UInt32Value
				if err := protojson.Unmarshal(raw, &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "UInt64Value":
				var msg wrapperspb.UInt64Value
				if err := protojson.Unmarshal(raw, &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "FloatValue":
				var msg wrapperspb.FloatValue
				if err := protojson.Unmarshal(raw, &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "DoubleValue":
				var msg wrapperspb.DoubleValue
				if err := protojson.Unmarshal(raw, &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "BytesValue":
				var msg wrapperspb.BytesValue
				if err := protojson.Unmarshal(quote(raw), &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "StringValue":
				var msg wrapperspb.StringValue
				if err := protojson.Unmarshal(quote(raw), &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			case "FieldMask":
				var msg fieldmaskpb.FieldMask
				if err := protojson.Unmarshal(quote(raw), &msg); err != nil {
					return protoreflect.ValueOf(nil), err
				}
				return protoreflect.ValueOfMessage(msg.ProtoReflect()), nil
			}
		}
		return protoreflect.ValueOf(nil), fmt.Errorf("unexpected message type %s", name)

	default:
		return protoreflect.ValueOf(nil), fmt.Errorf("unknown param type %s", kind)
	}
}

func quote(raw []byte) []byte {
	if n := len(raw); n > 0 && (raw[0] != '"' || raw[n-1] != '"') {
		raw = strconv.AppendQuote(raw[:0], string(raw))
	}
	return raw
}

func isNullValue(fd protoreflect.FieldDescriptor) bool {
	ed := fd.Enum()
	return ed != nil && ed.FullName() == "google.protobuf.NullValue"
}
