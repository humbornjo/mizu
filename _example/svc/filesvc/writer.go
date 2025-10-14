package filesvc

import (
	"io"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/api/httpbody"
)

type writer struct {
	inner connect.ServerStream[httpbody.HttpBody]
}

func NewWriter(stream connect.ServerStream[httpbody.HttpBody], prologue *httpbody.HttpBody) (io.Writer, error) {
	return &writer{inner: stream}, nil
}

func (w *writer) Write(p []byte) (n int, err error) {
	if err := w.inner.Send(&httpbody.HttpBody{Data: p}); err != nil {
		return 0, err
	}
	return len(p), nil
}
