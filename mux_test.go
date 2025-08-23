package mizu_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/internal"
)

func TestMux_Routing(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, r.Method)
	}

	handleHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "handle")
	})

	testCases := []struct {
		name           string
		setup          func(m internal.Mux)
		method         string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "Get",
			setup: func(m internal.Mux) {
				m.Get("/test", handler)
			},
			method:         http.MethodGet,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodGet,
		},
		{
			name: "Post",
			setup: func(m internal.Mux) {
				m.Post("/test", handler)
			},
			method:         http.MethodPost,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodPost,
		},
		{
			name: "Put",
			setup: func(m internal.Mux) {
				m.Put("/test", handler)
			},
			method:         http.MethodPut,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodPut,
		},
		{
			name: "Delete",
			setup: func(m internal.Mux) {
				m.Delete("/test", handler)
			},
			method:         http.MethodDelete,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodDelete,
		},
		{
			name: "Patch",
			setup: func(m internal.Mux) {
				m.Patch("/test", handler)
			},
			method:         http.MethodPatch,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodPatch,
		},
		{
			name: "Head",
			setup: func(m internal.Mux) {
				m.Head("/test", func(w http.ResponseWriter, r *http.Request) {
					// No body should be written for a HEAD request
				})
			},
			method:         http.MethodHead,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   "",
		},
		{
			name: "Options",
			setup: func(m internal.Mux) {
				m.Options("/test", handler)
			},
			method:         http.MethodOptions,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodOptions,
		},
		{
			name: "Connect",
			setup: func(m internal.Mux) {
				m.Connect("/test", handler)
			},
			method:         http.MethodConnect,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodConnect,
		},
		{
			name: "Trace",
			setup: func(m internal.Mux) {
				m.Trace("/test", handler)
			},
			method:         http.MethodTrace,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodTrace,
		},
		{
			name: "HandleFunc",
			setup: func(m internal.Mux) {
				m.HandleFunc("/test", handler)
			},
			method:         http.MethodGet,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   http.MethodGet,
		},
		{
			name: "Handle",
			setup: func(m internal.Mux) {
				m.Handle("/test", handleHandler)
			},
			method:         http.MethodGet,
			path:           "/test",
			expectedStatus: http.StatusOK,
			expectedBody:   "handle",
		},
		{
			name: "Not Found",
			setup: func(m internal.Mux) {
				m.Get("/other", handler)
			},
			method:         http.MethodGet,
			path:           "/notfound",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "404 page not found\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Each test case gets a fresh Serveinternal.Mux and internal.Mux
			server := mizu.NewServer("-")
			mux := server.Use(noopMiddleware)
			tc.setup(mux)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()

			server.Handler().ServeHTTP(rr, req)

			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tc.expectedStatus)
			}

			if body := rr.Body.String(); body != tc.expectedBody {
				t.Errorf("handler returned unexpected body: got %q want %q", body, tc.expectedBody)
			}
		})
	}
}

func TestMux_Middleware(t *testing.T) {
	t.Run("middleware application", func(t *testing.T) {
		server := mizu.NewServer("-")

		middleware1 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Test-Middleware1", "true")
				next.ServeHTTP(w, r)
			})
		}

		middleware2 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Test-Middleware2", "true")
				next.ServeHTTP(w, r)
			})
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "OK")
		}

		// Simulate getting a new mux for each middleware chain
		m1 := server.Use(noopMiddleware)
		m1.Use(middleware1).Use(middleware2).Get("/with-middlewares", handler)

		m2 := server.Use(noopMiddleware)
		m2.Get("/without-middlewares", handler)

		m3 := server.Use(noopMiddleware)
		m3.Use(middleware1).Handle("/handle-with-middleware", http.HandlerFunc(handler))

		testCases := []struct {
			name              string
			path              string
			expectMiddleware1 bool
			expectMiddleware2 bool
		}{
			{
				name:              "With Middlewares",
				path:              "/with-middlewares",
				expectMiddleware1: true,
				expectMiddleware2: true,
			},
			{
				name:              "Without Middlewares",
				path:              "/without-middlewares",
				expectMiddleware1: false,
				expectMiddleware2: false,
			},
			{
				name:              "Handle with middleware",
				path:              "/handle-with-middleware",
				expectMiddleware1: true,
				expectMiddleware2: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				rr := httptest.NewRecorder()

				server.Handler().ServeHTTP(rr, req)

				if status := rr.Code; status != http.StatusOK {
					t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
				}

				m1Header := rr.Header().Get("X-Test-Middleware1")
				if (m1Header == "true") != tc.expectMiddleware1 {
					t.Errorf("unexpected middleware 1 execution: got header %q, want execution %v", m1Header, tc.expectMiddleware1)
				}

				m2Header := rr.Header().Get("X-Test-Middleware2")
				if (m2Header == "true") != tc.expectMiddleware2 {
					t.Errorf("unexpected middleware 2 execution: got header %q, want execution %v", m2Header, tc.expectMiddleware2)
				}
			})
		}
	})

	t.Run("middleware lifecycle panic", func(t *testing.T) {
		server := mizu.NewServer("-")
		mux := server.Use(noopMiddleware)

		handler := func(w http.ResponseWriter, r *http.Request) {}

		// This should work and consume the middleware set
		mux.Use(func(h http.Handler) http.Handler { return h }).Get("/path1", handler)

		// Now m.ms is nil. Calling Use again on the same mux instance should panic.
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("m.Use() did not panic after middleware set was consumed")
			}
		}()
		mux.Use(func(h http.Handler) http.Handler { return h })
	})
}

func noopMiddleware(next http.Handler) http.Handler {
	return next
}
