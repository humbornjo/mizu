package compressmw

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

var _DEFAULT_CONTENT_TYPES = []string{
	"text/html",
	"text/css",
	"text/plain",
	"text/javascript",
	"application/javascript",
	"application/x-javascript",
	"application/json",
	"application/atom+xml",
	"application/rss+xml",
	"image/svg+xml",
	"video/mp4",
	"video/webm",
}

type Encoder interface {
	fmt.Stringer
	serveNext(w http.ResponseWriter, r *http.Request, next http.Handler, rules rules)
}

var _ Encoder = EncoderGzip{}
var _ Encoder = EncoderDeflate{}

type config struct {
	rules      rules
	precedence []Encoder
}

func init() {
	fill(&_DEFAULT_CONFIG.rules)
}

type rules struct {
	AllowedTypes     map[string]struct{}
	AllowedWildcards map[string]struct{}
}

func (c config) clone() config {
	return config{
		rules:      c.rules,
		precedence: slices.Clone(c.precedence),
	}
}

var _DEFAULT_CONFIG = config{
	rules:      rules{},
	precedence: []Encoder{EncoderGzip{}, EncoderDeflate{}},
}

type Option func(*config)

func fill(rules *rules, contentTypes ...string) {
	allowedTypes := make(map[string]struct{})
	allowedWildcards := make(map[string]struct{})

	if len(contentTypes) == 0 {
		for _, ct := range _DEFAULT_CONTENT_TYPES {
			allowedTypes[ct] = struct{}{}
		}
		rules.AllowedTypes = allowedTypes
		return
	}

	for _, ct := range contentTypes {
		if strings.Contains(strings.TrimSuffix(ct, "/*"), "*") {
			panic(fmt.Sprintf("invalid content type %s", ct))
		}
		if !strings.HasSuffix(ct, "/*") {
			allowedTypes[ct] = struct{}{}
		} else {
			allowedWildcards[strings.TrimSuffix(ct, "/*")] = struct{}{}
		}
	}

	rules.AllowedTypes = allowedTypes
	rules.AllowedWildcards = allowedWildcards
}

func WithOverrideGzip(enc *EncoderGzip) Option {
	return func(c *config) {
		if enc == nil {
			c.precedence = slices.DeleteFunc(c.precedence, func(e Encoder) bool {
				return e.String() == "gzip"
			})
			return
		}

		for i, e := range c.precedence {
			if e.String() == enc.String() {
				c.precedence[i] = enc
			}
		}
	}
}

func WithOverrideDeflate(enc *EncoderDeflate) Option {
	return func(c *config) {
		if enc == nil {
			c.precedence = slices.DeleteFunc(c.precedence, func(e Encoder) bool {
				return e.String() == "deflate"
			})
			return
		}

		for i, e := range c.precedence {
			if e.String() == enc.String() {
				c.precedence[i] = enc
			}
		}
	}
}

func WithContentTypes(contentTypes ...string) Option {
	return func(c *config) {
		if len(contentTypes) == 0 {
			return
		}
		c.rules = rules{}
		fill(&c.rules, contentTypes...)
	}
}

func New(opts ...Option) func(http.Handler) http.Handler {
	config := _DEFAULT_CONFIG.clone()
	for _, opt := range opts {
		opt(&config)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Compress should not be applied to chunked messages, as the call to
			// Flush() will lead to broken compression data.
			transferHeader := r.Header.Get("Transfer-Encoding")
			if transferHeader == "chunked" {
				next.ServeHTTP(w, r)
				return
			}

			// Select the appropriate encoder according to the Accept-Encoding header
			acceptedHeader := r.Header.Get("Accept-Encoding")
			accepted := strings.Split(strings.ToLower(acceptedHeader), ",")
			for _, enc := range config.precedence {
				if !slices.Contains(accepted, enc.String()) {
					continue
				}

				enc.serveNext(w, r, next, config.rules)
				return
			}

			// Fallback to no compression
			next.ServeHTTP(w, r)
		})
	}
}

// EncoderGzip -------------------------------------------------

type EncoderGzip struct {
	Level gzipLevel
}

func (EncoderGzip) String() string {
	return "gzip"
}

func (e EncoderGzip) serveNext(w http.ResponseWriter, r *http.Request, next http.Handler, rules rules) {
	gw, _ := gzip.NewWriterLevel(w, e.Level.Int())
	ww := embed(w, gw, e, rules)
	defer ww.Close() // nolint: errcheck
	next.ServeHTTP(ww, r)
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

// EncoderDeflate -----------------------------------------------

type EncoderDeflate struct {
	Level deflateLevel
}

func (EncoderDeflate) String() string {
	return "deflate"
}

func (e EncoderDeflate) serveNext(w http.ResponseWriter, r *http.Request, next http.Handler, rules rules) {
	fw, _ := flate.NewWriter(w, e.Level.Int())
	next.ServeHTTP(embed(w, fw, e, rules), r)
}

type deflateLevel int

const (
	DEFLATE_COMPRESSION_LEVEL_DEFAULT deflateLevel = iota
	DEFLATE_COMPRESSION_LEVEL_BEST
	DEFLATE_COMPRESSION_LEVEL_FAST
	DEFLATE_COMPRESSION_LEVEL_HUFFMAN
	DEFLATE_COMPRESSION_LEVEL_NONE
)

func (l deflateLevel) Int() int {
	switch l {
	case DEFLATE_COMPRESSION_LEVEL_NONE:
		return flate.NoCompression
	case DEFLATE_COMPRESSION_LEVEL_DEFAULT:
		return flate.DefaultCompression
	case DEFLATE_COMPRESSION_LEVEL_BEST:
		return flate.BestCompression
	case DEFLATE_COMPRESSION_LEVEL_FAST:
		return flate.BestSpeed
	case DEFLATE_COMPRESSION_LEVEL_HUFFMAN:
		return flate.HuffmanOnly
	default:
		panic("unreachable")
	}
}
