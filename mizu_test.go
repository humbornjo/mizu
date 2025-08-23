package mizu_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/humbornjo/mizu"
)

type ctxkey string

func TestMizu_NewServer(t *testing.T) {
	tests := []struct {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mizu.NewServer(tt.serviceName, tt.opts...)

			if server == nil {
				t.Fatal("NewServer returned nil")
			}

			// Test that the server can handle basic operations
			server.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("test"))
			})

			handler := server.Handler()
			if handler == nil {
				t.Fatal("Handler() returned nil")
			}

			// Test basic request handling
			req := httptest.NewRequest("GET", "/test", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rr.Code)
			}

			if rr.Body.String() != "test" {
				t.Errorf("expected body 'test', got %q", rr.Body.String())
			}
		})
	}
}

func TestMizu_WithPrometheusMetrics(t *testing.T) {
	server := mizu.NewServer("metrics-test", mizu.WithPrometheusMetrics())

	handler := server.Handler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 for /metrics endpoint, got %d", rr.Code)
	}

	// Check that it returns Prometheus format metrics
	body := rr.Body.String()
	if !strings.Contains(body, "# HELP") || !strings.Contains(body, "# TYPE") {
		t.Error("metrics endpoint doesn't return Prometheus format")
	}
}

func TestMizu_WithCustomHttpServer(t *testing.T) {
	customServer := &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	server := mizu.NewServer(
		"custom-test",
		mizu.WithCustomHttpServer(customServer),
	)

	// Add a test route
	server.HandleFunc("/custom", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("custom"))
	})

	handler := server.Handler()
	req := httptest.NewRequest("GET", "/custom", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	if rr.Body.String() != "custom" {
		t.Errorf("expected body 'custom', got %q", rr.Body.String())
	}
}

func TestMizu_WithWizardHandleReadiness(t *testing.T) {
	tests := []struct {
		name       string
		server     *mizu.Server
		endpoint   string
		expected   string
		statusCode int
	}{
		{
			name:       "default healthz endpoint",
			server:     mizu.NewServer("default-healthz"),
			endpoint:   "/healthz",
			expected:   "OK",
			statusCode: http.StatusOK,
		},
		{
			name: "custom readiness handler",
			server: mizu.NewServer(
				"custom-readiness",
				mizu.WithWizardHandleReadiness(
					"/healthz",
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
			endpoint:   "/healthz",
			expected:   "Custom: Ready",
			statusCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.server.Handler()
			req := httptest.NewRequest("GET", tt.endpoint, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, rr.Code)
			}

			body := strings.TrimSpace(rr.Body.String())
			if body != tt.expected {
				t.Errorf("expected body %q, got %q", tt.expected, body)
			}
		})
	}
}

func TestMizu_WithProfilingHandlers(t *testing.T) {
	server := mizu.NewServer("profiling-test", mizu.WithProfilingHandlers())

	handler := server.Handler()

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
	server := mizu.NewServer("composed-test",
		mizu.WithPrometheusMetrics(),
		mizu.WithProfilingHandlers(),
	)

	handler := server.Handler()

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
	// Test that server works even with empty service name
	server := mizu.NewServer("")

	server.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("empty-service"))
	})

	handler := server.Handler()
	req := httptest.NewRequest("GET", "/empty", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	if rr.Body.String() != "empty-service" {
		t.Errorf("expected body 'empty-service', got %q", rr.Body.String())
	}
}
