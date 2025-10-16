package filekit_test

import (
	"bytes"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu/mizuconnect/restful/filekit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/api/httpbody"
)

// mockServerStream implements a minimal mock of connect.ServerStream for testing
type mockServerStream struct {
	connect.ServerStream[httpbody.HttpBody]

	messages  []*httpbody.HttpBody
	sendError error

	conn   connect.StreamingHandlerConn
	header http.Header
	tailer http.Header
}

func NewMockServerStream() filekit.StreamResponse {
	return &mockServerStream{
		header: make(http.Header),
		tailer: make(http.Header),
		conn:   &mockStreamingHandlerConn{},
	}
}

func (m *mockServerStream) Send(msg *httpbody.HttpBody) error {
	if m.sendError != nil {
		return m.sendError
	}
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockServerStream) Conn() connect.StreamingHandlerConn {
	return m.conn
}

func (m *mockServerStream) ResponseHeader() http.Header {
	return m.header
}

func (m *mockServerStream) ResponseTrailer() http.Header {
	return m.tailer
}

// mockStreamingHandlerConn implements connect.StreamingHandlerConn for testing
type mockStreamingHandlerConn struct {
	mock *mockServerStream
}

func (m *mockStreamingHandlerConn) Spec() connect.Spec {
	return connect.Spec{}
}

func (m *mockStreamingHandlerConn) Peer() connect.Peer {
	return connect.Peer{}
}

func (m *mockStreamingHandlerConn) RequestHeader() http.Header {
	return m.mock.header
}

func (m *mockStreamingHandlerConn) ResponseHeader() http.Header {
	return m.mock.header
}

func (m *mockStreamingHandlerConn) ResponseTrailer() http.Header {
	return m.mock.tailer
}

func (m *mockStreamingHandlerConn) Send(msg any) error {
	if httpBody, ok := msg.(*httpbody.HttpBody); ok {
		return m.mock.Send(httpBody)
	}
	return errors.New("unexpected message type")
}

func (m *mockStreamingHandlerConn) Receive(msg any) error {
	return errors.New("receive not implemented")
}

func (m *mockStreamingHandlerConn) Close(err error) error {
	return nil
}

func TestFilekit_Write_Writer(t *testing.T) {
	t.Run("test writer basic usage", func(t *testing.T) {
		// Create mock stream
		stream := NewMockServerStream()
		prologue := &httpbody.HttpBody{
			ContentType: "text/plain",
			Data:        []byte("Hello, World!"),
		}

		// Create writer
		writer, err := filekit.NewWriter(stream, prologue)
		require.NoError(t, err)
		assert.NotNil(t, writer)

		// Write additional data
		n, err := writer.Write([]byte(" This is additional content."))
		require.NoError(t, err)
		assert.Equal(t, 28, n)

		// Close writer
		err = writer.Close()
		require.NoError(t, err)

		// Verify write size
		assert.Equal(t, int64(41), writer.WriteSize())

		// Verify messages sent to stream
		mockStream := stream.(*mockServerStream)

		// First message should be the prologue with detected content type
		assert.Equal(t,
			"Hello, World! This is additional content.",
			string(mockStream.messages[0].Data),
		)
		assert.Equal(t, "text/plain", mockStream.messages[0].ContentType)
	})

	t.Run("test writer with empty prologue", func(t *testing.T) {
		stream := NewMockServerStream()
		prologue := &httpbody.HttpBody{
			ContentType: "",
			Data:        []byte{},
		}

		writer, err := filekit.NewWriter(stream, prologue)
		require.NoError(t, err)
		assert.NotNil(t, writer)

		// Write some data to trigger MIME detection
		testData := []byte("Hello, World! This is plain text content.")
		n, err := writer.Write(testData)
		require.NoError(t, err)
		assert.Equal(t, len(testData), n)

		err = writer.Close()
		require.NoError(t, err)

		assert.Equal(t, int64(len(testData)), writer.WriteSize())

		mockStream := stream.(*mockServerStream)
		assert.Len(t, mockStream.messages, 1)
		assert.Equal(t, testData, mockStream.messages[0].Data)
		assert.Equal(t, "text/plain; charset=utf-8", mockStream.messages[0].ContentType)
	})

	t.Run("test writer multiple writes", func(t *testing.T) {
		stream := NewMockServerStream()
		prologue := &httpbody.HttpBody{
			ContentType: "application/json",
			Data:        []byte(`{"start": true}`),
		}

		writer, err := filekit.NewWriter(stream, prologue)
		require.NoError(t, err)

		// Multiple writes
		writes := []string{
			`, "data": "first"`,
			`, "more": "second"`,
			`, "final": "last"}`,
		}

		totalWritten := len(prologue.Data)
		for _, data := range writes {
			n, err := writer.Write([]byte(data))
			require.NoError(t, err)
			assert.Equal(t, len(data), n)
			totalWritten += n
		}

		err = writer.Close()
		require.NoError(t, err)

		assert.Equal(t, int64(totalWritten), writer.WriteSize())

		mockStream := stream.(*mockServerStream)
		assert.Len(t, mockStream.messages, 1) // Should be coalesced by bufio.Writer
		expected := `{"start": true}, "data": "first", "more": "second", "final": "last"}`
		assert.Equal(t, expected, string(mockStream.messages[0].Data))
		assert.Equal(t, "application/json", mockStream.messages[0].ContentType)
	})

	t.Run("test writer with large data", func(t *testing.T) {
		stream := NewMockServerStream()
		prologue := &httpbody.HttpBody{
			ContentType: "application/octet-stream",
			Data:        []byte{},
		}

		writer, err := filekit.NewWriter(stream, prologue)
		require.NoError(t, err)

		// Write large data (larger than buffer size)
		largeData := bytes.Repeat([]byte("ffffffff"), 16*1024) // 128KB
		n, err := writer.Write(largeData)
		require.NoError(t, err)
		assert.Equal(t, len(largeData), n)

		err = writer.Close()
		require.NoError(t, err)

		assert.Equal(t, int64(len(largeData)), writer.WriteSize())

		mockStream := stream.(*mockServerStream)
		assert.NotEmpty(t, mockStream.messages)

		// Verify total data sent
		totalSent := 0
		for _, msg := range mockStream.messages {
			totalSent += len(msg.Data)
		}
		assert.Equal(t, len(largeData), totalSent)
	})
}

func TestFilekit_Write_WriterMimeDetection(t *testing.T) {
	testCases := []struct {
		name             string
		testData         []byte
		expectedMimeType string
		description      string
	}{
		{
			name:             "plain text",
			testData:         []byte("Hello, World! This is plain text content."),
			expectedMimeType: "text/plain; charset=utf-8",
			description:      "Plain text should be detected correctly",
		},
		{
			name:             "HTML content",
			testData:         []byte("<!DOCTYPE html><html><body>Hello World</body></html>"),
			expectedMimeType: "text/html; charset=utf-8",
			description:      "HTML content should be detected as HTML",
		},
		{
			name: "XML content",
			testData: []byte(`<?xml version="1.0" encoding="UTF-8"?>
<root>
  <item>test content</item>
</root>`),
			expectedMimeType: "text/xml; charset=utf-8",
			description:      "XML content should be detected as XML",
		},
		{
			name:             "JSON content",
			testData:         []byte(`{"name": "test", "value": 123}`),
			expectedMimeType: "text/plain; charset=utf-8",
			description:      "JSON content should be detected as plain text",
		},
		{
			name:             "PNG signature",
			testData:         []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D},
			expectedMimeType: "image/png",
			description:      "PNG signature should be detected as PNG",
		},
		{
			name:             "JPEG signature",
			testData:         []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46},
			expectedMimeType: "image/jpeg",
			description:      "JPEG signature should be detected as JPEG",
		},
		{
			name:             "GIF signature",
			testData:         []byte("GIF89a"),
			expectedMimeType: "image/gif",
			description:      "GIF signature should be detected as GIF",
		},
		{
			name:             "binary content",
			testData:         []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD},
			expectedMimeType: "application/octet-stream",
			description:      "Binary content should be detected as octet stream",
		},
		{
			name:             "empty content",
			testData:         []byte{},
			expectedMimeType: "application/octet-stream",
			description:      "Empty content should default to octet stream",
		},
		{
			name:             "PDF signature",
			testData:         []byte("%PDF-1.4"),
			expectedMimeType: "application/pdf",
			description:      "PDF signature should be detected as PDF",
		},
		{
			name:             "ZIP signature",
			testData:         []byte("PK" + string([]byte{0x03, 0x04})),
			expectedMimeType: "application/zip",
			description:      "ZIP signature should be detected as ZIP",
		},
		{
			name:             "BMP signature",
			testData:         []byte{'B', 'M', 0x00, 0x00, 0x00, 0x00},
			expectedMimeType: "image/bmp",
			description:      "BMP signature should be detected as BMP",
		},
		{
			name:             "UTF-8 BOM",
			testData:         []byte{0xEF, 0xBB, 0xBF, 0x48, 0x65, 0x6C, 0x6C, 0x6F},
			expectedMimeType: "text/plain; charset=utf-8",
			description:      "UTF-8 BOM should be detected as UTF-8 text",
		},
		{
			name:             "UTF-16BE BOM",
			testData:         []byte{0xFE, 0xFF, 0x00, 0x48},
			expectedMimeType: "text/plain; charset=utf-16be",
			description:      "UTF-16BE BOM should be detected as UTF-16BE text",
		},
		{
			name:             "UTF-16LE BOM",
			testData:         []byte{0xFF, 0xFE, 0x48, 0x00},
			expectedMimeType: "text/plain; charset=utf-16le",
			description:      "UTF-16LE BOM should be detected as UTF-16LE text",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stream := NewMockServerStream()
			prologue := &httpbody.HttpBody{
				ContentType: "", // Empty to trigger detection
				Data:        []byte{},
			}

			writer, err := filekit.NewWriter(stream, prologue)
			require.NoError(t, err)
			assert.NotNil(t, writer)

			// Write test data to trigger MIME detection
			n, err := writer.Write(tc.testData)
			require.NoError(t, err)
			assert.Equal(t, len(tc.testData), n)

			err = writer.Close()
			require.NoError(t, err)

			// Verify the detected MIME type
			mockStream := stream.(*mockServerStream)
			if len(tc.testData) == 0 {
				assert.Empty(t, mockStream.messages)
			} else {
				assert.NotEmpty(t, mockStream.messages)
			}

			if len(tc.testData) == 0 {
				return
			}
			firstMsg := mockStream.messages[0]
			assert.Equal(t, tc.expectedMimeType, firstMsg.ContentType, tc.description)
			assert.Equal(t, tc.testData, firstMsg.Data)
		})
	}
}

func TestFilekit_Write_WriterEdgeCases(t *testing.T) {
	t.Run("test writer with nil prologue", func(t *testing.T) {
		stream := NewMockServerStream()

		writer, err := filekit.NewWriter(stream, nil)
		require.NoError(t, err)
		assert.NotNil(t, writer)

		// Should be able to write normally
		testData := []byte("test data")
		n, err := writer.Write(testData)
		require.NoError(t, err)
		assert.Equal(t, len(testData), n)

		err = writer.Close()
		require.NoError(t, err)
	})

	t.Run("test writer with prologue having content type but no data", func(t *testing.T) {
		stream := NewMockServerStream()
		prologue := &httpbody.HttpBody{
			ContentType: "application/custom",
			Data:        []byte{},
		}

		writer, err := filekit.NewWriter(stream, prologue)
		require.NoError(t, err)
		assert.NotNil(t, writer)

		// Write data - should use the prologue content type
		testData := []byte("custom data")
		n, err := writer.Write(testData)
		require.NoError(t, err)
		assert.Equal(t, len(testData), n)

		err = writer.Close()
		require.NoError(t, err)

		mockStream := stream.(*mockServerStream)
		assert.Len(t, mockStream.messages, 1)
		assert.Equal(t, "application/custom", mockStream.messages[0].ContentType)
		assert.Equal(t, testData, mockStream.messages[0].Data)
	})

	t.Run("test writer with zero-length write", func(t *testing.T) {
		stream := NewMockServerStream()
		prologue := &httpbody.HttpBody{
			ContentType: "text/plain",
			Data:        []byte("initial"),
		}

		writer, err := filekit.NewWriter(stream, prologue)
		require.NoError(t, err)

		// Write zero bytes
		n, err := writer.Write([]byte{})
		require.NoError(t, err)
		assert.Equal(t, 0, n)

		err = writer.Close()
		require.NoError(t, err)

		// Write size should still be just the initial data
		assert.Equal(t, int64(len("initial")), writer.WriteSize())
	})

	t.Run("test writer close without write", func(t *testing.T) {
		stream := NewMockServerStream()
		prologue := &httpbody.HttpBody{
			ContentType: "text/plain",
			Data:        []byte("only data"),
		}

		writer, err := filekit.NewWriter(stream, prologue)
		require.NoError(t, err)

		// Close without any additional writes
		err = writer.Close()
		require.NoError(t, err)

		assert.Equal(t, int64(len("only data")), writer.WriteSize())

		mockStream := stream.(*mockServerStream)
		assert.Len(t, mockStream.messages, 1)
		assert.Equal(t, "only data", string(mockStream.messages[0].Data))
	})
}
