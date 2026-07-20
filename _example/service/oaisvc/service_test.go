package oaisvc

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
