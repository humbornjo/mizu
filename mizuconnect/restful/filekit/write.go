package filekit

import (
	"bufio"
	"net/http"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/api/httpbody"
)

// StreamResponse represents a Connect RPC server stream that can
// send HttpForm messages. It embeds the standard Connect stream
// interface methods.
//
// INFO: This interface is equivalent to
// *connect.ServerStream[httpbody.HttpBody]. It is provided for
// test purposes.
type StreamResponse interface {
	Send(msg *httpbody.HttpBody) error
	Conn() connect.StreamingHandlerConn
	ResponseHeader() http.Header
	ResponseTrailer() http.Header
}

type Writer struct {
	writeBytes int64
	inner      *bufio.Writer
}

// NewWriter returns a new io.Writer that writes to the provided
// connect.ServerStream. Response heaser must be set before
// writing to the returned io.WriteCloser.
func NewWriter(stream StreamResponse, prologue *httpbody.HttpBody,
) (*Writer, error) {
	sw := &streamWriter{
		virgin:      true,
		inner:       stream,
		contentType: prologue.GetContentType(),
	}
	tx := &Writer{inner: bufio.NewWriterSize(sw, 64*1024)}

	data := prologue.GetData()
	if len(data) == 0 {
		return tx, nil
	}

	if _, err := tx.Write(data); err != nil {
		return nil, err
	}
	return tx, nil
}

// Write implements io.Writer. whick writes data to the
// underlying bufio.Writer
func (w *Writer) Write(p []byte) (int, error) {
	n, err := w.inner.Write(p)
	w.writeBytes += int64(n)
	return n, err
}

// Close calls bufio.Writer.Flush to ensure all data is written.
func (w *Writer) Close() error {
	return w.inner.Flush()
}

// WriteSize returns the total number of bytes written so far.
func (w *Writer) WriteSize() int64 {
	return w.writeBytes
}

type streamWriter struct {
	virgin      bool
	contentType string
	inner       StreamResponse
}

func (w *streamWriter) Write(p []byte) (n int, err error) {
	pLen := len(p)
	if !w.virgin {
		if err := w.inner.Send(&httpbody.HttpBody{Data: p}); err != nil {
			return 0, err
		}
		return pLen, nil
	}

	w.virgin = false
	if w.contentType == "" {
		sniffer := [512]byte{}
		n = copy(sniffer[:], p)
		w.contentType = http.DetectContentType(sniffer[:n])
	}

	if err := w.inner.Send(&httpbody.HttpBody{ContentType: w.contentType, Data: p}); err != nil {
		return 0, err
	}
	return pLen, nil
}
