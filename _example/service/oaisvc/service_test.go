package oaisvc

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuoai"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (r *flushRecorder) Flush() {
	r.flushes++
}

func TestOaisvc_HandleOaiEvents(t *testing.T) {
	recorder := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	request := httptest.NewRequest(http.MethodGet, "/oai/events", nil)

	HandleOaiEvents(recorder, request)

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

func TestOaisvc_CueBackedPackage(t *testing.T) {
	srv := mizu.NewServer("test")
	if err := mizuoai.Initialize(srv, "test"); err != nil {
		t.Fatal(err)
	}
	registerRoutes(srv)
	handler := srv.Handler()

	packageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(packageRecorder, httptest.NewRequest(http.MethodGet, "/oai/package", nil))
	if packageRecorder.Code != http.StatusOK {
		t.Fatalf("package status = %d, want %d", packageRecorder.Code, http.StatusOK)
	}
	if got := packageRecorder.Header().Get("Content-Type"); got != "application/gzip" {
		t.Fatalf("Content-Type = %q, want application/gzip", got)
	}
	if got := packageRecorder.Header().Get("Content-Disposition"); got != `attachment; filename="mizu-example.tar.gz"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got, want := packageRecorder.Body.Bytes(), []byte{0x1f, 0x8b, 0x08}; !bytes.Equal(got, want) {
		t.Fatalf("package body = %v, want %v", got, want)
	}

	documentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(documentRecorder, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if documentRecorder.Code != http.StatusOK {
		t.Fatalf("OpenAPI status = %d, want %d", documentRecorder.Code, http.StatusOK)
	}
	if strings.Contains(documentRecorder.Body.String(), "__") {
		t.Fatalf("rendered OpenAPI contains CUE hints: %s", documentRecorder.Body.String())
	}
	document, err := mizuoai.ParseOpenAPI(documentRecorder.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	item, ok := document.Model().Paths.PathItems.Get("/oai/package")
	if !ok || item.Get == nil {
		t.Fatalf("GET /oai/package missing from OpenAPI document")
	}
	operation := item.Get
	if operation.OperationId != "downloadPackage" {
		t.Fatalf("operationId = %q, want downloadPackage", operation.OperationId)
	}
	if !slices.Contains(operation.Tags, "package") {
		t.Fatalf("tags = %v, want package", operation.Tags)
	}
	if operation.Summary != "Download a CUE-documented package" {
		t.Fatalf("summary = %q", operation.Summary)
	}
	if operation.Description != "Streams a compressed example package using a CUE-owned transport contract." {
		t.Fatalf("description = %q", operation.Description)
	}
	response, ok := operation.Responses.Codes.Get("200")
	if !ok {
		t.Fatal("200 response missing")
	}
	if _, ok := response.Content.Get("application/gzip"); !ok {
		t.Fatal("application/gzip response content missing")
	}
	header, ok := response.Headers.Get("Content-Disposition")
	if !ok || !header.Required {
		t.Fatalf("Content-Disposition header = %#v", header)
	}
}
