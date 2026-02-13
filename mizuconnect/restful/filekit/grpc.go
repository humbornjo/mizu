package filekit

import (
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"
)

var _ runtime.Marshaler = (*formMarshaler)(nil)
var _ runtime.Marshaler = (*fileMarshaler)(nil)

type fileMarshaler struct {
	runtime.HTTPBodyMarshaler
}

// NewFileMarshaler creates a new FileMarshaler
func NewFileMarshaler(marshalOpts protojson.MarshalOptions, unmarshalOpts protojson.UnmarshalOptions,
) runtime.Marshaler {
	return &fileMarshaler{
		HTTPBodyMarshaler: runtime.HTTPBodyMarshaler{
			Marshaler: &runtime.JSONPb{MarshalOptions: marshalOpts, UnmarshalOptions: unmarshalOpts},
		},
	}
}

func (o *fileMarshaler) Delimiter() []byte {
	return []byte("")
}

type formMarshaler struct {
	inner runtime.Marshaler
}

// NewFormMarshaler creates a new FormMarshaler, which transcode
// multipart/form-data to HttpForm interface
func NewFormMarshaler(marshalOpts protojson.MarshalOptions, unmarshalOpts protojson.UnmarshalOptions,
) runtime.Marshaler {
	return &formMarshaler{inner: &runtime.JSONPb{MarshalOptions: marshalOpts, UnmarshalOptions: unmarshalOpts}}
}

func (m *formMarshaler) ContentType(v any) string {
	return m.inner.ContentType(v)
}

func (m *formMarshaler) Marshal(v any) ([]byte, error) {
	return m.inner.Marshal(v)
}

func (m *formMarshaler) Unmarshal(data []byte, v any) error {
	return m.inner.Unmarshal(data, v)
}

func (m *formMarshaler) NewDecoder(r io.Reader) runtime.Decoder {
	return &formDecoder{r}
}

func (m *formMarshaler) NewEncoder(w io.Writer) runtime.Encoder {
	return m.inner.NewEncoder(w)
}

type formDecoder struct {
	r io.Reader
}

func (d *formDecoder) Decode(v any) error {
	if _, ok := v.(HttpForm); !ok {
		return fmt.Errorf("%T is not a valid type", v)
	}
	rv := reflect.ValueOf(v).Elem()

	// Assert form as `*httpbody.HttpBody`
	// _, ok := form.(*httpbody.HttpBody)
	form := rv.FieldByName("Form")
	form.Set(reflect.New(form.Type().Elem()))

	buf := bufferPool.Get()
	defer bufferPool.Put(buf)

	n, err := d.r.Read(buf[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n == 0 {
		return io.EOF
	}

	form.Elem().FieldByName("Data").SetBytes(buf[:n])
	return nil
}
