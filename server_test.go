package mizu_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/stretchr/testify/assert"
)

const (
	key1 ctxkey = "key1"
	key2 ctxkey = "key2"
)

func TestServer_HTTPMethods(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		pattern        string
		setupHandler   func(*mizu.Server, string, http.HandlerFunc)
		requestMethod  string
		requestPath    string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:    "GET handler",
			method:  "GET",
			pattern: "/users",
			setupHandler: func(s *mizu.Server, pattern string, handler http.HandlerFunc) {
				s.Get(pattern, handler)
			},
			requestMethod:  "GET",
			requestPath:    "/users",
			expectedStatus: http.StatusOK,
			expectedBody:   "GET /users",
		},
		{
			name:    "POST handler",
			method:  "POST",
			pattern: "/users",
			setupHandler: func(s *mizu.Server, pattern string, handler http.HandlerFunc) {
				s.Post(pattern, handler)
			},
			requestMethod:  "POST",
			requestPath:    "/users",
			expectedStatus: http.StatusOK,
			expectedBody:   "POST /users",
		},
		{
			name:    "PUT handler",
			method:  "PUT",
			pattern: "/users/{id}",
			setupHandler: func(s *mizu.Server, pattern string, handler http.HandlerFunc) {
				s.Put(pattern, handler)
			},
			requestMethod:  "PUT",
			requestPath:    "/users/123",
			expectedStatus: http.StatusOK,
			expectedBody:   "PUT /users/{id}",
		},
		{
			name:    "DELETE handler",
			method:  "DELETE",
			pattern: "/users/{id}",
			setupHandler: func(s *mizu.Server, pattern string, handler http.HandlerFunc) {
				s.Delete(pattern, handler)
			},
			requestMethod:  "DELETE",
			requestPath:    "/users/123",
			expectedStatus: http.StatusOK,
			expectedBody:   "DELETE /users/{id}",
		},
		{
			name:    "PATCH handler",
			method:  "PATCH",
			pattern: "/users/{id}",
			setupHandler: func(s *mizu.Server, pattern string, handler http.HandlerFunc) {
				s.Patch(pattern, handler)
			},
			requestMethod:  "PATCH",
			requestPath:    "/users/123",
			expectedStatus: http.StatusOK,
			expectedBody:   "PATCH /users/{id}",
		},
		{
			name:    "HEAD handler",
			method:  "HEAD",
			pattern: "/status",
			setupHandler: func(s *mizu.Server, pattern string, handler http.HandlerFunc) {
				s.Head(pattern, handler)
			},
			requestMethod:  "HEAD",
			requestPath:    "/status",
			expectedStatus: http.StatusOK,
			expectedBody:   "",
		},
		{
			name:    "OPTIONS handler",
			method:  "OPTIONS",
			pattern: "/api",
			setupHandler: func(s *mizu.Server, pattern string, handler http.HandlerFunc) {
				s.Options(pattern, handler)
			},
			requestMethod:  "OPTIONS",
			requestPath:    "/api",
			expectedStatus: http.StatusOK,
			expectedBody:   "OPTIONS /api",
		},
		{
			name:    "wrong method returns 405",
			method:  "GET",
			pattern: "/users",
			setupHandler: func(s *mizu.Server, pattern string, handler http.HandlerFunc) {
				s.Get(pattern, handler)
			},
			requestMethod:  "POST",
			requestPath:    "/users",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := mizu.NewServer("test-server")

			handler := func(w http.ResponseWriter, r *http.Request) {
				if tt.method == "HEAD" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.method + " " + tt.pattern))
			}

			tt.setupHandler(srv, tt.pattern, handler)

			req := httptest.NewRequest(tt.requestMethod, tt.requestPath, nil)
			rr := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rr, req)
			assert.Equal(t, tt.expectedStatus, rr.Code)
			if tt.expectedBody != "" {
				assert.Contains(t, rr.Body.String(), tt.expectedBody)
			}
		})
	}
}

func TestServer_Handle_And_HandleFunc(t *testing.T) {
	tests := []struct {
		name         string
		useHandle    bool
		pattern      string
		expectedBody string
	}{
		{
			name:         "Handle with http.Handler",
			useHandle:    true,
			pattern:      "/handle",
			expectedBody: "handled",
		},
		{
			name:         "HandleFunc with http.HandlerFunc",
			useHandle:    false,
			pattern:      "/handlefunc",
			expectedBody: "handled func",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := mizu.NewServer("test-server")

			if tt.useHandle {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("handled"))
				})
				srv.Handle(tt.pattern, handler)
			} else {
				srv.HandleFunc(tt.pattern, func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("handled func"))
				})
			}

			req := httptest.NewRequest("GET", tt.pattern, nil)
			rr := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tt.expectedBody, rr.Body.String())
		})
	}
}

func TestServer_Middleware_Mux(t *testing.T) {
	t.Run("middleware_actually_applied", func(t *testing.T) {
		srv := mizu.NewServer("test-server")

		// Create middleware that adds observable behavior
		authMux := srv.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Auth", "middleware-applied")
				next.ServeHTTP(w, r)
			})
		})

		loggingMux := authMux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Log", "logged")
				next.ServeHTTP(w, r)
			})
		})

		// Register route through middleware chain
		loggingMux.HandleFunc("/protected", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("protected-content"))
		})

		// Register direct route (no middleware)
		srv.HandleFunc("/public", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("public-content"))
		})

		tests := []struct {
			name              string
			path              string
			expectedBody      string
			expectedAuthHdr   string
			expectedLogHdr    string
			shouldHaveHeaders bool
		}{
			{
				name:              "protected route with middleware",
				path:              "/protected",
				expectedBody:      "protected-content",
				expectedAuthHdr:   "middleware-applied",
				expectedLogHdr:    "logged",
				shouldHaveHeaders: true,
			},
			{
				name:              "public route without middleware",
				path:              "/public",
				expectedBody:      "public-content",
				shouldHaveHeaders: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", tt.path, nil)
				rr := httptest.NewRecorder()

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, http.StatusOK, rr.Code)
				assert.Equal(t, tt.expectedBody, rr.Body.String())

				// Test middleware headers
				if tt.shouldHaveHeaders {
					assert.Equal(t, tt.expectedAuthHdr, rr.Header().Get("X-Auth"))
					assert.Equal(t, tt.expectedLogHdr, rr.Header().Get("X-Log"))
				} else {
					assert.Empty(t, rr.Header().Get("X-Auth"))
					assert.Empty(t, rr.Header().Get("X-Log"))
				}
			})
		}
	})

	t.Run("middleware_execution_order", func(t *testing.T) {
		srv := mizu.NewServer("test-server")
		var executionOrder []string

		// First middleware
		mux1 := srv.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				executionOrder = append(executionOrder, "middleware1-before")
				next.ServeHTTP(w, r)
				executionOrder = append(executionOrder, "middleware1-after")
			})
		})

		// Second middleware
		mux2 := mux1.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				executionOrder = append(executionOrder, "middleware2-before")
				next.ServeHTTP(w, r)
				executionOrder = append(executionOrder, "middleware2-after")
			})
		})

		mux2.HandleFunc("/order", func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "handler")
			_, _ = w.Write([]byte("order-test"))
		})

		req := httptest.NewRequest("GET", "/order", nil)
		rr := httptest.NewRecorder()

		// Reset execution order
		executionOrder = []string{}

		srv.Handler().ServeHTTP(rr, req)

		expectedOrder := []string{
			"middleware1-before",
			"middleware2-before",
			"handler",
			"middleware2-after",
			"middleware1-after",
		}

		assert.Equal(t, len(expectedOrder), len(executionOrder))
		for i, expected := range expectedOrder {
			assert.Equal(t, expected, executionOrder[i])
		}
	})
}

func TestServer_Middleware_Server(t *testing.T) {
	t.Run("middleware_function_composition", func(t *testing.T) {
		srv := mizu.NewServer("test-server")

		// Add multiple middleware through different Use() calls
		srv.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Bucket1", "applied")
				next.ServeHTTP(w, r)
			})
		})

		srv.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Bucket2", "applied")
				next.ServeHTTP(w, r)
			})
		})

		srv.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("test-handler"))
		})

		// Test that the middleware composer applies all middlewares
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "applied", rr.Header().Get("X-Bucket1"))
		assert.Equal(t, "applied", rr.Header().Get("X-Bucket2"))
		assert.Equal(t, "test-handler", rr.Body.String())
	})

	t.Run("middleware_function_order", func(t *testing.T) {
		srv := mizu.NewServer("test-server")
		var executionOrder []string

		srv.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				executionOrder = append(executionOrder, "bucket1-before")
				next.ServeHTTP(w, r)
				executionOrder = append(executionOrder, "bucket1-after")
			})
		})

		srv.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				executionOrder = append(executionOrder, "bucket2-before")
				next.ServeHTTP(w, r)
				executionOrder = append(executionOrder, "bucket2-after")
			})
		})

		// Test handler
		srv.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "handler")
			_, _ = w.Write([]byte("test"))
		})

		// Reset and test execution order
		executionOrder = []string{}

		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)

		// Verify execution order: buckets applied in reverse order (last bucket first)
		expectedOrder := []string{
			"bucket1-before", // First bucket added becomes outermost
			"bucket2-before", // Last bucket added becomes inner
			"handler",
			"bucket2-after",
			"bucket1-after",
		}

		assert.Equal(t, expectedOrder, executionOrder)
		for i, expected := range expectedOrder {
			assert.Equal(t, expected, executionOrder[i])
		}
	})
}

func TestServer_InjectContext(t *testing.T) {
	tests := []struct {
		name           string
		injectors      []func(context.Context) context.Context
		expectedValues map[ctxkey]any
	}{
		{
			name: "single context injection",
			injectors: []func(context.Context) context.Context{
				func(ctx context.Context) context.Context {
					return context.WithValue(ctx, key1, "value1")
				},
			},
			expectedValues: map[ctxkey]any{
				key1: "value1",
			},
		},
		{
			name: "multiple context injections",
			injectors: []func(context.Context) context.Context{
				func(ctx context.Context) context.Context {
					return context.WithValue(ctx, key1, "value1")
				},
				func(ctx context.Context) context.Context {
					return context.WithValue(ctx, key2, "value2")
				},
			},
			expectedValues: map[ctxkey]any{
				key1: "value1",
				key2: "value2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := mizu.NewServer("test-server")

			for _, injector := range tt.injectors {
				srv.InjectContext(injector)
			}

			var capturedContext context.Context
			srv.HookOnExtractHandler(func(ctx context.Context, s *mizu.Server) {
				capturedContext = ctx
			})

			// Trigger handler extraction
			srv.Handler()
			for key, expected := range tt.expectedValues {
				assert.Equal(t, expected, capturedContext.Value(key))
			}
		})
	}
}

func TestServer_Hooks(t *testing.T) {
	tests := []struct {
		name                        string
		numStartupHooks             int
		numExtractHandlerHooks      int
		expectedStartupCalls        int
		expectedExtractHandlerCalls int
	}{
		{
			name:                        "no hooks",
			numStartupHooks:             0,
			numExtractHandlerHooks:      0,
			expectedStartupCalls:        0,
			expectedExtractHandlerCalls: 0,
		},
		{
			name:                        "single hooks",
			numStartupHooks:             1,
			numExtractHandlerHooks:      1,
			expectedStartupCalls:        1,
			expectedExtractHandlerCalls: 1,
		},
		{
			name:                        "multiple hooks",
			numStartupHooks:             3,
			numExtractHandlerHooks:      2,
			expectedStartupCalls:        3,
			expectedExtractHandlerCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := mizu.NewServer("test-server")

			var startupCalls, extractHandlerCalls int
			var mu sync.Mutex

			for i := 0; i < tt.numStartupHooks; i++ {
				srv.HookOnStartup(func(ctx context.Context, s *mizu.Server) {
					mu.Lock()
					startupCalls++
					mu.Unlock()
				})
			}

			for i := 0; i < tt.numExtractHandlerHooks; i++ {
				srv.HookOnExtractHandler(func(ctx context.Context, s *mizu.Server) {
					mu.Lock()
					extractHandlerCalls++
					mu.Unlock()
				})
			}

			// Trigger extract handler hooks
			srv.Handler()

			mu.Lock()
			gotExtractHandlerCalls := extractHandlerCalls
			mu.Unlock()

			assert.Equal(t, tt.expectedExtractHandlerCalls, gotExtractHandlerCalls)
		})
	}
}

func TestServer_Handler_CallsHooksEveryTime(t *testing.T) {
	srv := mizu.NewServer("test-server")

	srv.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("test"))
	})

	var hookCalls int
	srv.HookOnExtractHandler(func(ctx context.Context, s *mizu.Server) {
		hookCalls++
	})

	// Call Handler multiple times
	handler1 := srv.Handler()
	handler2 := srv.Handler()

	// Extract handler hooks are called every time Handler() is called
	if hookCalls != 2 {
		t.Errorf("expected extract handler hooks to be called twice, got %d calls", hookCalls)
	}

	// Both handlers should work the same
	req := httptest.NewRequest("GET", "/test", nil)

	rr1 := httptest.NewRecorder()
	handler1.ServeHTTP(rr1, req)

	rr2 := httptest.NewRecorder()
	handler2.ServeHTTP(rr2, req)

	assert.Equal(t, rr1.Body.String(), rr2.Body.String())
}

func TestServer_ConcurrentAccess(t *testing.T) {
	srv := mizu.NewServer("test-server")

	// Simulate concurrent access to server methods
	var wg sync.WaitGroup
	numGoroutines := 100

	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			// Different operations that might race
			switch id % 4 {
			case 0:
				// Use unique paths to avoid conflicts
				srv.HandleFunc(fmt.Sprintf("/concurrent/%d", id), func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("concurrent"))
				})
			case 1:
				srv.InjectContext(func(ctx context.Context) context.Context {
					return context.WithValue(ctx, ctxkey(fmt.Sprintf("concurrent_%d", id)), id)
				})
			case 2:
				srv.HookOnStartup(func(ctx context.Context, s *mizu.Server) {})
			case 3:
				srv.HookOnExtractHandler(func(ctx context.Context, s *mizu.Server) {})
			}
		}(i)
	}

	wg.Wait()

	// Add a test handler after concurrent access
	srv.HandleFunc("/test-after-concurrent", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("test"))
	})

	req := httptest.NewRequest("GET", "/test-after-concurrent", nil)
	rr := httptest.NewRecorder()

	// Handler should still work after concurrent access
	srv.Handler().ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestServer_RootPattern(t *testing.T) {
	srv := mizu.NewServer("test-server")

	srv.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("root"))
	})

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "root", rr.Body.String())
}
