package mizu_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/internal"
)

func TestMux_Route_Base(t *testing.T) {
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

func TestMux_Middleware_Base(t *testing.T) {
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
}

func TestMux_Route_Group(t *testing.T) {
	t.Run("basic group routing", func(t *testing.T) {
		server := mizu.NewServer("-")

		handler := func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "group")
		}

		// Create a group with prefix
		api := server.Use(noopMiddleware).Group("/api")
		api.Get("/users", handler)
		api.Post("/users", handler)

		testCases := []struct {
			name           string
			method         string
			path           string
			expectedStatus int
			expectedBody   string
		}{
			{
				name:           "GET /api/users",
				method:         http.MethodGet,
				path:           "/api/users",
				expectedStatus: http.StatusOK,
				expectedBody:   "group",
			},
			{
				name:           "POST /api/users",
				method:         http.MethodPost,
				path:           "/api/users",
				expectedStatus: http.StatusOK,
				expectedBody:   "group",
			},
			{
				name:           "GET /api/users/ (trailing slash)",
				method:         http.MethodGet,
				path:           "/api/users/",
				expectedStatus: http.StatusNotFound,
				expectedBody:   "404 page not found\n",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
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
	})

	t.Run("nested groups", func(t *testing.T) {
		server := mizu.NewServer("-")

		handler := func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, r.URL.Path)
		}

		// Create nested groups
		api := server.Use(noopMiddleware).Group("/api")
		v1 := api.Group("/v1")
		users := v1.Group("/users")

		users.Get("/list", handler)
		v1.Get("/status", handler)
		api.Get("/health", handler)

		testCases := []struct {
			name           string
			path           string
			expectedStatus int
			expectedBody   string
		}{
			{
				name:           "nested path /api/v1/users/list",
				path:           "/api/v1/users/list",
				expectedStatus: http.StatusOK,
				expectedBody:   "/api/v1/users/list",
			},
			{
				name:           "intermediate path /api/v1/status",
				path:           "/api/v1/status",
				expectedStatus: http.StatusOK,
				expectedBody:   "/api/v1/status",
			},
			{
				name:           "top level path /api/health",
				path:           "/api/health",
				expectedStatus: http.StatusOK,
				expectedBody:   "/api/health",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
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
	})

	t.Run("group with all HTTP methods", func(t *testing.T) {
		server := mizu.NewServer("-")

		handler := func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, r.Method)
		}

		api := server.Use(noopMiddleware).Group("/api")

		// Register all HTTP methods
		api.Get("/resource", handler)
		api.Post("/resource", handler)
		api.Put("/resource", handler)
		api.Delete("/resource", handler)
		api.Patch("/resource", handler)
		api.Head("/resource", handler)
		api.Options("/resource", handler)
		api.Connect("/resource", handler)
		api.Trace("/resource", handler)

		testCases := []struct {
			name         string
			method       string
			path         string
			expectedBody string
		}{
			{name: "GET", method: http.MethodGet, path: "/api/resource", expectedBody: http.MethodGet},
			{name: "POST", method: http.MethodPost, path: "/api/resource", expectedBody: http.MethodPost},
			{name: "PUT", method: http.MethodPut, path: "/api/resource", expectedBody: http.MethodPut},
			{name: "DELETE", method: http.MethodDelete, path: "/api/resource", expectedBody: http.MethodDelete},
			{name: "PATCH", method: http.MethodPatch, path: "/api/resource", expectedBody: http.MethodPatch},
			{name: "HEAD", method: http.MethodHead, path: "/api/resource", expectedBody: http.MethodHead},
			{name: "OPTIONS", method: http.MethodOptions, path: "/api/resource", expectedBody: http.MethodOptions},
			{name: "CONNECT", method: http.MethodConnect, path: "/api/resource", expectedBody: http.MethodConnect},
			{name: "TRACE", method: http.MethodTrace, path: "/api/resource", expectedBody: http.MethodTrace},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				rr := httptest.NewRecorder()

				server.Handler().ServeHTTP(rr, req)

				if status := rr.Code; status != http.StatusOK {
					t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
				}

				if body := rr.Body.String(); body != tc.expectedBody {
					t.Errorf("handler returned unexpected body: got %q want %q", body, tc.expectedBody)
				}
			})
		}
	})

	t.Run("group with Handle and HandleFunc", func(t *testing.T) {
		server := mizu.NewServer("-")

		handlerFunc := func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "handlerfunc")
		}

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "handler")
		})

		api := server.Use(noopMiddleware).Group("/api")
		api.HandleFunc("/test-func", handlerFunc)
		api.Handle("/test-handler", handler)

		testCases := []struct {
			name         string
			path         string
			expectedBody string
		}{
			{name: "HandleFunc", path: "/api/test-func", expectedBody: "handlerfunc"},
			{name: "Handle", path: "/api/test-handler", expectedBody: "handler"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				rr := httptest.NewRecorder()

				server.Handler().ServeHTTP(rr, req)

				if status := rr.Code; status != http.StatusOK {
					t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
				}

				if body := rr.Body.String(); body != tc.expectedBody {
					t.Errorf("handler returned unexpected body: got %q want %q", body, tc.expectedBody)
				}
			})
		}
	})
}

func TestMux_Middleware_Group(t *testing.T) {
	t.Run("middleware inheritance with groups", func(t *testing.T) {
		server := mizu.NewServer("-")

		// Create middleware that adds headers to track application
		mw1 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware-1", "applied")
				next.ServeHTTP(w, r)
			})
		}

		mw2 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware-2", "applied")
				next.ServeHTTP(w, r)
			})
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "OK")
		}

		// Setup: server.Use(mw1) and server.Get("/path1")
		mux1 := server.Use(mw1)
		mux1.Get("/path1", handler)

		// Setup: gp1 = server.Group("g1"), then gp1.Use(mw2) and gp1.Get("/path2")
		gp1 := server.Use(noopMiddleware).Group("g1")
		gp1.Use(mw2).Get("/path2", handler)

		testCases := []struct {
			name              string
			path              string
			expectMiddleware1 bool
			expectMiddleware2 bool
		}{
			{
				name:              "/path1 should only have mw1",
				path:              "/path1",
				expectMiddleware1: true,
				expectMiddleware2: false,
			},
			{
				name:              "/g1/path2 should have both mw1 and mw2",
				path:              "/g1/path2",
				expectMiddleware1: true,
				expectMiddleware2: true,
			},
			{
				name:              "/g1/path1 should only have mw1 (not registered, but would inherit if it was)",
				path:              "/g1/path1",
				expectMiddleware1: false, // This path doesn't exist, so we expect 404
				expectMiddleware2: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				rr := httptest.NewRecorder()

				server.Handler().ServeHTTP(rr, req)

				if tc.path == "/g1/path1" {
					// This path should return 404
					if status := rr.Code; status != http.StatusNotFound {
						t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusNotFound)
					}
				} else {
					// These paths should return 200
					if status := rr.Code; status != http.StatusOK {
						t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
					}
				}

				// Check middleware headers
				m1Header := rr.Header().Get("X-Middleware-1")
				if (m1Header == "applied") != tc.expectMiddleware1 {
					t.Errorf("unexpected middleware 1 execution: got header %q, want execution %v", m1Header, tc.expectMiddleware1)
				}

				m2Header := rr.Header().Get("X-Middleware-2")
				if (m2Header == "applied") != tc.expectMiddleware2 {
					t.Errorf("unexpected middleware 2 execution: got header %q, want execution %v", m2Header, tc.expectMiddleware2)
				}
			})
		}
	})

	// t.Run("nested group middleware inheritance", func(t *testing.T) {
	// 	server := mizu.NewServer("-")
	//
	// 	// Create multiple middlewares to track inheritance
	// 	mw1 := func(next http.Handler) http.Handler {
	// 		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// 			w.Header().Set("X-Middleware-1", "applied")
	// 			next.ServeHTTP(w, r)
	// 		})
	// 	}
	//
	// 	mw2 := func(next http.Handler) http.Handler {
	// 		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// 			w.Header().Set("X-Middleware-2", "applied")
	// 			next.ServeHTTP(w, r)
	// 		})
	// 	}
	//
	// 	mw3 := func(next http.Handler) http.Handler {
	// 		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// 			w.Header().Set("X-Middleware-3", "applied")
	// 			next.ServeHTTP(w, r)
	// 		})
	// 	}
	//
	// 	handler := func(w http.ResponseWriter, r *http.Request) {
	// 		w.WriteHeader(http.StatusOK)
	// 		_, _ = fmt.Fprint(w, "OK")
	// 	}
	//
	// 	// Setup nested groups: server.Use(mw1) -> Group("api") -> Group("v1")
	// 	api := server.Use(mw1).Group("api")
	// 	v1 := api.Group("v1")
	//
	// 	// Add middleware to the nested group
	// 	v1.Use(mw2).Get("/users", handler)
	//
	// 	// Add another level with middleware
	// 	v1.Use(mw3).Get("/posts", handler)
	//
	// 	testCases := []struct {
	// 		name              string
	// 		path              string
	// 		expectMiddleware1 bool
	// 		expectMiddleware2 bool
	// 		expectMiddleware3 bool
	// 	}{
	// 		{
	// 			name:              "/api/v1/users should have mw1 and mw2",
	// 			path:              "/api/v1/users",
	// 			expectMiddleware1: true,
	// 			expectMiddleware2: true,
	// 			expectMiddleware3: false,
	// 		},
	// 		{
	// 			name:              "/api/v1/posts should have mw1, mw2, and mw3",
	// 			path:              "/api/v1/posts",
	// 			expectMiddleware1: true,
	// 			expectMiddleware2: true,
	// 			expectMiddleware3: true,
	// 		},
	// 	}
	//
	// 	for _, tc := range testCases {
	// 		t.Run(tc.name, func(t *testing.T) {
	// 			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
	// 			rr := httptest.NewRecorder()
	//
	// 			server.Handler().ServeHTTP(rr, req)
	//
	// 			if status := rr.Code; status != http.StatusOK {
	// 				t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	// 			}
	//
	// 			// Check middleware headers
	// 			m1Header := rr.Header().Get("X-Middleware-1")
	// 			if (m1Header == "applied") != tc.expectMiddleware1 {
	// 				t.Errorf("unexpected middleware 1 execution: got header %q, want execution %v", m1Header, tc.expectMiddleware1)
	// 			}
	//
	// 			m2Header := rr.Header().Get("X-Middleware-2")
	// 			if (m2Header == "applied") != tc.expectMiddleware2 {
	// 				t.Errorf("unexpected middleware 2 execution: got header %q, want execution %v", m2Header, tc.expectMiddleware2)
	// 			}
	//
	// 			m3Header := rr.Header().Get("X-Middleware-3")
	// 			if (m3Header == "applied") != tc.expectMiddleware3 {
	// 				t.Errorf("unexpected middleware 3 execution: got header %q, want execution %v", m3Header, tc.expectMiddleware3)
	// 			}
	// 		})
	// 	}
	// })
}

func noopMiddleware(next http.Handler) http.Handler {
	return next
}
