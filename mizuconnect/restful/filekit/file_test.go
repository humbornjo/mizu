package filekit_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuconnect/restful/filekit"
	"github.com/stretchr/testify/assert"
)

func TestFilekit_FileReaderCompatibility(t *testing.T) {
	var reader *mizu.FileReader = filekit.NewFileReader(
		io.NopCloser(bytes.NewReader([]byte("compatibility"))),
		filekit.WithFileLimitBytes(4),
	)
	_, err := io.ReadAll(reader)
	assert.ErrorIs(t, err, filekit.ErrFileTooLarge)
	assert.True(t, errors.Is(err, mizu.ErrFileTooLarge))

	var compatibilityReader *filekit.FileReader = mizu.NewFileReader(io.NopCloser(bytes.NewReader(nil)))
	assert.NotNil(t, compatibilityReader)
}
