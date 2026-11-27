package serde

import (
	"bytes"
	"errors"
	"io"
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

func TestInternal_CodecDecodeQuerySlices(t *testing.T) {
	type input struct {
		Query struct {
			Roles []string `json:"roles[]"`
			Ids   []int    `json:"ids"`
		} `json:"query"`
	}

	tests := []struct {
		name      string
		target    string
		wantRoles []string
		wantIds   []int
		wantErr   bool
	}{
		{
			name:      "repeated strings",
			target:    "/?roles[]=owner&roles[]=membership_admin",
			wantRoles: []string{"owner", "membership_admin"},
		},
		{
			name:      "single string",
			target:    "/?roles[]=owner",
			wantRoles: []string{"owner"},
		},
		{
			name:    "repeated integers",
			target:  "/?ids=1&ids=2",
			wantIds: []int{1, 2},
		},
		{
			name:      "JSON array",
			target:    "/?roles[]=" + url.QueryEscape(`["owner","membership_admin"]`),
			wantRoles: []string{"owner", "membership_admin"},
		},
		{
			name:    "malformed JSON array",
			target:  "/?ids=[1,2",
			wantErr: true,
		},
	}

	c, err := NewCodec[input, any]()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			_, rx := c.Split(httptest.NewRecorder(), req)
			got, err := rx.Xread()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Query.Roles, tt.wantRoles) {
				t.Fatalf("got roles %v, want %v", got.Query.Roles, tt.wantRoles)
			}
			if !reflect.DeepEqual(got.Query.Ids, tt.wantIds) {
				t.Fatalf("got ids %v, want %v", got.Query.Ids, tt.wantIds)
			}
		})
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

type errStreamReader struct {
	data []byte
	err  error
	read bool
}

func (r *errStreamReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, r.err
	}
	r.read = true
	n := copy(p, r.data)
	return n, r.err
}

func (r *errStreamReader) Close() error {
	return nil
}

func TestSerde_SetStreamFieldValueScalarReadError(t *testing.T) {
	tests := []struct {
		name    string
		stream  io.ReadCloser
		target  any
		want    any
		wantErr error
	}{
		{
			name:    "int prefix with unexpected EOF",
			stream:  &errStreamReader{data: []byte("12"), err: io.ErrUnexpectedEOF},
			target:  0,
			want:    0,
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name:    "string prefix with unexpected EOF",
			stream:  &errStreamReader{data: []byte("12"), err: io.ErrUnexpectedEOF},
			target:  "",
			want:    "",
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name:   "int with clean EOF",
			stream: io.NopCloser(strings.NewReader("12")),
			target: 0,
			want:   12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := reflect.New(reflect.TypeOf(tt.target)).Elem()
			err := SetStreamFieldValue(field, tt.stream)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got err %v, want %v", err, tt.wantErr)
				}
				if got := field.Interface(); !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("partial value accepted: got %v, want %v", got, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := field.Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("codec decode scalar body error", func(t *testing.T) {
		type input struct {
			Body int `json:"body"`
		}
		c, err := NewCodec[input, any]()
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Body = &errStreamReader{data: []byte("12"), err: io.ErrUnexpectedEOF}
		var in input
		if err := c.Decode(req, &in); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("got err %v, want %v", err, io.ErrUnexpectedEOF)
		}
		if in.Body != 0 {
			t.Fatalf("partial body accepted: got %d, want 0", in.Body)
		}
	})
}

func TestSerde_SetStreamFieldValueMaxBytes(t *testing.T) {
	type bytesBodyInput struct {
		Body []byte `json:"body"`
	}
	type stringBodyInput struct {
		Body string `json:"body"`
	}
	type formInput struct {
		Form struct {
			Payload []byte `json:"payload"`
		} `json:"form"`
	}

	bytesCodec, err := NewCodec[bytesBodyInput, any]()
	if err != nil {
		t.Fatal(err)
	}
	stringCodec, err := NewCodec[stringBodyInput, any]()
	if err != nil {
		t.Fatal(err)
	}
	formCodec, err := NewCodec[formInput, any]()
	if err != nil {
		t.Fatal(err)
	}

	newMultipartReq := func(size int64) *http.Request {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		part, err := w.CreateFormFile("payload", "blob.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(bytes.Repeat([]byte("a"), int(size))); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		return req
	}

	tests := []struct {
		name    string
		decode  func() (int, error)
		wantLen int
		wantErr error
	}{
		{
			name: "bytes body 1 MiB",
			decode: func() (int, error) {
				req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte("a"), 1<<20)))
				var in bytesBodyInput
				err := bytesCodec.Decode(req, &in)
				return len(in.Body), err
			},
			wantLen: 1 << 20,
		},
		{
			name: "bytes body at exact limit",
			decode: func() (int, error) {
				req := httptest.NewRequest(http.MethodPost, "/", io.LimitReader(bytes.NewReader(bytes.Repeat([]byte("a"), int(MAX_STREAM_BYTES))), MAX_STREAM_BYTES))
				var in bytesBodyInput
				err := bytesCodec.Decode(req, &in)
				return len(in.Body), err
			},
			wantLen: int(MAX_STREAM_BYTES),
		},
		{
			name: "bytes body exceeds limit",
			decode: func() (int, error) {
				req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte("a"), int(MAX_STREAM_BYTES+1))))
				var in bytesBodyInput
				err := bytesCodec.Decode(req, &in)
				return len(in.Body), err
			},
			wantErr: ERR_STREAM_TOO_LARGE,
		},
		{
			name: "scalar string body exceeds limit",
			decode: func() (int, error) {
				req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte("a"), int(MAX_STREAM_BYTES+1))))
				var in stringBodyInput
				err := stringCodec.Decode(req, &in)
				return len(in.Body), err
			},
			wantErr: ERR_STREAM_TOO_LARGE,
		},
		{
			name: "multipart part 1 MiB",
			decode: func() (int, error) {
				var in formInput
				err := formCodec.Decode(newMultipartReq(1<<20), &in)
				return len(in.Form.Payload), err
			},
			wantLen: 1 << 20,
		},
		{
			name: "multipart part exceeds limit",
			decode: func() (int, error) {
				var in formInput
				err := formCodec.Decode(newMultipartReq(MAX_STREAM_BYTES+1), &in)
				return len(in.Form.Payload), err
			},
			wantErr: ERR_STREAM_TOO_LARGE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLen, err := tt.decode()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got err %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if gotLen != tt.wantLen {
				t.Fatalf("got len %d, want %d", gotLen, tt.wantLen)
			}
		})
	}
}
