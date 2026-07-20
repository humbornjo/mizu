package filekit

import (
	"io"

	"github.com/humbornjo/mizu"
)

// ErrFileTooLarge is retained for compatibility.
//
// Deprecated: use mizu.ErrFileTooLarge.
var ErrFileTooLarge = mizu.ErrFileTooLarge

// FormReader is retained for compatibility.
//
// Deprecated: use mizu.FormReader.
type FormReader = mizu.FormReader

// FileReader is retained for compatibility.
//
// Deprecated: use mizu.FileReader.
type FileReader = mizu.FileReader

// FileReaderOption is retained for compatibility.
//
// Deprecated: use mizu.FileReaderOption.
type FileReaderOption = mizu.FileReaderOption

// WithFileLimitBytes is retained for compatibility.
//
// Deprecated: use mizu.WithFileLimitBytes.
func WithFileLimitBytes(limit int64) FileReaderOption {
	return mizu.WithFileLimitBytes(limit)
}

// NewFileReader is retained for compatibility.
//
// Deprecated: use mizu.NewFileReader.
func NewFileReader(rx io.ReadCloser, opts ...FileReaderOption) *FileReader {
	return mizu.NewFileReader(rx, opts...)
}
