package mizu_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/stretchr/testify/assert"
)

func TestMizu_NewServer(t *testing.T) {
	testCases := []struct {
		name        string
		serviceName string
		opts        []mizu.Option
	}{
		{
			name:        "default server",
			serviceName: "test-service",
			opts:        nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := mizu.NewServer(tc.serviceName, tc.opts...)

			if srv == nil {
				t.Fatal("NewServer returned nil")
			}

			// Test that the server can handle basic operations
			srv.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("test"))
			})

			handler := srv.Handler()
			if handler == nil {
				t.Fatal("Handler() returned nil")
			}

			// Test basic request handling
			req := httptest.NewRequest("GET", "/test", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, "test", rr.Body.String())
		})
	}
}

func TestMizu_WithPrometheusMetrics(t *testing.T) {
	srv := mizu.NewServer("metrics-test", mizu.WithPrometheusMetrics())

	handler := srv.Handler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check that it returns Prometheus format metrics
	body := rr.Body.String()
	if !strings.Contains(body, "# HELP") || !strings.Contains(body, "# TYPE") {
		t.Error("metrics endpoint doesn't return Prometheus format")
	}
}

func TestMizu_WithWizardHandleReadiness(t *testing.T) {
	testCases := []struct {
		name               string
		server             *mizu.Server
		endpoint           string
		expectedBody       string
		expectedStatusCode int
	}{
		{
			name:               "default healthz endpoint",
			server:             mizu.NewServer("default-healthz"),
			endpoint:           "/healthz",
			expectedBody:       "OK",
			expectedStatusCode: http.StatusOK,
		},
		{
			name: "custom readiness handler",
			server: mizu.NewServer(
				"custom-readiness",
				mizu.WithWizardHandleReadiness(
					"GET /healthz",
					func(isShuttingDown *atomic.Bool) http.HandlerFunc {
						return func(w http.ResponseWriter, r *http.Request) {
							if isShuttingDown.Load() {
								http.Error(w, "Custom: Shutting down", http.StatusServiceUnavailable)
								return
							}
							_, _ = w.Write([]byte("Custom: Ready"))
						}
					}),
			),
			endpoint:           "/healthz",
			expectedBody:       "Custom: Ready",
			expectedStatusCode: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.endpoint, http.NoBody)
			tc.server.Handler().ServeHTTP(rr, req)
			assert.Equal(t, tc.expectedStatusCode, rr.Code)
			assert.Equal(t, tc.expectedBody, strings.TrimSpace(rr.Body.String()))
		})
	}
}

func TestMizu_WithProfilingHandlers(t *testing.T) {
	srv := mizu.NewServer("profiling-test", mizu.WithProfilingHandlers())
	handler := srv.Handler()

	// Test that profiling endpoints are registered
	profilingEndpoints := []struct {
		endpoint string
		params   string
	}{
		{"/debug/pprof/", ""},
		{"/debug/pprof/trace", "?seconds=1"},
		{"/debug/pprof/symbol", ""},
		{"/debug/pprof/cmdline", ""},
		{"/debug/pprof/profile", "?seconds=1"},
		{"/debug/pprof/heap", ""},
		{"/debug/pprof/block", ""},
		{"/debug/pprof/goroutine", ""},
		{"/debug/pprof/threadcreate", ""},
	}

	for _, endpoint := range profilingEndpoints {
		t.Run(" "+endpoint.endpoint, func(t *testing.T) {
			url := endpoint.endpoint + endpoint.params
			req := httptest.NewRequest("GET", url, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// All pprof endpoints should return 200 or at least not 404
			if rr.Code == http.StatusNotFound {
				t.Errorf("profiling endpoint %s returned 404, should be registered", endpoint.endpoint)
			}
		})
	}
}

func TestMizu_OptionComposition(t *testing.T) {
	// Test that multiple options work together
	srv := mizu.NewServer("composed-test",
		mizu.WithPrometheusMetrics(),
		mizu.WithProfilingHandlers(),
	)

	handler := srv.Handler()

	// Test default healthz endpoint (automatically mounted)
	req1 := httptest.NewRequest("GET", "/healthz", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Errorf("expected status 200 for /healthz, got %d", rr1.Code)
	}

	// Test Prometheus metrics
	req2 := httptest.NewRequest("GET", "/metrics", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("expected status 200 for /metrics, got %d", rr2.Code)
	}

	// Test pprof endpoint
	req3 := httptest.NewRequest("GET", "/debug/pprof/", nil)
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)

	if rr3.Code == http.StatusNotFound {
		t.Error("expected pprof endpoint to be available")
	}
}

func TestMizu_ServerWithEmptyServiceName(t *testing.T) {
	// Test that srv works even with empty service name
	srv := mizu.NewServer("")

	srv.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("empty-service"))
	})

	handler := srv.Handler()
	req := httptest.NewRequest("GET", "/empty", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "empty-service", rr.Body.String())
}
