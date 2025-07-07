package mizu

import (
	"net/http"
)

type Mux interface {
	Get(pattern string, handler http.HandlerFunc)
	Post(pattern string, handler http.HandlerFunc)
	Put(pattern string, handler http.HandlerFunc)
	Delete(pattern string, handler http.HandlerFunc)
	Patch(pattern string, handler http.HandlerFunc)
	Head(pattern string, handler http.HandlerFunc)
	Trace(pattern string, handler http.HandlerFunc)
	Options(pattern string, handler http.HandlerFunc)
	Connect(pattern string, handler http.HandlerFunc)
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handlerFunc http.HandlerFunc)
	Use(middleware func(http.Handler) http.Handler) Mux
}

type mux struct {
	inner  *Server
	bucket *middlewareBucket
}

func newMux(server *Server, ms *middlewareBucket) Mux {
	return &mux{inner: server, bucket: ms}
}

// drainBucket applies all accumulated middlewares in the bucket
// to the given handler and clears the bucket.
func drainBucket(handler http.Handler, m *mux) http.Handler {
	if m.bucket == nil {
		return handler
	}
	for i := len(m.bucket.Middlewares) - 1; i >= 0; i-- {
		handler = m.bucket.Middlewares[i](handler)
	}
	m.bucket.Middlewares = m.bucket.Middlewares[:0]
	m.bucket = nil
	return handler
}

func (m *mux) Use(middleware func(http.Handler) http.Handler) Mux {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	if m.bucket == nil {
		// TODO: Add best practice example in README.md
		panic("middlewares already consumed, see https://github.com/humborn/mizu/tree/main/README.md#Chaining-Middlewares")
	}
	m.bucket.Middlewares = append(m.bucket.Middlewares, middleware)
	return m
}

func (m *mux) Handle(pattern string, handler http.Handler) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Handle(pattern, drainBucket(handler, m))
}

func (m *mux) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.HandleFunc(pattern, drainBucket(handlerFunc, m).ServeHTTP)
}

func (m *mux) Get(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Get(pattern, drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Post(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Post(pattern, drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Put(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Put(pattern, drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Delete(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Delete(pattern, drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Patch(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Patch(pattern, drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Head(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Head(pattern, drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Trace(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Trace(pattern, drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Options(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Options(pattern, drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Connect(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Connect(pattern, drainBucket(handler, m).ServeHTTP)
}
