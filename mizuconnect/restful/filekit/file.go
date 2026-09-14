package filekit

import (
	"io"

	"github.com/humbornjo/mizu/x/formx"
)

// ErrFileTooLarge is retained for compatibility.
//
// Deprecated: use formx.ErrFileTooLarge.
var ErrFileTooLarge = formx.ErrFileTooLarge

// FormReader is retained for compatibility.
//
// Deprecated: use formx.FormReader.
type FormReader = formx.FormReader

// FileReader is retained for compatibility.
//
// Deprecated: use formx.FileReader.
type FileReader = formx.FileReader

// FileReaderOption is retained for compatibility.
//
// Deprecated: use formx.FileReaderOption.
type FileReaderOption = formx.FileReaderOption

// WithFileLimitBytes is retained for compatibility.
//
// Deprecated: use formx.WithFileLimitBytes.
func WithFileLimitBytes(limit int64) FileReaderOption {
	return formx.WithFileLimitBytes(limit)
}

// NewFileReader is retained for compatibility.
//
// Deprecated: use formx.NewFileReader.
func NewFileReader(rx io.ReadCloser, opts ...FileReaderOption) *FileReader {
	return formx.NewFileReader(rx, opts...)
}
