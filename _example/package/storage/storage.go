package storage

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
)

type File interface {
	io.ReadCloser
	Checksum() string
	ContentType() string
}

type Instance interface {
	Store(ctx context.Context, file File) (string, error)
	Retrieve(ctx context.Context, id string) (File, error)
}

type sfile struct {
	data        []byte
	size        int64
	checksum    string
	contentType string
}

func (f *sfile) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func (f *sfile) Close() error {
	return nil
}

func (f *sfile) Size() int64 {
	return f.size
}

func (f *sfile) Checksum() string {
	return f.checksum
}

func (f *sfile) ContentType() string {
	return f.contentType
}

type storage struct {
	inner sync.Map
}

func NewStorage() Instance {
	return &storage{}
}

func (s *storage) Store(ctx context.Context, file File) (string, error) {
	defer file.Close() // nolint: errcheck
	bytes, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	f := sfile{
		data:        bytes,
		checksum:    file.Checksum(),
		contentType: file.ContentType(),
	}

	s.inner.Store(file.Checksum(), f)
	return file.Checksum(), nil
}

func (s *storage) Retrieve(ctx context.Context, id string) (File, error) {
	f, ok := s.inner.Load(id)
	if !ok {
		return nil, errors.New("file not found")
	}

	ff := f.(sfile)
	ff.data = slices.Clone(ff.data)
	return &ff, nil
}
