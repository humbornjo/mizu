package compressmw

import (
	"compress/gzip"
	"fmt"
	"net/http"

	"golang.org/x/net/http/httpguts"
)

type Encoding interface {
	fmt.Stringer
	EncodingGzip | EncodingDeflate
}

func New[T Encoding](encoding T) func(http.Handler) http.Handler {
	encStr := encoding.String()
	switch enc := any(encoding).(type) {
	case EncodingGzip:
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !httpguts.HeaderValuesContainsToken(r.Header["Accept-Encoding"], encStr) {
					next.ServeHTTP(w, r)
					return
				}
				gw, _ := gzip.NewWriterLevel(w, enc.Level.Int())
				next.ServeHTTP(&wrapGzip{ResponseWriter: w, inner: gw}, r)
			})
		}
	default:
		panic("unreachable")
	}
}

// EncodingGzip -------------------------------------------------
type EncodingGzip struct {
	Level gzipLevel
}

func (EncodingGzip) String() string {
	return "gzip"
}

type gzipLevel int

const (
	GZIP_COMPRESSION_LEVEL_DEFAULT gzipLevel = iota
	GZIP_COMPRESSION_LEVEL_BEST
	GZIP_COMPRESSION_LEVEL_FAST
	GZIP_COMPRESSION_LEVEL_HUFFMAN
	GZIP_COMPRESSION_LEVEL_NONE
)

func (l gzipLevel) Int() int {
	switch l {
	case GZIP_COMPRESSION_LEVEL_NONE:
		return gzip.NoCompression
	case GZIP_COMPRESSION_LEVEL_DEFAULT:
		return gzip.DefaultCompression
	case GZIP_COMPRESSION_LEVEL_BEST:
		return gzip.BestCompression
	case GZIP_COMPRESSION_LEVEL_FAST:
		return gzip.BestSpeed
	case GZIP_COMPRESSION_LEVEL_HUFFMAN:
		return gzip.HuffmanOnly
	default:
		panic("unreachable")
	}
}

var _ http.Flusher = (*wrapGzip)(nil)

type wrapGzip struct {
	http.ResponseWriter

	doneHeader bool
	inner      *gzip.Writer
}

func (w *wrapGzip) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *wrapGzip) Write(b []byte) (int, error) {
	return w.ResponseWriter.Write(b)
}

func (w *wrapGzip) WriteHeader(code int) {
	if w.doneHeader {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	w.doneHeader = true
	defer w.ResponseWriter.WriteHeader(code)

	if w.Header().Get("Content-Encoding") != "" {
		return
	}

	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Del("Content-Length")
}

// Encoding deflate ---------------------------------------------
type EncodingDeflate struct {
	Level gzipLevel
}
