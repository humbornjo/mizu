package compressmw_test

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizumw/compressmw"
	"github.com/stretchr/testify/assert"
)

func sendTestRequest(handler http.Handler, method, path, acceptedEncodings string, chunked ...bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if acceptedEncodings != "" {
		req.Header.Set("Accept-Encoding", acceptedEncodings)
	}

	if len(chunked) > 0 && chunked[0] {
		// Set Transfer-Encoding header to simulate chunked request
		req.Header.Set("Transfer-Encoding", "chunked")
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestCompressMw_NoCompression(t *testing.T) {
	testCases := []struct {
		name              string
		acceptedEncodings string
		contentType       string
		content           string
		expectEncoding    string
	}{
		{
			name:              "accepted gziped content",
			acceptedEncodings: "gzip",
			contentType:       "text/html",
			content:           "<html><body>Hello</body></html>",
			expectEncoding:    "gzip",
		},
		{
			name:              "unsupported content type",
			acceptedEncodings: "gzip",
			contentType:       "application/octet-stream",
			content:           "binary data",
			expectEncoding:    "",
		},
		{
			name:              "chunked transfer encoding",
			acceptedEncodings: "gzip",
			contentType:       "application/octet-stream",
			content:           "binary data",
			expectEncoding:    "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := mizu.NewServer("test-server")
			srv.Use(compressmw.New()).Get("/test", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = fmt.Fprint(w, tc.content)
			})

			var rr *httptest.ResponseRecorder
			if strings.HasPrefix(tc.acceptedEncodings, "chunked") {
				rr = sendTestRequest(srv.Handler(), "GET", "/test", tc.acceptedEncodings, true)
			} else {
				rr = sendTestRequest(srv.Handler(), "GET", "/test", tc.acceptedEncodings)
			}

			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tc.expectEncoding, rr.Result().Header.Get("Content-Encoding"))

			// Verify content
			if tc.expectEncoding == "" {
				assert.Equal(t, tc.content, rr.Body.String())
			} else {
				gr, err := gzip.NewReader(rr.Body)
				assert.NoError(t, err)

				bytes, _ := io.ReadAll(gr)
				assert.Equal(t, tc.content, string(bytes))
			}
		})
	}
}

func TestCompressMw_CustomContentTypes(t *testing.T) {
	srv := mizu.NewServer("test-server")

	// Configure compressor to only compress specific content types
	srv.Use(compressmw.New(
		compressmw.WithContentTypes("application/*", "text/plain"),
	)).Get("/test", func(w http.ResponseWriter, r *http.Request) {
		contentType := r.URL.Query().Get("type")
		w.Header().Set("Content-Type", contentType)
		_, _ = fmt.Fprint(w, "Test content")
	})

	testCases := []struct {
		name           string
		contentType    string
		expectEncoding string
	}{
		{
			name:           "allowed json",
			contentType:    "application/json",
			expectEncoding: "gzip",
		},
		{
			name:           "allowed plain text",
			contentType:    "text/plain",
			expectEncoding: "gzip",
		},
		{
			name:           "not allowed html",
			contentType:    "text/html",
			expectEncoding: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", fmt.Sprintf("/test?type=%s", tc.contentType), nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tc.expectEncoding, rr.Result().Header.Get("Content-Encoding"))
		})
	}
}

func TestCompressMw_DisableEncoder(t *testing.T) {
	srv := mizu.NewServer("test-server")

	// Disable deflate encoder by setting it to nil
	srv.Use(compressmw.New(
		compressmw.WithOverrideDeflate(nil),
	)).Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, "Test content")
	})

	// Test that gzip still works
	rr := sendTestRequest(srv.Handler(), "GET", "/test", "gzip, deflate")
	assert.Equal(t, "gzip", rr.Header().Get("Content-Encoding"))

	// Test that deflate is not available
	rr = sendTestRequest(srv.Handler(), "GET", "/test", "deflate")
	assert.Equal(t, "", rr.Header().Get("Content-Encoding"))
}

func TestCompressMw_AlreadyCompressedContent(t *testing.T) {
	srv := mizu.NewServer("test-server")
	srv.Use(compressmw.New()).Get("/test", func(w http.ResponseWriter, r *http.Request) {
		// Content is already compressed
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, "Already compressed content")
	})

	rr := sendTestRequest(srv.Handler(), "GET", "/test", "gzip")
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gzip", rr.Result().Header.Get("Content-Encoding"))
	assert.Equal(t, "gzip", rr.Result().Header.Get("Content-Encoding"), "Should preserve existing encoding")
}

func TestCompressMw_EmptyContent(t *testing.T) {
	srv := mizu.NewServer("test-server")
	srv.Use(compressmw.New()).Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Don't write any content
		w.WriteHeader(http.StatusNoContent)
	})

	rr := sendTestRequest(srv.Handler(), "GET", "/test", "gzip")
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func varyTokens(h http.Header) []string {
	var tokens []string
	for _, v := range h.Values("Vary") {
		for part := range strings.SplitSeq(v, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				tokens = append(tokens, trimmed)
			}
		}
	}
	return tokens
}

func countVaryAcceptEncoding(h http.Header) int {
	count := 0
	for _, tok := range varyTokens(h) {
		if strings.EqualFold(tok, "Accept-Encoding") {
			count++
		}
	}
	return count
}

func TestCompressMw_VaryNonCompressed(t *testing.T) {
	testCases := []struct {
		name              string
		acceptedEncodings string
		setupHandler      func(w http.ResponseWriter)
		expectEncoding    string
		expectBody        string
		expectOriginVary  bool
	}{
		{
			name:              "no accept-encoding",
			acceptedEncodings: "",
			setupHandler: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = fmt.Fprint(w, "plain body")
			},
			expectEncoding: "",
			expectBody:     "plain body",
		},
		{
			name:              "unsupported accept-encoding",
			acceptedEncodings: "br",
			setupHandler: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = fmt.Fprint(w, "plain body")
			},
			expectEncoding: "",
			expectBody:     "plain body",
		},
		{
			name:              "non-compressible content type",
			acceptedEncodings: "gzip",
			setupHandler: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = fmt.Fprint(w, "binary data")
			},
			expectEncoding: "",
			expectBody:     "binary data",
		},
		{
			name:              "pre-encoded response",
			acceptedEncodings: "gzip",
			setupHandler: func(w http.ResponseWriter) {
				w.Header().Set("Content-Encoding", "br")
				w.Header().Set("Content-Type", "text/plain")
				_, _ = fmt.Fprint(w, "pre-encoded")
			},
			expectEncoding: "br",
			expectBody:     "pre-encoded",
		},
		{
			name:              "flush before write",
			acceptedEncodings: "gzip",
			setupHandler: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "text/plain")
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				_, _ = fmt.Fprint(w, "flushed body")
			},
			expectEncoding: "",
			expectBody:     "flushed body",
		},
		{
			name:              "existing vary accept-encoding not duplicated",
			acceptedEncodings: "gzip",
			setupHandler: func(w http.ResponseWriter) {
				w.Header().Set("Vary", "Origin, accept-encoding")
				w.Header().Set("Content-Type", "text/plain")
				_, _ = fmt.Fprint(w, "vary body")
			},
			expectEncoding:   "gzip",
			expectOriginVary: true,
		},
		{
			name:              "handler sets other vary header",
			acceptedEncodings: "gzip",
			setupHandler: func(w http.ResponseWriter) {
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Content-Type", "text/plain")
				_, _ = fmt.Fprint(w, "origin body")
			},
			expectEncoding:   "gzip",
			expectOriginVary: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := mizu.NewServer("test-server")
			srv.Use(compressmw.New()).Get("/test", func(w http.ResponseWriter, r *http.Request) {
				tc.setupHandler(w)
			})

			rr := sendTestRequest(srv.Handler(), "GET", "/test", tc.acceptedEncodings)
			res := rr.Result()
			defer res.Body.Close()

			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tc.expectEncoding, res.Header.Get("Content-Encoding"))
			assert.Equal(t, 1, countVaryAcceptEncoding(res.Header), "Vary should contain Accept-Encoding exactly once")
			if tc.expectOriginVary {
				assert.Contains(t, varyTokens(res.Header), "Origin")
			}
			if tc.expectBody != "" {
				assert.Equal(t, tc.expectBody, rr.Body.String())
			}
		})
	}
}

type cachedResponse struct {
	status int
	header http.Header
	body   []byte
	vary   []string
}

type sharedCache struct {
	next    http.Handler
	entries map[string][]cachedResponse
	hits    int
	misses  int
}

func newSharedCache(next http.Handler) *sharedCache {
	return &sharedCache{
		next:    next,
		entries: make(map[string][]cachedResponse),
	}
}

func (c *sharedCache) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlKey := r.URL.String()
	for _, entry := range c.entries[urlKey] {
		matched := true
		for _, hdr := range entry.vary {
			if r.Header.Get(hdr) != entry.header.Get("X-Req-"+hdr) {
				matched = false
				break
			}
		}
		if matched {
			c.hits++
			for k, vals := range entry.header {
				if strings.HasPrefix(k, "X-Req-") {
					continue
				}
				w.Header()[k] = append([]string(nil), vals...)
			}
			w.WriteHeader(entry.status)
			_, _ = w.Write(entry.body)
			return
		}
	}

	c.misses++
	rr := httptest.NewRecorder()
	c.next.ServeHTTP(rr, r)
	res := rr.Result()
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	storedHeader := res.Header.Clone()
	vary := varyTokens(res.Header)
	for _, hdr := range vary {
		storedHeader.Set("X-Req-"+hdr, r.Header.Get(hdr))
	}
	c.entries[urlKey] = append(c.entries[urlKey], cachedResponse{
		status: rr.Code,
		header: storedHeader,
		body:   body,
		vary:   vary,
	})

	for k, vals := range res.Header {
		w.Header()[k] = append([]string(nil), vals...)
	}
	w.WriteHeader(rr.Code)
	_, _ = w.Write(body)
}

func decodeBody(t *testing.T, res *http.Response, raw []byte) string {
	t.Helper()
	if res.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(strings.NewReader(string(raw)))
		assert.NoError(t, err)
		defer gr.Close()
		out, err := io.ReadAll(gr)
		assert.NoError(t, err)
		return string(out)
	}
	return string(raw)
}

func TestCompressMw_SharedCacheVary(t *testing.T) {
	const payload = "shared cache test payload"

	srv := mizu.NewServer("test-server")
	srv.Use(compressmw.New())
	srv.Get("/plain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, payload)
	})
	srv.Get("/pre-encoded", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "br")
		_, _ = fmt.Fprint(w, payload)
	})
	srv.Get("/flush-before-write", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, payload)
	})

	cache := newSharedCache(srv.Handler())

	// 1. Prime without Accept-Encoding, then request with gzip.
	rr1 := sendTestRequest(cache, "GET", "/plain", "")
	res1 := rr1.Result()
	defer res1.Body.Close()
	assert.Equal(t, 1, cache.misses)
	assert.Equal(t, 0, cache.hits)
	assert.Equal(t, "", res1.Header.Get("Content-Encoding"))
	assert.Equal(t, 1, countVaryAcceptEncoding(res1.Header))
	assert.Equal(t, payload, decodeBody(t, res1, rr1.Body.Bytes()))

	rr2 := sendTestRequest(cache, "GET", "/plain", "gzip, identity;q=0")
	res2 := rr2.Result()
	defer res2.Body.Close()
	assert.Equal(t, 2, cache.misses, "Vary: Accept-Encoding must prevent serving uncompressed cache entry to gzip client")
	assert.Equal(t, "gzip", res2.Header.Get("Content-Encoding"))
	assert.Equal(t, 1, countVaryAcceptEncoding(res2.Header))
	assert.Equal(t, payload, decodeBody(t, res2, rr2.Body.Bytes()))

	// Subsequent identical request should hit cache.
	rr3 := sendTestRequest(cache, "GET", "/plain", "gzip, identity;q=0")
	res3 := rr3.Result()
	defer res3.Body.Close()
	assert.Equal(t, 1, cache.hits)
	assert.Equal(t, "gzip", res3.Header.Get("Content-Encoding"))
	assert.Equal(t, payload, decodeBody(t, res3, rr3.Body.Bytes()))

	// 2. Control: reverse order (gzip first, then no Accept-Encoding) on a fresh URL.
	rr4 := sendTestRequest(cache, "GET", "/plain?rev=1", "gzip, identity;q=0")
	res4 := rr4.Result()
	defer res4.Body.Close()
	assert.Equal(t, "gzip", res4.Header.Get("Content-Encoding"))
	assert.Equal(t, 1, countVaryAcceptEncoding(res4.Header))

	rr5 := sendTestRequest(cache, "GET", "/plain?rev=1", "")
	res5 := rr5.Result()
	defer res5.Body.Close()
	assert.Equal(t, 4, cache.misses)
	assert.Equal(t, "", res5.Header.Get("Content-Encoding"))
	assert.Equal(t, 1, countVaryAcceptEncoding(res5.Header))
	assert.Equal(t, payload, decodeBody(t, res5, rr5.Body.Bytes()))

	// 3. Pre-encoded and flush-before-write handlers must also include Vary: Accept-Encoding.
	for _, path := range []string{"/pre-encoded", "/flush-before-write"} {
		missesBefore := cache.misses
		rrUnenc := sendTestRequest(cache, "GET", path, "")
		resUnenc := rrUnenc.Result()
		defer resUnenc.Body.Close()
		assert.Equal(t, 1, countVaryAcceptEncoding(resUnenc.Header), path)
		assert.Equal(t, payload, rrUnenc.Body.String(), path)

		rrGzip := sendTestRequest(cache, "GET", path, "gzip, identity;q=0")
		resGzip := rrGzip.Result()
		defer resGzip.Body.Close()
		assert.Equal(t, missesBefore+2, cache.misses, path)
		assert.Equal(t, 1, countVaryAcceptEncoding(resGzip.Header), path)
		assert.Equal(t, payload, rrGzip.Body.String(), path)
	}
}
