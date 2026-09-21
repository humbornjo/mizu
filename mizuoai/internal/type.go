package internal

import "net/http"

// ResponseWriter wraps an http.ResponseWriter and records whether the
// handler took over the response by writing a body. On takeover the
// caller skips its own response writing. Header handling keeps the
// underlying writer's original behavior.
type ResponseWriter struct {
	takeover bool
	http.ResponseWriter
}

// WrapResponseWriter wraps w as a ResponseWriter that records
// takeover.
func WrapResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{ResponseWriter: w}
}

// Takeover reports whether a body has been written.
func (w *ResponseWriter) Takeover() bool {
	return w.takeover
}

func (w *ResponseWriter) Write(p []byte) (int, error) {
	w.takeover = true
	return w.ResponseWriter.Write(p)
}

// Flush forwards to the underlying writer when it is an http.Flusher,
// so handlers streaming through the wrapper (SSE and the like) keep
// their flush path.
func (w *ResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
