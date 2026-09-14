package formx_test

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

	"github.com/humbornjo/mizu/x/formx"
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
	Title    formAlias `form:"title" json:"ignored_title"`
	Enabled  bool      `json:"enabled"`
	Count    formNumber
	Unsigned uint16 `form:"unsigned"`
	Ratio    float32
	Data     formBytes
	Pointer  *int
	Code     upperText
	Ignored  string `form:"-"`
	Trailing string `form:"trailing"`
}

func TestFormx_NewFormReader(t *testing.T) {
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
		formPart{name: "ignored_title", data: []byte("wrong")},
		formPart{name: "file", filename: "package.txt", data: fileData},
		formPart{name: "trailing", data: []byte("complete")},
	)

	var fields typedUploadForm
	form, err := formx.NewFormReader("file", request, &fields)
	require.NoError(t, err)
	defer form.Close()
	assert.Zero(t, body.readBytes, "constructing the form reader must not read the request body")

	file, purge, err := form.File()
	require.NoError(t, err)
	assert.Equal(t, "package.txt", file.FileName())
	assert.Less(t, body.readBytes, int64(bodySize), "finding the file must not buffer the whole upload")
	assert.Empty(t, fields.Trailing)

	reader := formx.NewFileReader(file, formx.WithFileLimitBytes(int64(len(fileData))))
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
	assert.Empty(t, fields.Ignored)
	assert.Equal(t, "complete", fields.Trailing)
}

func TestFormx_FormReaderNextPart(t *testing.T) {
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
	form, err := formx.NewFormReader("file", request, &message)
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

// A nil message puts the reader in manual mode: every part is
// returned to the caller untouched.
func TestFormx_FormReaderManual(t *testing.T) {
	type fields struct {
		Name string `form:"name"`
	}

	request, _, _ := newMultipartRequest(t,
		formPart{name: "name", data: []byte("visible")},
	)
	form, err := formx.NewFormReader("file", request, (*fields)(nil))
	require.NoError(t, err)
	defer form.Close()

	part, err := form.NextPart()
	require.NoError(t, err)
	data, err := io.ReadAll(part)
	require.NoError(t, err)
	assert.Equal(t, "visible", string(data))
}

func TestFormx_NewFormReaderValidation(t *testing.T) {
	type validForm struct{}

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
				_, err := formx.NewFormReader("", validRequest(t), &validForm{})
				return err
			},
			want: "file field is required",
		},
		{
			name: "nil request",
			run: func(t *testing.T) error {
				_, err := formx.NewFormReader("file", nil, &validForm{})
				return err
			},
			want: "request body is required",
		},
		{
			name: "nil request body",
			run: func(t *testing.T) error {
				request := &http.Request{Header: make(http.Header)}
				_, err := formx.NewFormReader("file", request, &validForm{})
				return err
			},
			want: "request body is required",
		},
		{
			name: "non-struct message",
			run: func(t *testing.T) error {
				message := 1
				_, err := formx.NewFormReader("file", validRequest(t), &message)
				return err
			},
			want: "must point to a struct",
		},
		{
			name: "malformed content type",
			run: func(t *testing.T) error {
				request := validRequest(t)
				request.Header.Set("Content-Type", "multipart/form-data; boundary")
				_, err := formx.NewFormReader("file", request, &validForm{})
				return err
			},
			want: "mime:",
		},
		{
			name: "missing boundary",
			run: func(t *testing.T) error {
				request := validRequest(t)
				request.Header.Set("Content-Type", "text/plain")
				_, err := formx.NewFormReader("file", request, &validForm{})
				return err
			},
			want: "boundary not found",
		},
		{
			name: "invalid field limit",
			run: func(t *testing.T) error {
				_, err := formx.NewFormReader(
					"file", validRequest(t), &validForm{}, formx.WithFormFieldLimitBytes(0),
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

// Field mapping is best-effort: unconvertible and oversized parts
// leave the field untouched instead of failing the upload.
func TestFormx_FormReaderLenient(t *testing.T) {
	t.Run("bad conversion", func(t *testing.T) {
		type fields struct {
			Count int `form:"count"`
		}
		var message fields
		request, _, _ := newMultipartRequest(t,
			formPart{name: "count", data: []byte("many")},
			formPart{name: "file", filename: "file.txt", data: []byte("data")},
		)
		form, err := formx.NewFormReader("file", request, &message)
		require.NoError(t, err)
		defer form.Close()

		_, purge, err := form.File()
		require.NoError(t, err)
		require.NoError(t, purge())
		assert.Zero(t, message.Count)
	})

	t.Run("oversized field is truncated", func(t *testing.T) {
		type fields struct {
			Name string `form:"name"`
		}
		var message fields
		request, _, _ := newMultipartRequest(t,
			formPart{name: "name", data: []byte("large")},
			formPart{name: "file", filename: "file.txt", data: []byte("data")},
		)
		form, err := formx.NewFormReader(
			"file", request, &message, formx.WithFormFieldLimitBytes(4),
		)
		require.NoError(t, err)
		defer form.Close()

		_, purge, err := form.File()
		require.NoError(t, err)
		require.NoError(t, purge())
		assert.Equal(t, "larg", message.Name)
	})

	t.Run("repeated field keeps the last value", func(t *testing.T) {
		type fields struct {
			Name string `form:"name"`
		}
		var message fields
		request, _, _ := newMultipartRequest(t,
			formPart{name: "name", data: []byte("first")},
			formPart{name: "name", data: []byte("second")},
			formPart{name: "file", filename: "file.txt", data: []byte("data")},
		)
		form, err := formx.NewFormReader("file", request, &message)
		require.NoError(t, err)
		defer form.Close()

		_, purge, err := form.File()
		require.NoError(t, err)
		require.NoError(t, purge())
		assert.Equal(t, "second", message.Name)
	})

	t.Run("missing file", func(t *testing.T) {
		request, _, _ := newMultipartRequest(t, formPart{name: "unknown", data: []byte("value")})
		form, err := formx.NewFormReader("file", request, &struct{}{})
		require.NoError(t, err)
		defer form.Close()
		_, _, err = form.File()
		assert.ErrorIs(t, err, io.EOF)
	})
}

// A file part sized to an exact multiple of the buffer ends without
// a short read: the final read returns full bytes without io.EOF.
// Purge must drain whatever the caller left unread — from nothing to
// everything-but-EOF — before the trailing fields become visible.
func TestFormx_FormReaderPurgeDrain(t *testing.T) {
	type fields struct {
		Trailing string `form:"trailing"`
	}

	testCases := []struct {
		name string
		size int
		read int64
	}{
		{name: "unread 1024", size: 1024, read: 0},
		{name: "read exact 1024", size: 1024, read: 1024},
		{name: "unread 4096", size: 4 * 1024, read: 0},
		{name: "read exact 4096", size: 4 * 1024, read: 4 * 1024},
		{name: "partial 4096", size: 4 * 1024, read: 512},
		{name: "read exact 8192", size: 8 * 1024, read: 8 * 1024},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			request, body, bodySize := newMultipartRequest(t,
				formPart{name: "file", filename: "blob.bin", data: bytes.Repeat([]byte("x"), tc.size)},
				formPart{name: "trailing", data: []byte("done")},
			)
			var message fields
			form, err := formx.NewFormReader("file", request, &message)
			require.NoError(t, err)
			defer form.Close()

			file, purge, err := form.File()
			require.NoError(t, err)
			if tc.read > 0 {
				n, err := io.CopyN(io.Discard, file, tc.read)
				require.NoError(t, err)
				assert.Equal(t, tc.read, n)
			}
			require.NoError(t, purge())
			assert.Equal(t, "done", message.Trailing)
			assert.Equal(t, int64(bodySize), body.readBytes, "purge must drain the request body")
		})
	}
}

func TestFormx_FormReaderClose(t *testing.T) {
	request, body, _ := newMultipartRequest(t, formPart{name: "file", filename: "file.txt", data: []byte("data")})
	form, err := formx.NewFormReader("file", request, &struct{}{})
	require.NoError(t, err)

	form.Close()
	assert.Equal(t, 1, body.closes)
}

func TestFormx_FileReader(t *testing.T) {
	data := []byte("hello, formx\n")
	inner := &trackingReadCloser{Reader: bytes.NewReader(data)}
	reader := formx.NewFileReader(inner)

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

func TestFormx_FileReaderLimit(t *testing.T) {
	reader := formx.NewFileReader(
		io.NopCloser(strings.NewReader("too large")),
		formx.WithFileLimitBytes(4),
	)
	data, err := io.ReadAll(reader)
	assert.Equal(t, "too large", string(data))
	assert.ErrorIs(t, err, formx.ErrFileTooLarge)
	assert.Equal(t, int64(len(data)), reader.ReadSize())

	n, err := reader.Read(make([]byte, 1))
	assert.Zero(t, n)
	assert.True(t, errors.Is(err, formx.ErrFileTooLarge))
}

var _ io.ReadCloser = (*formx.FileReader)(nil)
