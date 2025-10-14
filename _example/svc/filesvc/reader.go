package filesvc

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
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
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

var (
	ErrNilStream       = errors.New("nil stream")
	ErrMissingBoundary = errors.New("missing boundary")
	ErrMismatchProto   = errors.New("mismatched proto")
)

type FileReader struct {
	hash   hash.Hash
	inner  io.Reader
	closer io.Closer
}

func NewFileReader(rx io.ReadCloser) *FileReader {
	hash := sha256.New()
	return &FileReader{inner: io.TeeReader(rx, hash), hash: hash, closer: rx}
}

func (r *FileReader) Checksum() string {
	return hex.EncodeToString(r.hash.Sum(nil))
}

func (r *FileReader) Read(p []byte) (int, error) {
	return r.inner.Read(p)
}

func (r *FileReader) Close() error {
	return r.closer.Close()
}

type HttpForm interface {
	proto.Message
	GetForm() *httpbody.HttpBody
}

type StreamForm[T HttpForm] interface {
	Msg() T
	Err() error
	Receive() bool
	Peer() connect.Peer
	Spec() connect.Spec
	RequestHeader() http.Header
	Conn() connect.StreamingHandlerConn
}

type FormReader interface {
	NextPart() (*multipart.Part, error)
}

type formReader[T HttpForm] struct {
	fileField string
	stream    StreamForm[T]
	message   proto.Message
	inner     *multipart.Reader
}

func NewFormReader[T HttpForm](fileField string, stream StreamForm[T], msg proto.Message) (FormReader, error) {
	if stream == nil {
		return nil, ErrNilStream
	}
	if ok := stream.Receive(); !ok {
		return nil, stream.Err()
	}

	if msg != nil {
		var temp T
		cur := msg.ProtoReflect()
		if name, want := cur.Descriptor().FullName(), temp.ProtoReflect().Descriptor().FullName(); name != want {
			return nil, ErrMismatchProto
		}
	}

	prologue := stream.Msg()
	contentType := prologue.GetForm().GetContentType()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		slog.Error("failed to parse media type", "content_type", contentType, "error", err)
		return nil, err
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, ErrMissingBoundary
	}

	rx := &formReader[T]{
		fileField: fileField,
		message:   msg,
		stream:    stream,
		inner:     multipart.NewReader(&streamReader[T]{stream, prologue.GetForm().GetData()}, boundary),
	}

	return rx, nil
}

func (r *formReader[T]) NextPart() (*multipart.Part, error) {
	part, err := r.inner.NextPart()
	if err != nil {
		return nil, err
	}

	if part.FormName() != r.fileField {
		trySetMessage(r.message, part)
	}

	return part, nil
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

func trySetMessage(msg proto.Message, rx *multipart.Part) {
	if msg == nil {
		return
	}

	bytes, err := io.ReadAll(rx)
	if err != nil {
		return
	}

	fd := msg.ProtoReflect().Descriptor().Fields().ByJSONName(rx.FormName())
	val, err := parse(fd, bytes)
	if err != nil {
		return
	}
	msg.ProtoReflect().Set(fd, val)
	_ = rx.Close()
}

// nolint: gocyclo
func parse(fd protoreflect.FieldDescriptor, raw []byte) (protoreflect.Value, error) {
	if fd == nil {
		return protoreflect.ValueOf(nil), fmt.Errorf("nil field")
	}

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
