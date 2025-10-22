package filekit_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu/mizuconnect/restful/filekit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/api/httpbody"
)

// MockFormFrame implements HTTPForm interface for testing
type MockFormFrame struct {
	*httpbody.HttpBody
}

func (m MockFormFrame) GetForm() *httpbody.HttpBody {
	return m.HttpBody
}

func NewFormFrame(contentType string, data []byte) *MockFormFrame {
	return &MockFormFrame{
		HttpBody: &httpbody.HttpBody{
			Data:        data,
			ContentType: contentType,
		},
	}
}

// MockFormStream implements StreamForm interface for testing
type MockFormStream struct {
	messages     []*MockFormFrame
	currentIndex int
	receiveError error
	msgError     error
	peer         connect.Peer
	spec         connect.Spec
	header       http.Header
	conn         connect.StreamingHandlerConn
}

var _ filekit.StreamForm[MockFormFrame] = (*MockFormStream)(nil)

func NewMockStreamForm(messages ...*MockFormFrame) *MockFormStream {
	return &MockFormStream{
		currentIndex: -1,
		messages:     messages,
		header:       make(http.Header),
	}
}

func (m *MockFormStream) SetReceiveError(err error) {
	m.receiveError = err
}

func (m *MockFormStream) SetMsgError(err error) {
	m.msgError = err
}

func (m *MockFormStream) Msg() MockFormFrame {
	if m.msgError != nil {
		var zero MockFormFrame
		return zero
	}
	if m.currentIndex >= len(m.messages) {
		var zero MockFormFrame
		return zero
	}
	return *m.messages[m.currentIndex]
}

func (m *MockFormStream) Err() error {
	return m.receiveError
}

func (m *MockFormStream) Receive() bool {
	if m.receiveError != nil {
		return false
	}
	m.currentIndex++
	return m.currentIndex < len(m.messages)
}

func (m *MockFormStream) Peer() connect.Peer {
	return m.peer
}

func (m *MockFormStream) Spec() connect.Spec {
	return m.spec
}

func (m *MockFormStream) RequestHeader() http.Header {
	return m.header
}

func (m *MockFormStream) Conn() connect.StreamingHandlerConn {
	return m.conn
}

func TestFilekit_Read_FormReader(t *testing.T) {
	t.Run("test form reader basic usage", func(t *testing.T) {
		// Create multipart form data
		body := bytes.NewBuffer(nil)
		writer := multipart.NewWriter(body)

		// Add a simple form field
		field, err := writer.CreateFormField("field1")
		require.NoError(t, err)
		_, err = field.Write([]byte("value1"))
		require.NoError(t, err)

		// Add a file field
		file, err := writer.CreateFormFile("upload", "test.txt")
		require.NoError(t, err)
		_, err = file.Write([]byte("file content data"))
		require.NoError(t, err)

		err = writer.Close()
		require.NoError(t, err)

		// Create mock HTTP form
		form := NewFormFrame(writer.FormDataContentType(), body.Bytes())
		stream := NewMockStreamForm(form)

		// Create form reader
		reader, err := filekit.NewFormReader("upload", stream, nil)
		require.NoError(t, err)
		assert.NotNil(t, reader)

		// Read first part (field)
		part1, err := reader.NextPart()
		require.NoError(t, err)
		assert.Equal(t, "field1", part1.FormName())

		// Read field content
		fieldContent, err := io.ReadAll(part1)
		require.NoError(t, err)
		assert.Equal(t, "value1", string(fieldContent))

		// Read second part (file)
		part2, err := reader.NextPart()
		require.NoError(t, err)
		assert.Equal(t, "upload", part2.FormName())
		assert.Equal(t, "test.txt", part2.FileName())

		// Read file content
		fileContent, err := io.ReadAll(part2)
		require.NoError(t, err)
		assert.Equal(t, "file content data", string(fileContent))

		// EOF on next read
		_, err = reader.NextPart()
		assert.Equal(t, io.EOF, err)
	})

	t.Run("test empty multipart form", func(t *testing.T) {
		// Create empty multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		err := writer.Close()
		require.NoError(t, err)

		// Create mock HTTP form
		form := NewFormFrame(writer.FormDataContentType(), body.Bytes())
		stream := NewMockStreamForm(form)

		// Create form reader
		reader, err := filekit.NewFormReader("file", stream, nil)
		require.NoError(t, err)

		// Should get EOF immediately
		_, err = reader.NextPart()
		assert.Equal(t, io.EOF, err)
	})

	t.Run("test NextPart with different appear order of fields", func(t *testing.T) {
		testCases := []struct {
			name          string
			fieldOrder    []string
			fileField     string
			expectedOrder []string
		}{
			{
				name:          "file first, then fields",
				fieldOrder:    []string{"upload", "name", "age", "email"},
				fileField:     "upload",
				expectedOrder: []string{"upload", "name", "age", "email"},
			},
			{
				name:          "fields first, then file",
				fieldOrder:    []string{"name", "age", "upload", "email"},
				fileField:     "upload",
				expectedOrder: []string{"name", "age", "upload", "email"},
			},
			{
				name:          "file in middle",
				fieldOrder:    []string{"name", "upload", "age", "email"},
				fileField:     "upload",
				expectedOrder: []string{"name", "upload", "age", "email"},
			},
			{
				name:          "multiple files",
				fieldOrder:    []string{"avatar", "name", "document", "email"},
				fileField:     "document",
				expectedOrder: []string{"avatar", "name", "document", "email"},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create multipart form with specified field order
				body := &bytes.Buffer{}
				writer := multipart.NewWriter(body)

				fieldData := map[string][]byte{
					"name":     []byte("John Doe"),
					"age":      []byte("30"),
					"email":    []byte("john@example.com"),
					"upload":   []byte("file content 1"),
					"avatar":   []byte("avatar image"),
					"document": []byte("document content"),
				}

				// Create fields in specified order
				for _, fieldName := range tc.fieldOrder {
					if fieldName == tc.fileField || fieldName == "upload" || fieldName == "avatar" || fieldName == "document" {
						// It's a file field
						file, err := writer.CreateFormFile(fieldName, fieldName+".txt")
						require.NoError(t, err)
						_, err = file.Write(fieldData[fieldName])
						require.NoError(t, err)
					} else {
						// It's a regular field
						field, err := writer.CreateFormField(fieldName)
						require.NoError(t, err)
						_, err = field.Write(fieldData[fieldName])
						require.NoError(t, err)
					}
				}

				err := writer.Close()
				require.NoError(t, err)

				// Create mock HTTP form
				form := NewFormFrame(writer.FormDataContentType(), body.Bytes())
				stream := NewMockStreamForm(form)

				// Create form reader
				reader, err := filekit.NewFormReader(tc.fileField, stream, nil)
				require.NoError(t, err)

				// Read all parts and verify order
				var actualOrder []string
				for {
					part, err := reader.NextPart()
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(t, err)
					actualOrder = append(actualOrder, part.FormName())
				}

				assert.Equal(t, tc.expectedOrder, actualOrder)
			})
		}
	})
}

func TestFilekit_Read_FileReader(t *testing.T) {
	t.Run("test file size tie with read size", func(t *testing.T) {
		testCases := []struct {
			name        string
			testData    []byte
			limitBytes  int64
			readSize    int
			expectError error
			description string
		}{
			{
				name:        "exact size match - 10 bytes",
				testData:    []byte("0123456789"),
				limitBytes:  10,
				readSize:    10,
				expectError: io.EOF,
				description: "File size exactly matches limit, should read successfully and return EOF on next read",
			},
			{
				name:        "empty content - zero bytes",
				testData:    []byte{},
				limitBytes:  0,
				readSize:    0,
				expectError: io.EOF,
				description: "Empty file with zero limit should handle gracefully",
			},
			{
				name:        "single byte exact match",
				testData:    []byte("A"),
				limitBytes:  1,
				readSize:    1,
				expectError: io.EOF,
				description: "Single byte file exactly at limit",
			},
			{
				name:        "large file exact match",
				testData:    bytes.Repeat([]byte("X"), 1024),
				limitBytes:  1024,
				readSize:    1024,
				expectError: io.EOF,
				description: "Larger file exactly at limit (1KB)",
			},
			{
				name:        "multiple reads to reach limit",
				testData:    []byte("0123456789"),
				limitBytes:  10,
				readSize:    3, // Read in smaller chunks
				expectError: io.EOF,
				description: "Multiple small reads that sum to exact limit",
			},
			{
				name:        "unicode content exact match",
				testData:    []byte("你好世界"), // "Hello World" in Chinese
				limitBytes:  12,             // 12 bytes for UTF-8 encoding
				readSize:    12,
				expectError: io.EOF,
				description: "Unicode content exactly at byte limit",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				reader := filekit.NewFileReader(
					io.NopCloser(bytes.NewReader(tc.testData)),
					filekit.WithLimitBytes(tc.limitBytes),
				)
				defer reader.Close() // nolint: errcheck

				totalRead := 0
				buf := make([]byte, tc.readSize)

				// Read data in chunks if necessary
				for totalRead < len(tc.testData) {
					n, err := reader.Read(buf)
					totalRead += n

					if err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						require.NoError(t, err, tc.description)
					}

					// For partial reads, adjust buffer size for next read
					if totalRead+n < len(tc.testData) && n < len(buf) {
						buf = buf[n:]
					}
				}

				// Verify total bytes read
				assert.Equal(t, int64(len(tc.testData)), reader.ReadSize(), tc.description)

				// Verify content
				if len(tc.testData) > 0 {
					allData := make([]byte, len(tc.testData))
					reader2 := filekit.NewFileReader(
						io.NopCloser(bytes.NewReader(tc.testData)),
						filekit.WithLimitBytes(tc.limitBytes),
					)
					defer reader2.Close() // nolint: errcheck

					n, err := io.ReadFull(reader2, allData)
					require.NoError(t, err)
					require.Equal(t, len(tc.testData), n)
					require.Equal(t, tc.testData, allData, tc.description)
				}

				// Next read should return expected error
				_, err := reader.Read(buf)
				require.Equal(t, tc.expectError, err, tc.description)
			})
		}
	})

	t.Run("test file size exceeded", func(t *testing.T) {
		testCases := []struct {
			name          string
			testData      []byte
			limitBytes    int64
			readChunks    []int
			expectErrorAt int // which read chunk should trigger error
			description   string
		}{
			{
				name:          "single byte over limit",
				testData:      []byte("AB"),
				limitBytes:    1,
				readChunks:    []int{2}, // Try to read 2 bytes when limit is 1
				expectErrorAt: 1,
				description:   "File 1 byte over limit should trigger error",
			},
			{
				name:          "multiple reads - small overflow",
				testData:      []byte("0123456789ABCDE"),
				limitBytes:    10,
				readChunks:    []int{10, 5},
				expectErrorAt: 2,
				description:   "File 5 bytes over limit with multiple reads",
			},
			{
				name:          "large file overflow",
				testData:      bytes.Repeat([]byte("X"), 1500),
				limitBytes:    1024,
				readChunks:    []int{512, 988},
				expectErrorAt: 2,
				description:   "Large file (1.5KB) with 476 byte overflow",
			},
			{
				name:          "tiny chunks building up to overflow",
				testData:      []byte("0123456789"),
				limitBytes:    8,
				readChunks:    []int{3, 3, 3, 1},
				expectErrorAt: 3,
				description:   "Multiple tiny reads that eventually exceed limit",
			},
			{
				name:          "unicode content overflow",
				testData:      []byte("你好世界abc"), // 12 bytes + 3 ASCII = 15 bytes
				limitBytes:    12,
				readChunks:    []int{12, 3},
				expectErrorAt: 2,
				description:   "Unicode content with ASCII overflow",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				reader := filekit.NewFileReader(
					io.NopCloser(bytes.NewReader(tc.testData)),
					filekit.WithLimitBytes(tc.limitBytes),
				)
				defer reader.Close() // nolint: errcheck

				totalRead := 0
				chunkIndex := 0

				for _, chunkSize := range tc.readChunks {
					chunkIndex++
					buf := make([]byte, chunkSize)
					n, err := reader.Read(buf)
					totalRead += n

					if chunkIndex == tc.expectErrorAt {
						// This read should trigger the size limit error
						require.Error(t, err, tc.description)
						require.ErrorIs(t, err, filekit.ErrFileTooLarge, tc.description)
						require.Equal(t, chunkSize, n, tc.description)

						// Verify total bytes read exceeds limit
						assert.Greater(t, int64(totalRead), tc.limitBytes, tc.description)
						assert.Equal(t, int64(totalRead), reader.ReadSize(), tc.description)

						// Subsequent reads should also return size limit error
						_, err = reader.Read(buf)
						require.Error(t, err, tc.description)
						require.ErrorIs(t, err, filekit.ErrFileTooLarge, tc.description)
						break
					}

					// This read should succeed
					require.NoError(t, err, tc.description)
					require.Equal(t, chunkSize, n, tc.description, chunkIndex)
				}
			})
		}
	})

	t.Run("test file hash correctness", func(t *testing.T) {
		testCases := []struct {
			name        string
			testData    []byte
			description string
		}{
			{
				name:        "simple ASCII text",
				testData:    []byte("Hello, World! This is a test file for hash calculation."),
				description: "ASCII text should have correct hash",
			},
			{
				name:        "empty content",
				testData:    []byte{},
				description: "Empty content should have correct hash",
			},
			{
				name:        "single byte",
				testData:    []byte("A"),
				description: "Single byte should have correct hash",
			},
			{
				name:        "unicode content",
				testData:    []byte("你好世界 🌍 Unicode测试"),
				description: "Unicode content should have correct hash",
			},
			{
				name:        "binary-like content",
				testData:    []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD},
				description: "Binary content should have correct hash",
			},
			{
				name:        "large content",
				testData:    bytes.Repeat([]byte("Large test content "), 100), // ~1.9KB
				description: "Large content should have correct hash",
			},
			{
				name:        "content with newlines and special chars",
				testData:    []byte("Line 1\nLine 2\r\nLine 3\tTabbed\nEnd!"),
				description: "Content with special characters should have correct hash",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				reader := filekit.NewFileReader(io.NopCloser(bytes.NewReader(tc.testData)))
				defer reader.Close() // nolint: errcheck

				// Read all data
				buf := make([]byte, 4096) // Large buffer for all test cases
				totalRead := 0
				for {
					n, err := reader.Read(buf[totalRead:])
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(t, err, tc.description)
					totalRead += n
				}

				// Verify read size
				assert.Equal(t, int64(len(tc.testData)), reader.ReadSize(), tc.description)

				// Verify checksum
				checksum := reader.Checksum()
				assert.NotEmpty(t, checksum, tc.description)

				// Calculate expected SHA256 hash
				h := sha256.New()
				h.Write(tc.testData)
				expectedChecksum := hex.EncodeToString(h.Sum(nil))
				assert.Equal(t, expectedChecksum, checksum, tc.description)
			})
		}
	})

	t.Run("test MIME type detection", func(t *testing.T) {
		testCases := []struct {
			name             string
			testData         []byte
			expectedMimeType string
			description      string
		}{
			// Content that Go's http.DetectContentType will detect as text/plain using textSig
			{
				name:             "large plain text using textSig",
				testData:         bytes.Repeat([]byte("This is a substantial amount of text content that should be detected as plain text by Go's MIME detection algorithm. "), 20),
				expectedMimeType: "text/plain; charset=utf-8",
				description:      "Large plain text should be detected as text/plain by textSig",
			},
			{
				name:             "HTML content",
				testData:         []byte("<!DOCTYPE html><html><head><title>Test</title></head><body>Hello World</body></html>"),
				expectedMimeType: "text/html; charset=utf-8",
				description:      "HTML content should be detected as HTML",
			},
			{
				name: "valid XML content",
				testData: []byte(`<?xml version="1.0" encoding="UTF-8"?>
<root>
  <item id="1">test content</item>
  <item id="2">more content</item>
</root>`),
				expectedMimeType: "text/xml; charset=utf-8",
				description:      "Valid XML content should be detected as XML",
			},

			// Binary content
			{
				name:             "binary zeros",
				testData:         []byte{0x00, 0x00, 0x00, 0x00, 0x00},
				expectedMimeType: "application/octet-stream",
				description:      "Binary zeros should be detected as octet stream",
			},
			{
				name:             "mixed binary",
				testData:         []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD},
				expectedMimeType: "application/octet-stream",
				description:      "Mixed binary data should be detected as octet stream",
			},
			{
				name:             "high bytes",
				testData:         []byte{0x80, 0x90, 0xA0, 0xB0, 0xC0, 0xD0, 0xE0, 0xF0},
				expectedMimeType: "text/plain; charset=utf-8",
				description:      "High byte values should be detected as text/plain",
			},

			// Image content (magic bytes)
			{
				name:             "PNG signature",
				testData:         []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D},
				expectedMimeType: "image/png",
				description:      "PNG signature should be detected as PNG image",
			},
			{
				name:             "JPEG signature",
				testData:         []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46},
				expectedMimeType: "image/jpeg",
				description:      "JPEG signature should be detected as JPEG image",
			},
			{
				name:             "GIF signature",
				testData:         []byte("GIF89a"),
				expectedMimeType: "image/gif",
				description:      "GIF signature should be detected as GIF image",
			},
			{
				name:             "BMP signature",
				testData:         []byte{'B', 'M', 0x00, 0x00, 0x00, 0x00},
				expectedMimeType: "image/bmp",
				description:      "BMP signature should be detected as BMP image",
			},

			// Edge cases using Go's exact signatures
			{
				name:             "empty content",
				testData:         []byte{},
				expectedMimeType: "text/plain; charset=utf-8",
				description:      "Empty content should default to plain text",
			},
			{
				name:             "single null byte",
				testData:         []byte{0x00},
				expectedMimeType: "application/octet-stream",
				description:      "Single null byte should be detected as binary",
			},
			{
				name:             "UTF-16BE BOM",
				testData:         []byte{0xFE, 0xFF, 0x00, 0x00},
				expectedMimeType: "text/plain; charset=utf-16be",
				description:      "UTF-16BE BOM should be detected as UTF-16BE text",
			},
			{
				name:             "UTF-16LE BOM",
				testData:         []byte{0xFF, 0xFE, 0x00, 0x00},
				expectedMimeType: "text/plain; charset=utf-16le",
				description:      "UTF-16LE BOM should be detected as UTF-16LE text",
			},
			{
				name:             "UTF-8 BOM",
				testData:         []byte{0xEF, 0xBB, 0xBF, 0x00},
				expectedMimeType: "text/plain; charset=utf-8",
				description:      "UTF-8 BOM should be detected as UTF-8 text",
			},

			// Special formats with proper signatures that Go recognizes
			{
				name:             "PDF signature",
				testData:         []byte("%PDF-1.4"),
				expectedMimeType: "application/pdf",
				description:      "PDF signature should be detected as PDF",
			},
			{
				name:             "ZIP signature with proper header",
				testData:         []byte("PK" + string([]byte{0x03, 0x04})),
				expectedMimeType: "application/zip",
				description:      "ZIP signature should be detected as ZIP",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				reader := filekit.NewFileReader(io.NopCloser(bytes.NewReader(tc.testData)))
				defer reader.Close() // nolint: errcheck

				// Read a small portion to trigger MIME detection
				buf := make([]byte, 64)
				n, err := reader.Read(buf)
				if len(tc.testData) == 0 {
					// Empty content should return EOF immediately
					require.Equal(t, io.EOF, err, "Empty content should return EOF")
					require.Equal(t, 0, n, "Empty content should read 0 bytes")
				} else {
					require.NoError(t, err)
					assert.Positive(t, n, "Should read some data for MIME detection")
				}

				// Verify MIME sniffer contains expected data
				snifferData := reader.MimeSniffer()
				assert.NotNil(t, snifferData, "MIME sniffer should not be nil")
				assert.LessOrEqual(t, len(snifferData), 512, "MIME sniffer should not exceed 512 bytes")

				// Verify content type detection
				contentType := reader.ContentType()
				assert.NotEmpty(t, contentType, "Content type should not be empty")
				assert.Equal(t, http.DetectContentType(tc.testData), contentType, tc.description)

				// Verify that reading more data doesn't change the detected type
				// (MIME type should be determined from first 512 bytes)
				if len(tc.testData) > 64 {
					moreBuf := make([]byte, 64)
					_, err := reader.Read(moreBuf)
					require.NoError(t, err)

					// Content type should remain the same
					finalContentType := reader.ContentType()
					assert.Equal(t, contentType, finalContentType, "Content type should not change after additional reads")
				}
			})
		}
	})
}
