package mizu

import (
	"net/http"
	"path"
	"sync"

	"github.com/humbornjo/mizu/internal"
)

type mux struct {
	prefix         string
	inner          *Server
	bucketVolatile *middlewareBucket
	bucketPersist  *middlewareBucket
}

func newMux(
	prefix string, server *Server, volatile *middlewareBucket, mws ...func(http.Handler) http.Handler,
) internal.Mux {
	return &mux{
		prefix:         prefix,
		inner:          server,
		bucketVolatile: volatile,
		bucketPersist:  &middlewareBucket{Middlewares: mws},
	}
}

// drainBucket applies all accumulated middlewares in the bucket
// to the given handler and clears the bucket.
func drainBucket(handler http.Handler, m *mux) http.Handler {
	if m.bucketVolatile == nil {
		return handler
	}
	for i := len(m.bucketVolatile.Middlewares) - 1; i >= 0; i-- {
		handler = m.bucketVolatile.Middlewares[i](handler)
	}
	m.bucketVolatile.Middlewares = m.bucketVolatile.Middlewares[:0]
	m.bucketVolatile = nil

	if m.bucketPersist != nil {
		for i := len(m.bucketPersist.Middlewares) - 1; i >= 0; i-- {
			handler = m.bucketPersist.Middlewares[i](handler)
		}
	}
	return handler
}

func (m *mux) Use(middleware func(http.Handler) http.Handler) internal.Mux {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	if m.bucketVolatile == nil {
		panic("middlewares already consumed")
	}
	m.bucketVolatile.Middlewares = append(m.bucketVolatile.Middlewares, middleware)
	return m
}

func (m *mux) Handle(pattern string, handler http.Handler) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Handle(path.Join(m.prefix, pattern), drainBucket(handler, m))
}

func (m *mux) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.HandleFunc(path.Join(m.prefix, pattern), drainBucket(handlerFunc, m).ServeHTTP)
}

func (m *mux) Get(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Get(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Post(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Post(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Put(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Put(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Delete(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Delete(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Patch(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Patch(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Head(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Head(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Trace(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Trace(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Options(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Options(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Connect(pattern string, handler http.HandlerFunc) {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	m.inner.Connect(path.Join(m.prefix, pattern), drainBucket(handler, m).ServeHTTP)
}

func (m *mux) Group(prefix string) internal.Mux {
	m.inner.mu.Lock()
	defer m.inner.mu.Unlock()
	return newGroupMux(prefix, m.inner, &middlewareBucket{
		Middlewares: append(m.bucketPersist.Middlewares, m.bucketVolatile.Middlewares...),
	})
}

type groupMux struct {
	mux *Server
	mu  sync.Mutex

	prefix  string
	buckets []*middlewareBucket
}

func newGroupMux(prefix string, server *Server, bucket *middlewareBucket) internal.Mux {
	return &groupMux{mux: server, prefix: prefix, buckets: []*middlewareBucket{bucket}}
}

func (m *groupMux) middleware() func(http.Handler) http.Handler {
	m.mu.Lock()
	defer m.mu.Unlock()

	return func(handler http.Handler) http.Handler {
		for i := len(m.buckets) - 1; i >= 0; i-- {
			bucket := m.buckets[i]
			for j := len(bucket.Middlewares) - 1; j >= 0; j-- {
				m := bucket.Middlewares[j]
				handler = m(handler)
			}
		}
		return handler
	}
}

func (m *groupMux) Use(middleware func(http.Handler) http.Handler) internal.Mux {
	m.mu.Lock()
	defer m.mu.Unlock()

	bucket := &middlewareBucket{
		Middlewares: []func(http.Handler) http.Handler{middleware},
	}
	m.buckets = append(m.buckets, bucket)
	return newMux(m.prefix, m.mux, nil, m.middleware())
}

func (m *groupMux) Handle(pattern string, handler http.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Handle(path.Join(m.prefix, pattern), m.middleware()(handler))
}

func (m *groupMux) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.HandleFunc(path.Join(m.prefix, pattern), m.middleware()(handlerFunc).ServeHTTP)
}

func (m *groupMux) Get(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Get(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Post(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Post(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Put(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Put(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Delete(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Delete(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Patch(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Patch(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Head(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Head(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Trace(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Trace(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Options(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Options(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Connect(pattern string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux.Connect(path.Join(m.prefix, pattern), m.middleware()(handler).ServeHTTP)
}

func (m *groupMux) Group(prefix string) internal.Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	return newGroupMux(path.Join(m.prefix, prefix), m.mux, &middlewareBucket{
		Middlewares: []func(http.Handler) http.Handler{m.middleware()},
	})
}
