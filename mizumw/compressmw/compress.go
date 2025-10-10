package compressmw

import (
	"compress/gzip"
	"net/http"
)

type Encoding interface {
	EncodingGzip
}

func New[T Encoding](encoding T) func(http.Handler) http.Handler {
	switch enc := any(encoding).(type) {
	case EncodingGzip:
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// TODO: Check if the client supports gzip
				gw, _ := gzip.NewWriterLevel(w, enc.Level.Level())
				w.Header().Set("Content-Encoding", "gzip")
				next.ServeHTTP(wrapGzip{ResponseWriter: w, inner: gw}, r)
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

type gzipLevel int

func (l gzipLevel) Level() int {
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

const (
	GZIP_COMPRESSION_LEVEL_DEFAULT gzipLevel = iota
	GZIP_COMPRESSION_LEVEL_BEST
	GZIP_COMPRESSION_LEVEL_FAST
	GZIP_COMPRESSION_LEVEL_HUFFMAN
	GZIP_COMPRESSION_LEVEL_NONE
)

type wrapGzip struct {
	http.ResponseWriter
	inner *gzip.Writer
}

func (w wrapGzip) Write(b []byte) (int, error) {
	return w.inner.Write(b)
}
