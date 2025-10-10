package mizu_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/stretchr/testify/assert"
)

func TestMux_Base_Route(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, r.Method)
	}

	handleHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "handle")
	})

	testCases := []struct {
		name               string
		setup              func(m *mizu.Server)
		method             string
		path               string
		expectedStatusCode int
		expectedBody       string
	}{
		{
			name: "Get",
			setup: func(s *mizu.Server) {
				s.Get("/test", handler)
			},
			method:             http.MethodGet,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodGet,
		},
		{
			name: "Post",
			setup: func(s *mizu.Server) {
				s.Post("/test", handler)
			},
			method:             http.MethodPost,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodPost,
		},
		{
			name: "Put",
			setup: func(s *mizu.Server) {
				s.Put("/test", handler)
			},
			method:             http.MethodPut,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodPut,
		},
		{
			name: "Delete",
			setup: func(s *mizu.Server) {
				s.Delete("/test", handler)
			},
			method:             http.MethodDelete,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodDelete,
		},
		{
			name: "Patch",
			setup: func(s *mizu.Server) {
				s.Patch("/test", handler)
			},
			method:             http.MethodPatch,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodPatch,
		},
		{
			name: "Head",
			setup: func(s *mizu.Server) {
				s.Head("/test", func(w http.ResponseWriter, r *http.Request) {
					// No body should be written for a HEAD request
				})
			},
			method:             http.MethodHead,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       "",
		},
		{
			name: "Options",
			setup: func(s *mizu.Server) {
				s.Options("/test", handler)
			},
			method:             http.MethodOptions,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodOptions,
		},
		{
			name: "Connect",
			setup: func(s *mizu.Server) {
				s.Connect("/test", handler)
			},
			method:             http.MethodConnect,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodConnect,
		},
		{
			name: "Trace",
			setup: func(s *mizu.Server) {
				s.Trace("/test", handler)
			},
			method:             http.MethodTrace,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodTrace,
		},
		{
			name: "HandleFunc",
			setup: func(s *mizu.Server) {
				s.HandleFunc("/test", handler)
			},
			method:             http.MethodGet,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       http.MethodGet,
		},
		{
			name: "Handle",
			setup: func(s *mizu.Server) {
				s.Handle("/test", handleHandler)
			},
			method:             http.MethodGet,
			path:               "/test",
			expectedStatusCode: http.StatusOK,
			expectedBody:       "handle",
		},
		{
			name: "Not Found",
			setup: func(s *mizu.Server) {
				s.Get("/other", handler)
			},
			method:             http.MethodGet,
			path:               "/notfound",
			expectedStatusCode: http.StatusNotFound,
			expectedBody:       "404 page not found\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Each test case gets a fresh Serveinternal.Mux and internal.Mux
			srv := mizu.NewServer("-")
			tc.setup(srv)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rr, req)
			assert.Equal(t, rr.Code, tc.expectedStatusCode)
			assert.Equal(t, tc.expectedBody, rr.Body.String())
		})
	}
}

func TestMux_Base_Middleware(t *testing.T) {
	t.Run("middleware application", func(t *testing.T) {
		srv := mizu.NewServer("-")

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
		m1 := srv.Use(noopMiddleware)
		m1.Use(middleware1).Use(middleware2).Get("/with-middlewares", handler)

		m2 := srv.Use(noopMiddleware)
		m2.Get("/without-middlewares", handler)

		m3 := srv.Use(noopMiddleware)
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

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, http.StatusOK, rr.Code)

				m1Header := rr.Header().Get("X-Test-Middleware1")
				assert.Equal(t, tc.expectMiddleware1, m1Header == "true")
				m2Header := rr.Header().Get("X-Test-Middleware2")
				assert.Equal(t, tc.expectMiddleware2, m2Header == "true")
			})
		}
	})
}

func TestMux_Group_Route(t *testing.T) {
	t.Run("basic group routing", func(t *testing.T) {
		srv := mizu.NewServer("-")

		handler := func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "group")
		}

		// Create a group with prefix
		groupApi := srv.Use(noopMiddleware).Group("/api")
		groupApi.Get("/users", handler)
		groupApi.Post("/users", handler)

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

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, tc.expectedStatus, rr.Code)
				assert.Equal(t, tc.expectedBody, rr.Body.String())
			})
		}
	})

	t.Run("nested groups", func(t *testing.T) {
		srv := mizu.NewServer("-")

		handler := func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, r.URL.Path)
		}

		// Create nested groups
		groupApi := srv.Use(noopMiddleware).Group("/api")
		groupApi_V1 := groupApi.Group("/v1")
		groupApi_V1_Users := groupApi_V1.Group("/users")

		groupApi_V1_Users.Get("/list", handler)
		groupApi_V1.Get("/status", handler)
		groupApi.Get("/health", handler)

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

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, tc.expectedStatus, rr.Code)
				assert.Equal(t, tc.expectedBody, rr.Body.String())
			})
		}
	})

	t.Run("group with all HTTP methods", func(t *testing.T) {
		srv := mizu.NewServer("-")

		handler := func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, r.Method)
		}

		groupApi := srv.Use(noopMiddleware).Group("/api")

		// Register all HTTP methods
		groupApi.Get("/resource", handler)
		groupApi.Post("/resource", handler)
		groupApi.Put("/resource", handler)
		groupApi.Delete("/resource", handler)
		groupApi.Patch("/resource", handler)
		groupApi.Head("/resource", handler)
		groupApi.Options("/resource", handler)
		groupApi.Connect("/resource", handler)
		groupApi.Trace("/resource", handler)

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

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, http.StatusOK, rr.Code)
				assert.Equal(t, tc.expectedBody, rr.Body.String())
			})
		}
	})

	t.Run("group with Handle and HandleFunc", func(t *testing.T) {
		srv := mizu.NewServer("-")

		handlerFunc := func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "handlerfunc")
		}

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "handler")
		})

		groupApi := srv.Use(noopMiddleware).Group("/api")
		groupApi.HandleFunc("/test-func", handlerFunc)
		groupApi.Handle("/test-handler", handler)

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

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, http.StatusOK, rr.Code)
				assert.Equal(t, tc.expectedBody, rr.Body.String())
			})
		}
	})
}

func TestMux_Group_Middleware(t *testing.T) {
	t.Run("middleware inheritance with groups", func(t *testing.T) {
		srv := mizu.NewServer("-")

		// Create middleware that adds headers to track application
		middleware1 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware-1", "applied")
				next.ServeHTTP(w, r)
			})
		}

		middleware2 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware-2", "applied")
				next.ServeHTTP(w, r)
			})
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "OK")
		}

		srv.Use(middleware1)
		srv.Get("/path1", handler)

		groupG1 := srv.Use(middleware2).Group("/g1")
		groupG1.Get("/path2", handler)

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
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				rr := httptest.NewRecorder()

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, http.StatusOK, rr.Code)

				// Check middleware headers
				m1Header := rr.Header().Get("X-Middleware-1")
				assert.Equal(t, tc.expectMiddleware1, m1Header == "applied")
				m2Header := rr.Header().Get("X-Middleware-2")
				assert.Equal(t, tc.expectMiddleware2, m2Header == "applied")
			})
		}
	})

	t.Run("nested group middleware inheritance", func(t *testing.T) {
		srv := mizu.NewServer("-")

		// Create multiple middlewares to track inheritance
		middleware1 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware-1", "applied")
				next.ServeHTTP(w, r)
			})
		}

		middleware2 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware-2", "applied")
				next.ServeHTTP(w, r)
			})
		}

		middleware3 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware-3", "applied")
				next.ServeHTTP(w, r)
			})
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "OK")
		}

		// Setup nested groups: Group("api") -> Group("v1")
		groupApi := srv.Use(middleware1).Group("/api")
		groupApi_V1 := groupApi.Group("/v1")

		// Add middleware to the nested group
		groupApi_V1.Use(middleware2)
		groupApi_V1.Get("/users", handler)

		// Add another level with middleware
		groupApi_V1.Use(middleware3).Get("/posts", handler)

		testCases := []struct {
			name              string
			path              string
			expectMiddleware1 bool
			expectMiddleware2 bool
			expectMiddleware3 bool
		}{
			{
				name:              "/api/v1/users should have mw1 and mw2",
				path:              "/api/v1/users",
				expectMiddleware1: true,
				expectMiddleware2: true,
				expectMiddleware3: false,
			},
			{
				name:              "/api/v1/posts should have mw1, mw2, and mw3",
				path:              "/api/v1/posts",
				expectMiddleware1: true,
				expectMiddleware2: true,
				expectMiddleware3: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				rr := httptest.NewRecorder()

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, http.StatusOK, rr.Code)

				// Check middleware headers
				m1Header := rr.Header().Get("X-Middleware-1")
				assert.Equal(t, tc.expectMiddleware1, m1Header == "applied")
				m2Header := rr.Header().Get("X-Middleware-2")
				assert.Equal(t, tc.expectMiddleware2, m2Header == "applied")
				m3Header := rr.Header().Get("X-Middleware-3")
				assert.Equal(t, tc.expectMiddleware3, m3Header == "applied")
			})
		}
	})
}

func noopMiddleware(next http.Handler) http.Handler {
	return next
}
