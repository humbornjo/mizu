package mizuoai

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
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
				req := httptest.NewRequest("POST", "/users/123?name=John&age=30&admin=true",
					bytes.NewBufferString(`{"email": "test@example.com", "role": "user"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer xyz")
				req.SetPathValue("id", "123")
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
			rx := Rx[TestInputBodyJSON]{r: tc.request, read: func(r *http.Request) *TestInputBodyJSON {
				input := new(TestInputBodyJSON)
				for _, parseFn := range genParser[TestInputBodyJSON]() {
					parseFn(r, input)
				}
				return input
			}}
			result := rx.Read()

			// A simple way to compare, ignoring the body struct if it has been consumed
			if !reflect.DeepEqual(result.Query, tc.expected.Query) {
				t.Errorf("Query mismatch: got %+v, want %+v", result.Query, tc.expected.Query)
			}
			if !reflect.DeepEqual(result.Path, tc.expected.Path) {
				t.Errorf("Path mismatch: got %+v, want %+v", result.Path, tc.expected.Path)
			}
			if !reflect.DeepEqual(result.Header, tc.expected.Header) {
				t.Errorf("Header mismatch: got %+v, want %+v", result.Header, tc.expected.Header)
			}
			if tc.name == "JSON Body Request" && !reflect.DeepEqual(result.Body, tc.expected.Body) {
				t.Errorf("Body mismatch: got %+v, want %+v", result.Body, tc.expected.Body)
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
				req := httptest.NewRequest("POST", "/",
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
			rx := Rx[TestInputBodyString]{r: tc.request, read: func(r *http.Request) *TestInputBodyString {
				input := new(TestInputBodyString)
				for _, parseFn := range genParser[TestInputBodyString]() {
					parseFn(r, input)
				}
				return input
			}}
			result := rx.Read()

			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Body mismatch: got %+v, want %+v", result, tc.expected)
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
				req := httptest.NewRequest("POST", "/",
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
			rx := Rx[TestInputBodyInt]{r: tc.request, read: func(r *http.Request) *TestInputBodyInt {
				input := new(TestInputBodyInt)
				for _, parseFn := range genParser[TestInputBodyInt]() {
					parseFn(r, input)
				}
				return input
			}}
			result := rx.Read()

			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Body mismatch: got %+v, want %+v", result, tc.expected)
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
				req := httptest.NewRequest("POST", "/",
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
			rx := Rx[TestInputBodyFloat]{r: tc.request, read: func(r *http.Request) *TestInputBodyFloat {
				input := new(TestInputBodyFloat)
				for _, parseFn := range genParser[TestInputBodyFloat]() {
					parseFn(r, input)
				}
				return input
			}}
			result := rx.Read()

			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Body mismatch: got %+v, want %+v", result, tc.expected)
			}
		})
	}
}
