package compressmw

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

var (
	_ io.Closer           = (*wrappedWriter)(nil)
	_ http.Pusher         = (*wrappedWriter)(nil)
	_ http.Flusher        = (*wrappedWriter)(nil)
	_ http.Hijacker       = (*wrappedWriter)(nil)
	_ http.ResponseWriter = (*wrappedWriter)(nil)
)

type compressFlusher interface {
	Flush() error
}

type wrappedWriter struct {
	http.ResponseWriter

	enable           bool
	doneHeader       bool
	inner            io.Writer
	encoding         string
	contentTypes     map[string]struct{}
	contentWildcards map[string]struct{}
}

func embed(w http.ResponseWriter, delegator io.Writer, enc Encoder, rules rules) *wrappedWriter {
	return &wrappedWriter{
		ResponseWriter:   w,
		inner:            delegator,
		encoding:         enc.String(),
		contentTypes:     rules.AllowedTypes,
		contentWildcards: rules.AllowedWildcards,
	}
}

func (w *wrappedWriter) Flush() {
	if flusher, ok := w.writer().(http.Flusher); ok {
		flusher.Flush()
	}

	// If the underlying writer has a compression flush signature,
	// call this Flush() method instead
	if f, ok := w.writer().(compressFlusher); ok {
		_ = f.Flush()

		// Also flush the underlying response writer
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func (w *wrappedWriter) Write(b []byte) (int, error) {
	if !w.doneHeader {
		w.WriteHeader(http.StatusOK)
	}

	return w.writer().Write(b)
}

func (w *wrappedWriter) Close() error {
	if c, ok := w.writer().(io.Closer); ok {
		return c.Close()
	}
	return errors.New("io.WriteCloser is unavailable on the writer")
}

func (w *wrappedWriter) WriteHeader(code int) {
	if w.doneHeader {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	w.doneHeader = true
	defer w.ResponseWriter.WriteHeader(code)

	if w.Header().Get("Content-Encoding") != "" {
		return
	}

	if w.enable = w.compressible(); !w.enable {
		return
	}

	w.Header().Set("Content-Encoding", w.encoding)
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Del("Content-Length")
}

func (w *wrappedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.writer().(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("http.Hijacker is unavailable on the writer")
}

func (w *wrappedWriter) Push(target string, opts *http.PushOptions) error {
	if ps, ok := w.writer().(http.Pusher); ok {
		return ps.Push(target, opts)
	}
	return errors.New("http.Pusher is unavailable on the writer")
}

func (w *wrappedWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *wrappedWriter) writer() io.Writer {
	if w.enable {
		return w.inner
	}
	return w.ResponseWriter
}

func (w *wrappedWriter) compressible() bool {
	// Parse the first part of the Content-Type response header.
	contentType := w.Header().Get("Content-Type")
	contentType, _, _ = strings.Cut(contentType, ";")

	if _, ok := w.contentTypes[contentType]; ok {
		return true
	}
	if contentType, _, hadSlash := strings.Cut(contentType, "/"); hadSlash {
		_, ok := w.contentWildcards[contentType]
		return ok
	}
	return false
}
