package mizu_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/stretchr/testify/assert"
)

func TestServer_HTTPMethods(t *testing.T) {
	testCases := []struct {
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := mizu.NewServer("test-server")

			handler := func(w http.ResponseWriter, r *http.Request) {
				if tc.method == "HEAD" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.method + " " + tc.pattern))
			}

			tc.setupHandler(srv, tc.pattern, handler)

			req := httptest.NewRequest(tc.requestMethod, tc.requestPath, nil)
			rr := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rr, req)
			assert.Equal(t, tc.expectedStatus, rr.Code)
			if tc.expectedBody != "" {
				assert.Contains(t, rr.Body.String(), tc.expectedBody)
			}
		})
	}
}

func TestServer_Handle_And_HandleFunc(t *testing.T) {
	testCases := []struct {
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := mizu.NewServer("test-server")

			if tc.useHandle {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("handled"))
				})
				srv.Handle(tc.pattern, handler)
			} else {
				srv.HandleFunc(tc.pattern, func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("handled func"))
				})
			}

			req := httptest.NewRequest("GET", tc.pattern, nil)
			rr := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tc.expectedBody, rr.Body.String())
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

		testCases := []struct {
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

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", tc.path, nil)
				rr := httptest.NewRecorder()

				srv.Handler().ServeHTTP(rr, req)
				assert.Equal(t, http.StatusOK, rr.Code)
				assert.Equal(t, tc.expectedBody, rr.Body.String())

				// Test middleware headers
				if tc.shouldHaveHeaders {
					assert.Equal(t, tc.expectedAuthHdr, rr.Header().Get("X-Auth"))
					assert.Equal(t, tc.expectedLogHdr, rr.Header().Get("X-Log"))
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

func TestServer_Hook_StoreAndRetrieveValue(t *testing.T) {
	srv := mizu.NewServer("test-server")
	type testKey string
	type testValue struct {
		data string
	}

	key := testKey("test-key")
	expectedValue := &testValue{data: "test-data"}

	// Test storing and retrieving a value
	retrievedValue := mizu.Hook(srv, key, expectedValue)

	assert.NotNil(t, retrievedValue)
	assert.Equal(t, expectedValue, retrievedValue)
	assert.Equal(t, "test-data", retrievedValue.data)
}

func TestServer_Hook_ReturnsNewForNonExistentKey(t *testing.T) {
	srv := mizu.NewServer("test-server")

	type testKey string
	key := testKey("non-existent-key")
	// Should return nil for non-existent key
	result := mizu.Hook(srv, key, (*struct{})(nil))
	assert.NotNil(t, result)
}

func TestServer_Hook_WithHookHandler(t *testing.T) {
	srv := mizu.NewServer("test-server")
	handlerCalled := false
	type testKey string
	type testValue struct {
		data string
	}

	key := testKey("handler-key")
	value := &testValue{data: "handler-data"}

	// Register hook with WithHookHandler option
	_ = mizu.Hook(srv, key, value, mizu.WithHookHandler(func(s *mizu.Server) {
		handlerCalled = true
		assert.Equal(t, srv.Name(), "test-server")
	}))

	// Handler() should trigger the hookHandler
	_ = srv.Handler()

	assert.True(t, handlerCalled, "WithHookHandler should have been called")
}

func TestServer_Hook_MultipleHooks(t *testing.T) {
	srv := mizu.NewServer("test-server")
	handler1Called := false
	handler2Called := false

	type testKey string
	type testValue struct {
		data string
	}

	key1 := testKey("key1")
	key2 := testKey("key2")
	value1 := &testValue{data: "value1"}
	value2 := &testValue{data: "value2"}

	// Register multiple hooks with handler options
	_ = mizu.Hook(srv, key1, value1,
		mizu.WithHookHandler(func(s *mizu.Server) {
			handler1Called = true
		}),
	)

	_ = mizu.Hook(srv, key2, value2,
		mizu.WithHookHandler(func(s *mizu.Server) {
			handler2Called = true
		}),
	)

	// Trigger handler hooks
	_ = srv.Handler()
	assert.True(t, handler1Called, "handler1 should be called")
	assert.True(t, handler2Called, "handler2 should be called")
}

func TestServer_Hook_Concurrent(t *testing.T) {
	srv := mizu.NewServer("test-server")
	type testKey string
	type testValue struct {
		data string
	}

	const numGoroutines = 10
	wg := sync.WaitGroup{}

	// Launch multiple goroutines to register hooks concurrently
	for i := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			key := testKey(fmt.Sprintf("concurrent-key-%d", goroutineID))
			value := &testValue{data: fmt.Sprintf("concurrent-value-%d", goroutineID)}

			// Store the value
			mizu.Hook(srv, key, value)
		}(i)
	}
	wg.Wait()
}
