package mizuoai

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/stretchr/testify/require"
)

type TestInputSchema struct {
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
	Form struct {
		Token string `form:"token"`
	} `mizu:"form"`
}

func TestMizuOai_Libopenapi(t *testing.T) {
	doc, err := libopenapi.NewDocument([]byte("openapi: 3.0"))
	require.NoError(t, err)

	model, errs := doc.BuildV3Model()
	for _, err := range errs {
		require.NoError(t, err)
	}

	model.Model.Paths.PathItems.Set("/users/{id}", &v3.PathItem{})

	t.Log(doc)
}

func TestMizuOai_Rx_Read(t *testing.T) {
	testCases := []struct {
		name     string
		request  *http.Request
		expected *TestInputSchema
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
			expected: &TestInputSchema{
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
		{
			name: "Form Body Request",
			request: func() *http.Request {
				form := url.Values{}
				form.Add("token", "secret-token")
				req := httptest.NewRequest("POST", "/users/456?name=Jane&age=25&admin=false", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Authorization", "Bearer abc")
				req.SetPathValue("id", "456")
				return req
			}(),
			expected: &TestInputSchema{
				Query: struct {
					Name  string `query:"name"`
					Age   int    `query:"age"`
					Admin bool   `query:"admin"`
				}{Name: "Jane", Age: 25, Admin: false},
				Path: struct {
					ID string `path:"id"`
				}{ID: "456"},
				Header: struct {
					Authorization string `header:"Authorization"`
					ContentType   string `header:"Content-Type"`
				}{Authorization: "Bearer abc", ContentType: "application/x-www-form-urlencoded"},
				Form: struct {
					Token string `form:"token"`
				}{Token: "secret-token"},
			},
		},
		{
			name: "GET Request (No Body)",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/items/789?name=Widget&age=1&admin=false", nil)
				req.Header.Set("Authorization", "Bearer qwe")
				req.SetPathValue("id", "789")
				return req
			}(),
			expected: &TestInputSchema{
				Query: struct {
					Name  string `query:"name"`
					Age   int    `query:"age"`
					Admin bool   `query:"admin"`
				}{Name: "Widget", Age: 1, Admin: false},
				Path: struct {
					ID string `path:"id"`
				}{ID: "789"},
				Header: struct {
					Authorization string `header:"Authorization"`
					ContentType   string `header:"Content-Type"`
				}{Authorization: "Bearer qwe"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rx := Rx[TestInputSchema]{r: tc.request}
			result := rx.Read()

			// For form requests, we need to parse it to make it available for the test assertion.
			if strings.Contains(tc.request.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
				// Re-parse form for assertion check
				tc.request.ParseForm()
				if result.Form.Token != tc.request.FormValue("token") {
					t.Errorf("Form token not parsed correctly. Got %s, want %s", result.Form.Token, tc.request.FormValue("token"))
				}
			}

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
			if tc.name == "Form Body Request" && !reflect.DeepEqual(result.Form, tc.expected.Form) {
				t.Errorf("Form mismatch: got %+v, want %+v", result.Form, tc.expected.Form)
			}
		})
	}
}

func TestMizuOai_Rx_Read_PlainType(t *testing.T) {
	t.Run("Plain String Body", func(t *testing.T) {
		body := "this is a plain string body"
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "text/plain")

		rx := Rx[string]{r: req}
		result := rx.Read()

		if result == nil {
			t.Fatal("Read() returned nil")
		}
		if *result != body {
			t.Errorf("Body mismatch: got %q, want %q", *result, body)
		}
	})

	t.Run("Plain JSON-encoded Int Body", func(t *testing.T) {
		body := "12345"
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rx := Rx[int]{r: req}
		result := rx.Read()

		if result == nil {
			t.Fatal("Read() returned nil")
		}
		if *result != 12345 {
			t.Errorf("Body mismatch: got %d, want %d", *result, 12345)
		}
	})
}
