package filekit_test

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"testing"

	"github.com/humbornjo/mizu/mizuconnect/restful/filekit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/api/httpbody"
	"google.golang.org/protobuf/encoding/protojson"
)

type mockDecodeForm struct {
	*httpbody.HttpBody
	Form *httpbody.HttpBody
}

func (m *mockDecodeForm) GetForm() *httpbody.HttpBody {
	return m.Form
}

type readStep struct {
	data []byte
	err  error
}

type scriptedReader struct {
	steps []readStep
	idx   int
}

func (r *scriptedReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.steps) {
		return 0, io.EOF
	}
	step := r.steps[r.idx]
	r.idx++
	n := copy(p, step.data)
	return n, step.err
}

type alwaysEmptyReader struct{}

func (alwaysEmptyReader) Read([]byte) (int, error) {
	return 0, nil
}

func TestFilekit_FormDecoderEmptyReadsAndMultipart(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	filePart, err := writer.CreateFormFile("file", "hello.txt")
	require.NoError(t, err)
	_, err = filePart.Write([]byte("hello world file content"))
	require.NoError(t, err)

	require.NoError(t, writer.WriteField("title", "greeting"))
	require.NoError(t, writer.Close())

	raw := body.Bytes()
	split := len(raw) / 2
	prefix := raw[:split]
	suffix := raw[split:]

	rx := &scriptedReader{
		steps: []readStep{
			{data: prefix, err: nil},
			{data: nil, err: nil},
			{data: suffix, err: io.EOF},
		},
	}

	marshaler := filekit.NewFormMarshaler(protojson.MarshalOptions{}, protojson.UnmarshalOptions{})
	decoder := marshaler.NewDecoder(rx)

	var decoded bytes.Buffer
	for {
		var frame mockDecodeForm
		err := decoder.Decode(&frame)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		require.NotNil(t, frame.GetForm())
		decoded.Write(frame.GetForm().GetData())
	}

	assert.Equal(t, raw, decoded.Bytes())

	mr := multipart.NewReader(&decoded, writer.Boundary())
	part1, err := mr.NextPart()
	require.NoError(t, err)
	assert.Equal(t, "file", part1.FormName())
	assert.Equal(t, "hello.txt", part1.FileName())
	fileData, err := io.ReadAll(part1)
	require.NoError(t, err)
	assert.Equal(t, "hello world file content", string(fileData))
	require.NoError(t, part1.Close())

	part2, err := mr.NextPart()
	require.NoError(t, err)
	assert.Equal(t, "title", part2.FormName())
	scalarData, err := io.ReadAll(part2)
	require.NoError(t, err)
	assert.Equal(t, "greeting", string(scalarData))
	require.NoError(t, part2.Close())

	_, err = mr.NextPart()
	assert.ErrorIs(t, err, io.EOF)
}

func TestFilekit_FormDecoderNoProgress(t *testing.T) {
	marshaler := filekit.NewFormMarshaler(protojson.MarshalOptions{}, protojson.UnmarshalOptions{})
	decoder := marshaler.NewDecoder(alwaysEmptyReader{})

	var frame mockDecodeForm
	err := decoder.Decode(&frame)
	assert.ErrorIs(t, err, io.ErrNoProgress)
}

func TestFilekit_FormDecoderErrors(t *testing.T) {
	marshaler := filekit.NewFormMarshaler(protojson.MarshalOptions{}, protojson.UnmarshalOptions{})
	boom := errors.New("read failed")

	tests := []struct {
		name    string
		reader  io.Reader
		target  any
		wantErr error
	}{
		{
			name:    "invalid target",
			reader:  bytes.NewReader(nil),
			target:  &struct{}{},
			wantErr: nil,
		},
		{
			name:    "clean eof",
			reader:  &scriptedReader{steps: []readStep{{data: nil, err: io.EOF}}},
			target:  &mockDecodeForm{},
			wantErr: io.EOF,
		},
		{
			name:    "non eof error",
			reader:  &scriptedReader{steps: []readStep{{data: nil, err: boom}}},
			target:  &mockDecodeForm{},
			wantErr: boom,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := marshaler.NewDecoder(tc.reader).Decode(tc.target)
			require.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}
