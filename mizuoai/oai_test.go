package mizuoai_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuoai"
)

type TestInputBodyJSON struct {
	Query struct {
		Name  string `query:"name"`
		Age   int    `query:"age"`
		Admin bool   `query:"admin"`
	} `mizu:"query"`
	Body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	} `mizu:"body"`
	Path struct {
		ID string `path:"id"`
	} `mizu:"path"`
	Header struct {
		Authorization string `header:"Authorization"`
		ContentType   string `header:"Content-Type"`
	} `mizu:"header"`
}

type TestInputBodyString struct {
	Message string `mizu:"body"`
}

type TestInputBodyInt struct {
	Value int `mizu:"body"`
}

type TestInputBodyFloat struct {
	Value float64 `mizu:"body"`
}

func TestMizuOai_Rx_Read_BodyJSON(t *testing.T) {
	testCases := []struct {
		name     string
		request  *http.Request
		expected *TestInputBodyJSON
	}{
		{
			name: "JSON Body Request",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/users/123?name=John&age=30&admin=true",
					bytes.NewBufferString(`{"email": "test@example.com", "role": "user"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer xyz")
				return req
			}(),
			expected: &TestInputBodyJSON{
				Query: struct {
					Name  string `query:"name"`
					Age   int    `query:"age"`
					Admin bool   `query:"admin"`
				}{Name: "John", Age: 30, Admin: true},
				Body: struct {
					Email string `json:"email"`
					Role  string `json:"role"`
				}{Email: "test@example.com", Role: "user"},
				Path: struct {
					ID string `path:"id"`
				}{ID: "123"},
				Header: struct {
					Authorization string `header:"Authorization"`
					ContentType   string `header:"Content-Type"`
				}{Authorization: "Bearer xyz", ContentType: "application/json"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := mizu.NewServer("test")
			scope := mizuoai.NewOai(server, "test_title")

			var receivedInput *TestInputBodyJSON

			mizuoai.Get(scope, "/users/{id}", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputBodyJSON]) {
				receivedInput = rx.MizuRead()
			})

			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, tc.request)

			if receivedInput == nil {
				t.Fatal("handler was not called")
			}

			if !reflect.DeepEqual(receivedInput.Query, tc.expected.Query) {
				t.Errorf("Query mismatch: got %+v, want %+v", receivedInput.Query, tc.expected.Query)
			}
			if !reflect.DeepEqual(receivedInput.Path, tc.expected.Path) {
				t.Errorf("Path mismatch: got %+v, want %+v", receivedInput.Path, tc.expected.Path)
			}
			if !reflect.DeepEqual(receivedInput.Header, tc.expected.Header) {
				t.Errorf("Header mismatch: got %+v, want %+v", receivedInput.Header, tc.expected.Header)
			}
			if !reflect.DeepEqual(receivedInput.Body, tc.expected.Body) {
				t.Errorf("Body mismatch: got %+v, want %+v", receivedInput.Body, tc.expected.Body)
			}
		})
	}
}

func TestMizuOai_Rx_Read_BodyString(t *testing.T) {
	testCases := []struct {
		name     string
		request  *http.Request
		expected *TestInputBodyString
	}{
		{
			name: "String Body Request",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/test",
					bytes.NewBufferString("hello world"))
				req.Header.Set("Content-Type", "text/plain")
				return req
			}(),
			expected: &TestInputBodyString{
				Message: "hello world",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := mizu.NewServer("test")
			scope := mizuoai.NewOai(server, "test_title")

			var receivedInput *TestInputBodyString

			mizuoai.Get(scope, "/test", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputBodyString]) {
				receivedInput = rx.MizuRead()
			})

			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, tc.request)

			if receivedInput == nil {
				t.Fatal("handler was not called")
			}

			if !reflect.DeepEqual(receivedInput, tc.expected) {
				t.Errorf("Body mismatch: got %+v, want %+v", receivedInput, tc.expected)
			}
		})
	}
}

func TestMizuOai_Rx_Read_BodyInt(t *testing.T) {
	testCases := []struct {
		name     string
		request  *http.Request
		expected *TestInputBodyInt
	}{
		{
			name: "Int Body Request",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/int",
					bytes.NewBufferString(`12345`))
				req.Header.Set("Content-Type", "application/json")
				return req
			}(),
			expected: &TestInputBodyInt{
				Value: 12345,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := mizu.NewServer("test")
			scope := mizuoai.NewOai(server, "test_title")

			var receivedInput *TestInputBodyInt

			mizuoai.Get(scope, "/int", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputBodyInt]) {
				receivedInput = rx.MizuRead()
			})

			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, tc.request)

			if receivedInput == nil {
				t.Fatal("handler was not called")
			}

			if !reflect.DeepEqual(receivedInput, tc.expected) {
				t.Errorf("Body mismatch: got %+v, want %+v", receivedInput, tc.expected)
			}
		})
	}
}

func TestMizuOai_Rx_Read_BodyFloat(t *testing.T) {
	testCases := []struct {
		name     string
		request  *http.Request
		expected *TestInputBodyFloat
	}{
		{
			name: "Float Body Request",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/float",
					bytes.NewBufferString(`123.45`))
				req.Header.Set("Content-Type", "application/json")
				return req
			}(),
			expected: &TestInputBodyFloat{
				Value: 123.45,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := mizu.NewServer("test")
			scope := mizuoai.NewOai(server, "test_title")

			var receivedInput *TestInputBodyFloat

			mizuoai.Get(scope, "/float", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputBodyFloat]) {
				receivedInput = rx.MizuRead()
			})

			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, tc.request)

			if receivedInput == nil {
				t.Fatal("handler was not called")
			}

			if !reflect.DeepEqual(receivedInput, tc.expected) {
				t.Errorf("Body mismatch: got %+v, want %+v", receivedInput, tc.expected)
			}
		})
	}
}
