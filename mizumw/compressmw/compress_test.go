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
