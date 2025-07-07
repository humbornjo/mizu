package mizu_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/humbornjo/mizu"
)

const (
	key1 ctxkey = "key1"
	key2 ctxkey = "key2"
)

// Helper function to get the complete handler with middlewares applied
func getCompleteHandler(server *mizu.Server) http.Handler {
	baseHandler := server.Handler()
	applyMiddlewares := server.Middleware()
	return applyMiddlewares(baseHandler)
}

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
			server := mizu.NewServer("test-server")

			handler := func(w http.ResponseWriter, r *http.Request) {
				if tt.method == "HEAD" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.method + " " + tt.pattern))
			}

			tt.setupHandler(server, tt.pattern, handler)

			req := httptest.NewRequest(tt.requestMethod, tt.requestPath, nil)
			rr := httptest.NewRecorder()

			getCompleteHandler(server).ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			if tt.expectedBody != "" && !strings.Contains(rr.Body.String(), tt.expectedBody) {
				t.Errorf("expected body to contain %q, got %q", tt.expectedBody, rr.Body.String())
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
			server := mizu.NewServer("test-server")

			if tt.useHandle {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("handled"))
				})
				server.Handle(tt.pattern, handler)
			} else {
				server.HandleFunc(tt.pattern, func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("handled func"))
				})
			}

			req := httptest.NewRequest("GET", tt.pattern, nil)
			rr := httptest.NewRecorder()

			getCompleteHandler(server).ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rr.Code)
			}

			if rr.Body.String() != tt.expectedBody {
				t.Errorf("expected body %q, got %q", tt.expectedBody, rr.Body.String())
			}
		})
	}
}

func TestServer_Middleware_Mux(t *testing.T) {
	t.Run("middleware_actually_applied", func(t *testing.T) {
		server := mizu.NewServer("test-server")

		// Create middleware that adds observable behavior
		authMux := server.Use(func(next http.Handler) http.Handler {
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
		server.HandleFunc("/public", func(w http.ResponseWriter, r *http.Request) {
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

				getCompleteHandler(server).ServeHTTP(rr, req)

				if rr.Code != http.StatusOK {
					t.Errorf("expected status 200, got %d", rr.Code)
				}

				if rr.Body.String() != tt.expectedBody {
					t.Errorf("expected body %q, got %q", tt.expectedBody, rr.Body.String())
				}

				// Test middleware headers
				if tt.shouldHaveHeaders {
					if auth := rr.Header().Get("X-Auth"); auth != tt.expectedAuthHdr {
						t.Errorf("expected X-Auth header %q, got %q", tt.expectedAuthHdr, auth)
					}
					if log := rr.Header().Get("X-Log"); log != tt.expectedLogHdr {
						t.Errorf("expected X-Log header %q, got %q", tt.expectedLogHdr, log)
					}
				} else {
					if auth := rr.Header().Get("X-Auth"); auth != "" {
						t.Errorf("expected no X-Auth header, got %q", auth)
					}
					if log := rr.Header().Get("X-Log"); log != "" {
						t.Errorf("expected no X-Log header, got %q", log)
					}
				}
			})
		}
	})

	t.Run("base_handler_vs_complete_handler", func(t *testing.T) {
		server := mizu.NewServer("test-server")

		// Middleware that modifies response
		server.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware", "applied")
				next.ServeHTTP(w, r)
			})
		})

		server.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("test-response"))
		})

		req := httptest.NewRequest("GET", "/test", nil)

		// Test base handler (should NOT have middleware)
		t.Run("base_handler_no_middleware", func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.Handler().ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rr.Code)
			}

			if middleware := rr.Header().Get("X-Middleware"); middleware != "" {
				t.Errorf("base handler should not apply middleware, but got X-Middleware: %q", middleware)
			}
		})

		// Test complete handler (should HAVE middleware)
		t.Run("complete_handler_with_middleware", func(t *testing.T) {
			rr := httptest.NewRecorder()
			getCompleteHandler(server).ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rr.Code)
			}

			if middleware := rr.Header().Get("X-Middleware"); middleware != "applied" {
				t.Errorf("complete handler should apply middleware, expected X-Middleware: 'applied', got %q", middleware)
			}
		})
	})

	t.Run("middleware_execution_order", func(t *testing.T) {
		server := mizu.NewServer("test-server")
		var executionOrder []string

		// First middleware
		mux1 := server.Use(func(next http.Handler) http.Handler {
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

		getCompleteHandler(server).ServeHTTP(rr, req)

		expectedOrder := []string{
			"middleware1-before",
			"middleware2-before",
			"handler",
			"middleware2-after",
			"middleware1-after",
		}

		if len(executionOrder) != len(expectedOrder) {
			t.Fatalf("expected %d execution steps, got %d: %v", len(expectedOrder), len(executionOrder), executionOrder)
		}

		for i, expected := range expectedOrder {
			if executionOrder[i] != expected {
				t.Errorf("execution order[%d]: expected %q, got %q", i, expected, executionOrder[i])
			}
		}
	})
}

func TestServer_Middleware_Server(t *testing.T) {
	t.Run("middleware_function_composition", func(t *testing.T) {
		server := mizu.NewServer("test-server")

		// Add multiple middleware through different Use() calls
		server.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Bucket1", "applied")
				next.ServeHTTP(w, r)
			})
		})

		server.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Bucket2", "applied")
				next.ServeHTTP(w, r)
			})
		})

		// Create a test handler to apply Server.Middleware() to
		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("test-handler"))
		})

		// Get the middleware composer function
		middlewareComposer := server.Middleware()
		composedHandler := middlewareComposer(testHandler)

		// Test that the middleware composer applies all middlewares
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()

		composedHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		if body := rr.Body.String(); body != "test-handler" {
			t.Errorf("expected body 'test-handler', got %q", body)
		}

		// Verify that all middleware buckets were applied
		if bucket1 := rr.Header().Get("X-Bucket1"); bucket1 != "applied" {
			t.Errorf("expected X-Bucket1 header 'applied', got %q", bucket1)
		}

		if bucket2 := rr.Header().Get("X-Bucket2"); bucket2 != "applied" {
			t.Errorf("expected X-Bucket2 header 'applied', got %q", bucket2)
		}
	})

	t.Run("middleware_function_order", func(t *testing.T) {
		server := mizu.NewServer("test-server")
		var executionOrder []string

		server.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				executionOrder = append(executionOrder, "bucket1-before")
				next.ServeHTTP(w, r)
				executionOrder = append(executionOrder, "bucket1-after")
			})
		})

		server.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				executionOrder = append(executionOrder, "bucket2-before")
				next.ServeHTTP(w, r)
				executionOrder = append(executionOrder, "bucket2-after")
			})
		})

		// Test handler
		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "handler")
			_, _ = w.Write([]byte("test"))
		})

		// Apply Server.Middleware()
		middlewareComposer := server.Middleware()
		composedHandler := middlewareComposer(testHandler)

		// Reset and test execution order
		executionOrder = []string{}

		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		composedHandler.ServeHTTP(rr, req)

		// Verify execution order: buckets applied in reverse order (last bucket first)
		expectedOrder := []string{
			"bucket1-before", // First bucket added becomes outermost
			"bucket2-before", // Last bucket added becomes inner
			"handler",
			"bucket2-after",
			"bucket1-after",
		}

		if len(executionOrder) != len(expectedOrder) {
			t.Fatalf("expected %d execution steps, got %d: %v", len(expectedOrder), len(executionOrder), executionOrder)
		}

		for i, expected := range expectedOrder {
			if executionOrder[i] != expected {
				t.Errorf("execution order[%d]: expected %q, got %q", i, expected, executionOrder[i])
			}
		}
	})

	t.Run("middleware_function_empty_buckets", func(t *testing.T) {
		server := mizu.NewServer("test-server")

		// Get middleware composer without any Use() calls
		middlewareComposer := server.Middleware()

		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("no-middleware"))
		})

		composedHandler := middlewareComposer(testHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		composedHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		if body := rr.Body.String(); body != "no-middleware" {
			t.Errorf("expected body 'no-middleware', got %q", body)
		}

		// Should have no middleware headers
		if len(rr.Header()) > 0 && rr.Header().Get("content-type") != "text/plain; charset=utf-8" {
			t.Errorf("expected no headers with empty middleware, got %v", rr.Header())
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
			server := mizu.NewServer("test-server")

			for _, injector := range tt.injectors {
				server.InjectContext(injector)
			}

			var capturedContext context.Context
			server.HookOnExtractHandler(func(ctx context.Context, s *mizu.Server) {
				capturedContext = ctx
			})

			// Trigger handler extraction
			getCompleteHandler(server)

			for key, expected := range tt.expectedValues {
				if got := capturedContext.Value(key); got != expected {
					t.Errorf("expected context value %s=%v, got %v", key, expected, got)
				}
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
			server := mizu.NewServer("test-server")

			var startupCalls, extractHandlerCalls int
			var mu sync.Mutex

			for i := 0; i < tt.numStartupHooks; i++ {
				server.HookOnStartup(func(ctx context.Context, s *mizu.Server) {
					mu.Lock()
					startupCalls++
					mu.Unlock()
				})
			}

			for i := 0; i < tt.numExtractHandlerHooks; i++ {
				server.HookOnExtractHandler(func(ctx context.Context, s *mizu.Server) {
					mu.Lock()
					extractHandlerCalls++
					mu.Unlock()
				})
			}

			// Trigger extract handler hooks
			getCompleteHandler(server)

			mu.Lock()
			gotExtractHandlerCalls := extractHandlerCalls
			mu.Unlock()

			if gotExtractHandlerCalls != tt.expectedExtractHandlerCalls {
				t.Errorf("expected %d extract handler calls, got %d", tt.expectedExtractHandlerCalls, gotExtractHandlerCalls)
			}

			// Test startup hooks would require ServeContext which is harder to test
			// in unit tests due to network binding
		})
	}
}

func TestServer_Handler_CallsHooksEveryTime(t *testing.T) {
	server := mizu.NewServer("test-server")

	server.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("test"))
	})

	var hookCalls int
	server.HookOnExtractHandler(func(ctx context.Context, s *mizu.Server) {
		hookCalls++
	})

	// Call Handler multiple times
	handler1 := getCompleteHandler(server)
	handler2 := getCompleteHandler(server)

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

	if rr1.Body.String() != rr2.Body.String() {
		t.Errorf("handlers returned different responses: %q vs %q", rr1.Body.String(), rr2.Body.String())
	}
}

func TestServer_ConcurrentAccess(t *testing.T) {
	server := mizu.NewServer("test-server")

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
				server.HandleFunc(fmt.Sprintf("/concurrent/%d", id), func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("concurrent"))
				})
			case 1:
				server.InjectContext(func(ctx context.Context) context.Context {
					return context.WithValue(ctx, ctxkey(fmt.Sprintf("concurrent_%d", id)), id)
				})
			case 2:
				server.HookOnStartup(func(ctx context.Context, s *mizu.Server) {})
			case 3:
				server.HookOnExtractHandler(func(ctx context.Context, s *mizu.Server) {})
			}
		}(i)
	}

	wg.Wait()

	// Add a test handler after concurrent access
	server.HandleFunc("/test-after-concurrent", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("test"))
	})

	// Handler should still work after concurrent access
	handler := getCompleteHandler(server)
	req := httptest.NewRequest("GET", "/test-after-concurrent", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 after concurrent access, got %d", rr.Code)
	}
}

func TestServer_RootPattern(t *testing.T) {
	server := mizu.NewServer("test-server")

	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("root"))
	})

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	getCompleteHandler(server).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	if rr.Body.String() != "root" {
		t.Errorf("expected body 'root', got %q", rr.Body.String())
	}
}
