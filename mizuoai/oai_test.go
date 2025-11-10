package mizuoai_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuoai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			srv := mizu.NewServer("test")
			if err := mizuoai.Initialize(srv, "test_title"); err != nil {
				t.Fatal(err)
			}

			var receivedInput *TestInputBodyJSON
			var err error

			mizuoai.Get(srv, "/users/{id}", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputBodyJSON]) {
				receivedInput, err = rx.MizuRead()
			})

			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, tc.request)
			assert.Nil(t, err)
			assert.Equal(t, tc.expected.Query, receivedInput.Query)
			assert.Equal(t, tc.expected.Body, receivedInput.Body)
			assert.Equal(t, tc.expected.Path, receivedInput.Path)
			assert.Equal(t, tc.expected.Header, receivedInput.Header)
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
			srv := mizu.NewServer("test")
			if err := mizuoai.Initialize(srv, "test_title"); err != nil {
				t.Fatal(err)
			}

			var receivedInput *TestInputBodyString
			var err error

			mizuoai.Get(srv, "/test", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputBodyString]) {
				receivedInput, err = rx.MizuRead()
			})

			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, tc.request)
			assert.Nil(t, err)
			assert.Equal(t, tc.expected, receivedInput)
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
			srv := mizu.NewServer("test")
			if err := mizuoai.Initialize(srv, "test_title"); err != nil {
				t.Fatal(err)
			}

			var receivedInput *TestInputBodyInt
			var err error

			mizuoai.Get(srv, "/int", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputBodyInt]) {
				receivedInput, err = rx.MizuRead()
			})

			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, tc.request)
			assert.Nil(t, err)
			assert.Equal(t, tc.expected, receivedInput)
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
			srv := mizu.NewServer("test")
			if err := mizuoai.Initialize(srv, "test_title"); err != nil {
				t.Fatal(err)
			}

			var receivedInput *TestInputBodyFloat
			var err error

			mizuoai.Get(srv, "/float", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputBodyFloat]) {
				receivedInput, err = rx.MizuRead()
			})

			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, tc.request)
			assert.Nil(t, err)
			assert.Equal(t, tc.expected, receivedInput)
		})
	}
}

type TestInputForm struct {
	Form struct {
		Name      string `form:"name"`
		Email     string `form:"email"`
		Age       int    `form:"age"`
		Subscribe bool   `form:"subscribe"`
	} `mizu:"form"`
}

func TestMizuOai_Rx_Read_FormData(t *testing.T) {
	testCases := []struct {
		name     string
		request  *http.Request
		expected *TestInputForm
	}{
		{
			name: "Form Data Request with nested struct",
			request: func() *http.Request {
				// Create multipart form data
				body := bytes.NewBuffer(nil)
				writer := multipart.NewWriter(body)

				fieldName, err := writer.CreateFormField("name")
				require.NoError(t, err)
				_, err = fieldName.Write([]byte("John Doe"))
				require.NoError(t, err)

				fieldAge, err := writer.CreateFormField("age")
				require.NoError(t, err)
				_, err = fieldAge.Write([]byte("25"))
				require.NoError(t, err)

				fieldEmail, err := writer.CreateFormField("email")
				require.NoError(t, err)
				_, err = fieldEmail.Write([]byte("john@example.com"))
				require.NoError(t, err)

				fieldSubscribe, err := writer.CreateFormField("subscribe")
				require.NoError(t, err)
				_, err = fieldSubscribe.Write([]byte("true"))
				require.NoError(t, err)

				req := httptest.NewRequest("POST", "/form", body)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req
			}(),
			expected: &TestInputForm{
				Form: struct {
					Name      string `form:"name"`
					Email     string `form:"email"`
					Age       int    `form:"age"`
					Subscribe bool   `form:"subscribe"`
				}{
					Name:      "John Doe",
					Email:     "john@example.com",
					Age:       25,
					Subscribe: true,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := mizu.NewServer("test")
			if err := mizuoai.Initialize(srv, "test_title"); err != nil {
				t.Fatal(err)
			}

			var receivedInput *TestInputForm
			var err error

			mizuoai.Post(srv, "/form", func(tx mizuoai.Tx[string], rx mizuoai.Rx[TestInputForm]) {
				receivedInput, err = rx.MizuRead()
			})

			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, tc.request)
			assert.Nil(t, err)
			assert.Equal(t, tc.expected, receivedInput)
		})
	}
}
