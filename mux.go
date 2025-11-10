package mizu

import (
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
)

type multiplexer interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handlerFunc http.HandlerFunc)

	Handler() http.Handler
	Use(middleware func(http.Handler) http.Handler) multiplexer

	Group(prefix string) multiplexer
	Get(pattern string, handler http.HandlerFunc)
	Post(pattern string, handler http.HandlerFunc)
	Put(pattern string, handler http.HandlerFunc)
	Delete(pattern string, handler http.HandlerFunc)
	Patch(pattern string, handler http.HandlerFunc)
	Head(pattern string, handler http.HandlerFunc)
	Trace(pattern string, handler http.HandlerFunc)
	Options(pattern string, handler http.HandlerFunc)
	Connect(pattern string, handler http.HandlerFunc)
}

type mux struct {
	mu       *sync.Mutex // passed from server to prevent concurrent access
	paths    *[]string
	inner    *http.ServeMux
	prefix   string
	buckets  []*bucket // contains the middlewares passed by initializer
	volatile *bucket   // contains the middlewares passed by Use
}

func (m *mux) Handler() http.Handler {
	return m.inner
}

func (m *mux) Use(middleware func(http.Handler) http.Handler) multiplexer {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.volatile != nil {
		m.volatile.Middlewares = append(m.volatile.Middlewares, middleware)
		return m
	}

	mm := &mux{
		mu:     m.mu,
		paths:  m.paths,
		inner:  m.inner,
		prefix: m.prefix,
	}
	b := &bucket{Middlewares: []func(http.Handler) http.Handler{middleware}}

	m.buckets = append(m.buckets, b)

	mm.volatile = b
	mm.buckets = append([]*bucket{}, m.buckets...)
	return mm
}

func (m *mux) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	m.handle("", pattern, handlerFunc)
}

func (m *mux) Handle(pattern string, handler http.Handler) {
	m.handle("", pattern, handler)
}

func (m *mux) Get(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodGet, pattern, handler)
}

func (m *mux) Post(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodPost, pattern, handler)
}

func (m *mux) Put(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodPut, pattern, handler)
}

func (m *mux) Delete(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodDelete, pattern, handler)
}

func (m *mux) Patch(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodPatch, pattern, handler)
}

func (m *mux) Head(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodHead, pattern, handler)
}

func (m *mux) Trace(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodTrace, pattern, handler)
}

func (m *mux) Options(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodOptions, pattern, handler)
}

func (m *mux) Connect(pattern string, handler http.HandlerFunc) {
	m.handle(http.MethodConnect, pattern, handler)
}

func (m *mux) Group(prefix string) multiplexer {
	m.mu.Lock()
	defer m.mu.Unlock()

	mm := &mux{
		mu:      m.mu,
		paths:   m.paths,
		inner:   m.inner,
		prefix:  path.Join(m.prefix, prefix),
		buckets: append([]*bucket{}, m.buckets...),
	}
	m.volatile = nil
	return mm
}

// drain applies all accumulated middlewares in the bucket to the
// given handler and clears the bucket.
func (m *mux) drain() []func(http.Handler) http.Handler {
	var mws []func(http.Handler) http.Handler
	if m.volatile != nil {
		for i := len(m.volatile.Middlewares) - 1; i >= 0; i-- {
			mws = append(mws, m.volatile.Middlewares[i])
		}
		m.volatile.Middlewares = m.volatile.Middlewares[:0]
		m.volatile = nil
	}

	for i := len(m.buckets) - 1; i >= 0; i-- {
		for j := len(m.buckets[i].Middlewares) - 1; j >= 0; j-- {
			mws = append(mws, m.buckets[i].Middlewares[j])
		}
	}

	return mws
}

// handle registers the handler for the given pattern
func (m *mux) handle(method string, pattern string, handler http.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record the registered paths
	path := path.Join(m.prefix, pattern)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		path += string(os.PathSeparator)
	}

	if method != "" {
		path = strings.Join([]string{method, path}, " ")
	}

	if paths := m.paths; paths != nil {
		*paths = append(*paths, path)
	}

	for _, mw := range m.drain() {
		handler = mw(handler)
	}

	m.inner.HandleFunc(strings.TrimSpace(path), handler.ServeHTTP)
}
