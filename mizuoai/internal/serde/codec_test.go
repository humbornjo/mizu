package serde

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestInternal_CodecDecode(t *testing.T) {
	type input struct {
		Path struct {
			Id string `json:"id"`
		} `json:"path"`
		Query struct {
			Limit int   `json:"limit"`
			Ids   []int `json:"ids"`
		} `json:"query"`
		Header struct {
			RequestId string `json:"X_Request_Id"`
		} `json:"header"`
		Body struct {
			Name string `json:"name"`
		} `json:"body"`
	}

	c, err := NewCodec[input, any]()
	if err != nil {
		t.Fatal(err)
	}
	newReq := func() *http.Request {
		req := httptest.NewRequest("POST", "/nodes/42?limit=10&ids=[1,2]", strings.NewReader(`{"name":"root"}`))
		req.SetPathValue("id", "42")
		req.Header.Set("X-Request-Id", "req-1")
		return req
	}

	var in input
	if err := c.Decode(newReq(), &in); err != nil {
		t.Fatal(err)
	}
	if in.Path.Id != "42" || in.Query.Limit != 10 || in.Header.RequestId != "req-1" || in.Body.Name != "root" {
		t.Fatalf("unexpected decode result: %+v", in)
	}
	if !reflect.DeepEqual(in.Query.Ids, []int{1, 2}) {
		t.Fatalf("unexpected ids: %v", in.Query.Ids)
	}

	_, rx := c.Split(httptest.NewRecorder(), newReq())
	got, err := rx.Xread()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("split read got %+v, want %+v", got, in)
	}
}

func TestInternal_NewCodec(t *testing.T) {
	type badQuery struct {
		Query int `json:"query"`
	}
	if _, err := NewCodec[badQuery, any](); err == nil {
		t.Fatal("expected error for non-struct query")
	}

	type bodyAndForm struct {
		Body []byte         `json:"body"`
		Form map[string]any `json:"form"`
	}
	if _, err := NewCodec[bodyAndForm, any](); err == nil {
		t.Fatal("expected error for body and form together")
	}
}

func TestInternal_CodecDecodeForm(t *testing.T) {
	type input struct {
		Form struct {
			Name    string   `json:"name"`
			Count   int      `json:"count"`
			Tags    []string `json:"tags"`
			Package []byte   `json:"package"`
		} `json:"form"`
	}

	t.Run("urlencoded", func(t *testing.T) {
		c, err := NewCodec[input, any]()
		if err != nil {
			t.Fatal(err)
		}
		body := "name=root&count=3&tags=" + url.QueryEscape(`["a","b"]`)
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		var in input
		if err := c.Decode(req, &in); err != nil {
			t.Fatal(err)
		}
		if in.Form.Name != "root" || in.Form.Count != 3 {
			t.Fatalf("unexpected decode result: %+v", in.Form)
		}
		if !reflect.DeepEqual(in.Form.Tags, []string{"a", "b"}) {
			t.Fatalf("unexpected tags: %v", in.Form.Tags)
		}
	})

	t.Run("multipart", func(t *testing.T) {
		c, err := NewCodec[input, any]()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		if err := w.WriteField("name", "root"); err != nil {
			t.Fatal(err)
		}
		part, err := w.CreateFormFile("package", "p.gz")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("gzip-bytes")); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest("POST", "/", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())

		var in input
		if err := c.Decode(req, &in); err != nil {
			t.Fatal(err)
		}
		if in.Form.Name != "root" {
			t.Fatalf("unexpected decode result: %+v", in.Form)
		}
		if !bytes.Equal(in.Form.Package, []byte("gzip-bytes")) {
			t.Fatalf("unexpected package: %q", in.Form.Package)
		}
	})
}

func TestInternal_SetFieldValue(t *testing.T) {
	type inner struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	tests := []struct {
		name    string
		raw     string
		want    any
		wantErr bool
	}{
		{"string", "hello", "hello", false},
		{"bool", "true", true, false},
		{"int", "42", 42, false},
		{"uint", "7", uint(7), false},
		{"float", "1.5", 1.5, false},
		{"struct from JSON object", `{"a":1,"b":"x"}`, inner{A: 1, B: "x"}, false},
		{"slice from JSON array", "[1,2,3]", []int{1, 2, 3}, false},
		{"string slice from JSON array", `["a","b"]`, []string{"a", "b"}, false},
		{"nested slice from JSON array", "[[1,2],[3,4]]", [][]int{{1, 2}, {3, 4}}, false},
		{"map from JSON object", `{"env":"prod"}`, map[string]string{"env": "prod"}, false},
		{"map with composite values", `{"a":[1,2]}`, map[string][]int{"a": {1, 2}}, false},
		{"bytes stay raw not base64", "hello", []byte("hello"), false},
		{"int parse error", "abc", 0, true},
		{"malformed JSON array", "[1,2", []int{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := reflect.New(reflect.TypeOf(tt.want)).Elem()
			err := SetFieldValue(target, tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := target.Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
