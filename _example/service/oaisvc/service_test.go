package oaisvc

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizucue"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizuoai"
)

// sharedHandler is the service wired exactly the way main.go wires
// it: the whole repository CUE module into DI, every package's
// OpenAPI document merged into the served spec, then Initialize.
var sharedHandler http.Handler

func TestMain(m *testing.M) {
	module, err := mizucue.LoadModule(os.DirFS("../.."))
	if err != nil {
		panic(err)
	}
	srv := mizu.NewServer("test")
	var options []mizuoai.DocumentOption
	for importPath, doc := range module.MustOpenAPIs(nil) {
		options = append(options, mizuoai.WithDocumentPatch(importPath, doc))
	}
	if err := mizuoai.Initialize(srv, "test", options...); err != nil {
		panic(err)
	}
	mizudi.Register(func() (*mizu.Server, error) { return srv, nil })
	mizudi.Register(func() (mizucue.Module, error) { return module, nil })
	Initialize(nil)
	sharedHandler = srv.Handler()
	os.Exit(m.Run())
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (r *flushRecorder) Flush() {
	r.flushes++
}

func TestOaisvc_HandleEvents(t *testing.T) {
	recorder := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	request := httptest.NewRequest(http.MethodGet, "/oai/events", nil)

	(&Service{}).HandleEvents(recorder, request)

	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if recorder.flushes != 3 {
		t.Fatalf("flush count = %d, want 3", recorder.flushes)
	}
	if got, want := recorder.Body.String(), "data: connected\n\ndata: working\n\ndata: complete\n\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// The scrape endpoint decodes the header into the CUE-generated
// request type and validates it against the CUE definition: an
// empty key passes decoding but fails validation.
func TestOaisvc_HandleScrape(t *testing.T) {
	handler := sharedHandler

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/oai/scrape", nil)
	request.Header.Set("key", "magic-key-123")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), `"message":"Hello, magic-key-123"`) {
		t.Fatalf("scrape body = %s", recorder.Body)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/oai/scrape", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("scrape without key status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

// CUE owns the contract: amount must be positive, and the constraint
// lives only in schema.cue.
func TestOaisvc_HandleCreateOrder(t *testing.T) {
	handler := sharedHandler

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/oai/user/u1/order?timestamp=1",
		strings.NewReader(`{"id": "o1", "amount": 2}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("order status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), `"amount":1`) {
		t.Fatalf("order body = %s", recorder.Body)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/oai/user/u1/order",
		strings.NewReader(`{"id": "o1", "amount": -1}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("negative amount status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestOaisvc_HandlePackage(t *testing.T) {
	handler := sharedHandler

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/oai/package", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("package status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/gzip" {
		t.Fatalf("Content-Type = %q, want application/gzip", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="mizu-example.tar.gz"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got, want := recorder.Body.Bytes(), []byte{0x1f, 0x8b, 0x08}; !bytes.Equal(got, want) {
		t.Fatalf("package body = %v, want %v", got, want)
	}
}

// The served document is where the two worlds converge: the CUE
// operation fragment documents the raw download, and the CUE
// component shadows the reflected one — the doc comments exist only
// in schema.cue, so their presence proves the shadow.
func TestOaisvc_OpenAPIDocument(t *testing.T) {
	handler := sharedHandler

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("OpenAPI status = %d, want %d", recorder.Code, http.StatusOK)
	}
	document := recorder.Body.String()

	for _, want := range []string{
		// The CUE operation fragment, merged over the raw route.
		"operationId: downloadPackage",
		"application/gzip",
		"Content-Disposition",
		// The hand-built SSE operation.
		"operationId: streamEvents",
		"text/event-stream",
		// Components carry the canonical import-path name —
		// identical on the CUE and the reflection side.
		"example.com.mizu.service.oaisvc.CreateOrderResponse",
		// CUE-only descriptions prove the patch shadowed the
		// reflected component.
		"amount is the amount actually processed",
	} {
		if !strings.Contains(document, want) {
			t.Fatalf("OpenAPI document missing %q:\n%s", want, document)
		}
	}
}
