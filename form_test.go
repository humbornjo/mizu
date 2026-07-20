package mizu_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type formPart struct {
	name     string
	filename string
	data     []byte
}

type trackingReadCloser struct {
	io.Reader
	readBytes int64
	closes    int
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.readBytes += int64(n)
	return n, err
}

func (r *trackingReadCloser) Close() error {
	r.closes++
	return nil
}

func newMultipartRequest(t *testing.T, parts ...formPart) (*http.Request, *trackingReadCloser, int) {
	t.Helper()

	var content bytes.Buffer
	writer := multipart.NewWriter(&content)
	for _, item := range parts {
		var part io.Writer
		var err error
		if item.filename == "" {
			part, err = writer.CreateFormField(item.name)
		} else {
			part, err = writer.CreateFormFile(item.name, item.filename)
		}
		require.NoError(t, err)
		_, err = part.Write(item.data)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	body := &trackingReadCloser{Reader: bytes.NewReader(content.Bytes())}
	request := httptest.NewRequest(http.MethodPost, "/upload", nil)
	request.Body = body
	request.ContentLength = int64(content.Len())
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request, body, content.Len()
}

type formAlias string
type formNumber int16
type formBytes []byte

type upperText string

func (v *upperText) UnmarshalText(text []byte) error {
	*v = upperText(strings.ToUpper(string(text)))
	return nil
}

type typedUploadForm struct {
	Title    formAlias `form:"title" json:"ignored_title" required:"true"`
	Enabled  bool      `json:"enabled"`
	Count    formNumber
	Unsigned uint16 `form:"unsigned"`
	Ratio    float32
	Data     formBytes
	Pointer  *int
	Code     upperText
	Labels   []formAlias  `form:"label"`
	Texts    []*upperText `form:"text"`
	Ignored  string       `form:"-"`
	Trailing string       `form:"trailing" required:"true"`
}

func TestMizu_NewFormReader(t *testing.T) {
	fileData := bytes.Repeat([]byte("streamed upload\n"), 8*1024)
	request, body, bodySize := newMultipartRequest(t,
		formPart{name: "title", data: []byte("release")},
		formPart{name: "enabled", data: []byte("true")},
		formPart{name: "Count", data: []byte("-12")},
		formPart{name: "unsigned", data: []byte("42")},
		formPart{name: "Ratio", data: []byte("1.25")},
		formPart{name: "Data", data: []byte{0, 1, 2}},
		formPart{name: "Pointer", data: []byte("7")},
		formPart{name: "Code", data: []byte("mixed")},
		formPart{name: "label", data: []byte("first")},
		formPart{name: "label", data: []byte("second")},
		formPart{name: "text", data: []byte("lower")},
		formPart{name: "ignored_title", data: []byte("wrong")},
		formPart{name: "file", filename: "package.txt", data: fileData},
		formPart{name: "trailing", data: []byte("complete")},
	)

	var fields typedUploadForm
	form, err := mizu.NewFormReader("file", request, &fields)
	require.NoError(t, err)
	defer form.Close()
	assert.Zero(t, body.readBytes, "constructing the form reader must not read the request body")

	file, purge, err := form.File()
	require.NoError(t, err)
	assert.Equal(t, "package.txt", file.FileName())
	assert.Less(t, body.readBytes, int64(bodySize), "finding the file must not buffer the whole upload")
	assert.Empty(t, fields.Trailing)

	reader := mizu.NewFileReader(file, mizu.WithFileLimitBytes(int64(len(fileData))))
	actual, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, fileData, actual)
	require.NoError(t, purge())

	checksum := sha256.Sum256(fileData)
	assert.Equal(t, hex.EncodeToString(checksum[:]), reader.Checksum())
	assert.Equal(t, int64(len(fileData)), reader.ReadSize())
	assert.Equal(t, "text/plain; charset=utf-8", reader.ContentType())
	assert.Equal(t, formAlias("release"), fields.Title)
	assert.True(t, fields.Enabled)
	assert.Equal(t, formNumber(-12), fields.Count)
	assert.Equal(t, uint16(42), fields.Unsigned)
	assert.Equal(t, float32(1.25), fields.Ratio)
	assert.Equal(t, formBytes{0, 1, 2}, fields.Data)
	require.NotNil(t, fields.Pointer)
	assert.Equal(t, 7, *fields.Pointer)
	assert.Equal(t, upperText("MIXED"), fields.Code)
	assert.Equal(t, []formAlias{"first", "second"}, fields.Labels)
	require.Len(t, fields.Texts, 1)
	assert.Equal(t, upperText("LOWER"), *fields.Texts[0])
	assert.Empty(t, fields.Ignored)
	assert.Equal(t, "complete", fields.Trailing)
}

func TestMizu_FormReaderNextPart(t *testing.T) {
	type fields struct {
		Known   string `form:"known"`
		Ignored string `form:"-"`
	}

	request, _, _ := newMultipartRequest(t,
		formPart{name: "unknown", data: []byte("visible")},
		formPart{name: "Ignored", data: []byte("also visible")},
		formPart{name: "known", data: []byte("decoded")},
	)
	var message fields
	form, err := mizu.NewFormReader("file", request, &message)
	require.NoError(t, err)
	defer form.Close()

	unknown, err := form.NextPart()
	require.NoError(t, err)
	data, err := io.ReadAll(unknown)
	require.NoError(t, err)
	assert.Equal(t, "visible", string(data))

	ignored, err := form.NextPart()
	require.NoError(t, err)
	data, err = io.ReadAll(ignored)
	require.NoError(t, err)
	assert.Equal(t, "also visible", string(data))

	known, err := form.NextPart()
	require.NoError(t, err)
	data, err = io.ReadAll(known)
	require.NoError(t, err)
	assert.Empty(t, data, "declared fields are decoded and consumed")
	assert.Equal(t, "decoded", message.Known)
	assert.Empty(t, message.Ignored)

	_, err = form.NextPart()
	assert.ErrorIs(t, err, io.EOF)
}

func TestMizu_NewFormReaderValidation(t *testing.T) {
	type validForm struct{}
	type conflictingForm struct {
		File string `form:"file"`
	}
	type duplicateForm struct {
		First  string `form:"same"`
		Second string `json:"same"`
	}
	type invalidRequiredForm struct {
		Name string `required:"sometimes"`
	}

	validRequest := func(t *testing.T) *http.Request {
		request, _, _ := newMultipartRequest(t)
		return request
	}
	testCases := []struct {
		name string
		run  func(*testing.T) error
		want string
	}{
		{
			name: "empty file field",
			run: func(t *testing.T) error {
				_, err := mizu.NewFormReader("", validRequest(t), &validForm{})
				return err
			},
			want: "file field is required",
		},
		{
			name: "nil request",
			run: func(t *testing.T) error {
				_, err := mizu.NewFormReader("file", nil, &validForm{})
				return err
			},
			want: "request body is required",
		},
		{
			name: "nil request body",
			run: func(t *testing.T) error {
				request := &http.Request{Header: make(http.Header)}
				_, err := mizu.NewFormReader("file", request, &validForm{})
				return err
			},
			want: "request body is required",
		},
		{
			name: "nil message",
			run: func(t *testing.T) error {
				var message *validForm
				_, err := mizu.NewFormReader("file", validRequest(t), message)
				return err
			},
			want: "message is nil",
		},
		{
			name: "non-struct message",
			run: func(t *testing.T) error {
				message := 1
				_, err := mizu.NewFormReader("file", validRequest(t), &message)
				return err
			},
			want: "must point to a struct",
		},
		{
			name: "invalid content type",
			run: func(t *testing.T) error {
				request := validRequest(t)
				request.Header.Set("Content-Type", "text/plain")
				_, err := mizu.NewFormReader("file", request, &validForm{})
				return err
			},
			want: "expected multipart/form-data",
		},
		{
			name: "malformed content type",
			run: func(t *testing.T) error {
				request := validRequest(t)
				request.Header.Set("Content-Type", "multipart/form-data; boundary")
				_, err := mizu.NewFormReader("file", request, &validForm{})
				return err
			},
			want: "parse form content type",
		},
		{
			name: "missing boundary",
			run: func(t *testing.T) error {
				request := validRequest(t)
				request.Header.Set("Content-Type", "multipart/form-data")
				_, err := mizu.NewFormReader("file", request, &validForm{})
				return err
			},
			want: "boundary not found",
		},
		{
			name: "invalid boundary",
			run: func(t *testing.T) error {
				request := validRequest(t)
				request.Header.Set("Content-Type", `multipart/form-data; boundary="`+strings.Repeat("a", 71)+`"`)
				_, err := mizu.NewFormReader("file", request, &validForm{})
				return err
			},
			want: "invalid form boundary",
		},
		{
			name: "file field collision",
			run: func(t *testing.T) error {
				_, err := mizu.NewFormReader("file", validRequest(t), &conflictingForm{})
				return err
			},
			want: "conflicts with message field",
		},
		{
			name: "duplicate field name",
			run: func(t *testing.T) error {
				_, err := mizu.NewFormReader("file", validRequest(t), &duplicateForm{})
				return err
			},
			want: `duplicate form field name "same"`,
		},
		{
			name: "invalid required tag",
			run: func(t *testing.T) error {
				_, err := mizu.NewFormReader("file", validRequest(t), &invalidRequiredForm{})
				return err
			},
			want: "parse required tag",
		},
		{
			name: "invalid field limit",
			run: func(t *testing.T) error {
				_, err := mizu.NewFormReader(
					"file", validRequest(t), &validForm{}, mizu.WithFormFieldLimitBytes(0),
				)
				return err
			},
			want: "field limit must be positive",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(t)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMizu_FormReaderErrors(t *testing.T) {
	t.Run("bad conversion", func(t *testing.T) {
		type fields struct {
			Count int `form:"count"`
		}
		request, _, _ := newMultipartRequest(t,
			formPart{name: "count", data: []byte("many")},
			formPart{name: "file", filename: "file.txt", data: []byte("data")},
		)
		form, err := mizu.NewFormReader("file", request, &fields{})
		require.NoError(t, err)
		defer form.Close()
		_, _, err = form.File()
		assert.ErrorContains(t, err, `decode form field "count"`)
	})

	t.Run("duplicate singleton", func(t *testing.T) {
		type fields struct {
			Name string `form:"name"`
		}
		request, _, _ := newMultipartRequest(t,
			formPart{name: "name", data: []byte("first")},
			formPart{name: "name", data: []byte("second")},
			formPart{name: "file", filename: "file.txt", data: []byte("data")},
		)
		form, err := mizu.NewFormReader("file", request, &fields{})
		require.NoError(t, err)
		defer form.Close()
		_, _, err = form.File()
		assert.ErrorContains(t, err, `duplicate form field "name"`)
	})

	t.Run("missing required field", func(t *testing.T) {
		type fields struct {
			Name string `form:"name" required:"true"`
		}
		request, _, _ := newMultipartRequest(t,
			formPart{name: "file", filename: "file.txt", data: []byte("data")},
		)
		form, err := mizu.NewFormReader("file", request, &fields{})
		require.NoError(t, err)
		defer form.Close()
		file, purge, err := form.File()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, file)
		require.NoError(t, err)
		firstErr := purge()
		require.ErrorContains(t, firstErr, `required form field "name" is missing`)
		assert.EqualError(t, purge(), firstErr.Error(), "EOF validation errors must remain stable")
	})

	t.Run("missing file", func(t *testing.T) {
		request, _, _ := newMultipartRequest(t, formPart{name: "unknown", data: []byte("value")})
		form, err := mizu.NewFormReader("file", request, &struct{}{})
		require.NoError(t, err)
		defer form.Close()
		_, _, err = form.File()
		assert.ErrorContains(t, err, `form file field "file" is required`)
	})

	t.Run("duplicate file", func(t *testing.T) {
		request, _, _ := newMultipartRequest(t,
			formPart{name: "file", filename: "first.txt", data: []byte("first")},
			formPart{name: "file", filename: "second.txt", data: []byte("second")},
		)
		form, err := mizu.NewFormReader("file", request, &struct{}{})
		require.NoError(t, err)
		defer form.Close()
		file, purge, err := form.File()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, file)
		require.NoError(t, err)
		assert.ErrorContains(t, purge(), `duplicate form file field "file"`)
	})

	t.Run("oversized scalar field", func(t *testing.T) {
		type fields struct {
			Name string `form:"name"`
		}
		request, _, _ := newMultipartRequest(t,
			formPart{name: "name", data: []byte("large")},
			formPart{name: "file", filename: "file.txt", data: []byte("data")},
		)
		form, err := mizu.NewFormReader(
			"file", request, &fields{}, mizu.WithFormFieldLimitBytes(4),
		)
		require.NoError(t, err)
		defer form.Close()
		_, _, err = form.File()
		assert.ErrorContains(t, err, `form field "name" exceeds 4 bytes`)
	})
}

func TestMizu_FormReaderClose(t *testing.T) {
	request, body, _ := newMultipartRequest(t, formPart{name: "file", filename: "file.txt", data: []byte("data")})
	form, err := mizu.NewFormReader("file", request, &struct{}{})
	require.NoError(t, err)

	form.Close()
	form.Close()
	assert.Equal(t, 1, body.closes)
	_, err = form.NextPart()
	assert.ErrorContains(t, err, "form reader is closed")
}

func TestMizu_FileReader(t *testing.T) {
	data := []byte("hello, mizu\n")
	inner := &trackingReadCloser{Reader: bytes.NewReader(data)}
	reader := mizu.NewFileReader(inner)

	assert.Zero(t, reader.ReadSize())
	assert.Equal(t, "text/plain; charset=utf-8", reader.ContentType())
	sniffed := reader.MimeSniffer()
	assert.Equal(t, data, sniffed)
	sniffed[0] = 'x'
	assert.Equal(t, data, reader.MimeSniffer(), "MimeSniffer must return a copy")

	actual, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, actual)
	assert.Equal(t, int64(len(data)), reader.ReadSize())
	checksum := sha256.Sum256(data)
	assert.Equal(t, hex.EncodeToString(checksum[:]), reader.Checksum())
	require.NoError(t, reader.Close())
	assert.Equal(t, 1, inner.closes)
}

func TestMizu_FileReaderLimit(t *testing.T) {
	reader := mizu.NewFileReader(
		io.NopCloser(strings.NewReader("too large")),
		mizu.WithFileLimitBytes(4),
	)
	data, err := io.ReadAll(reader)
	assert.Equal(t, "too large", string(data))
	assert.ErrorIs(t, err, mizu.ErrFileTooLarge)
	assert.Equal(t, int64(len(data)), reader.ReadSize())

	n, err := reader.Read(make([]byte, 1))
	assert.Zero(t, n)
	assert.True(t, errors.Is(err, mizu.ErrFileTooLarge))
}

var _ io.ReadCloser = (*mizu.FileReader)(nil)
