package filekit

import (
	"bufio"
	"sync"
)

type Pool[T any] struct {
	sync.Pool
}

func newpool[T any](f func() T) *Pool[T] {
	return &Pool[T]{Pool: sync.Pool{New: func() any {
		return f()
	}}}
}

func (p *Pool[T]) Get() T {
	return p.Pool.Get().(T)
}

func (p *Pool[T]) Put(val T) {
	p.Pool.Put(val)
}

var (
	fieldMutex sync.RWMutex
	fieldPools map[int64]*Pool[[]byte]

	readerPool *Pool[*bufio.Reader]
	writerPool *Pool[*bufio.Writer]
)

func init() {
	fieldPools = make(map[int64]*Pool[[]byte])

	readerPool = newpool(func() *bufio.Reader {
		return bufio.NewReader(nil)
	})

	writerPool = newpool(func() *bufio.Writer {
		return bufio.NewWriter(nil)
	})
}
