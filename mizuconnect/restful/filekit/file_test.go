package filekit_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/humbornjo/mizu/mizuconnect/restful/filekit"
	"github.com/humbornjo/mizu/x/formx"
)

func TestFilekit_FileReaderCompatibility(t *testing.T) {
	var reader *formx.FileReader = filekit.NewFileReader(
		io.NopCloser(bytes.NewReader([]byte("compatibility"))),
		filekit.WithFileLimitBytes(4),
	)
	_, err := io.ReadAll(reader)
	assert.ErrorIs(t, err, filekit.ErrFileTooLarge)
	assert.True(t, errors.Is(err, formx.ErrFileTooLarge))

	var compatibilityReader *filekit.FileReader = formx.NewFileReader(io.NopCloser(bytes.NewReader(nil)))
	assert.NotNil(t, compatibilityReader)
}
