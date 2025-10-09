package mizu

import (
	"context"
	"net/http"
	"path"

	"github.com/humbornjo/mizu/internal"
)

type mux struct {
	inner    *http.ServeMux
	prefix   string
	server   *Server
	buckets  []*bucket // contains the middlewares passed by initializer
	volatile *bucket   // contains the middlewares passed by Use
}

func (m *mux) Handler() http.Handler {
	return m.inner
}

func (m *mux) Middleware() func(http.Handler) http.Handler {
	m.server.mu.Lock()
	defer m.server.mu.Unlock()

	mws := m.drainBucket()
	for i := len(m.buckets) - 1; i >= 0; i-- {
		for j := len(m.buckets[i].Middlewares) - 1; j >= 0; j-- {
			mws = append(mws, m.buckets[i].Middlewares[j])
		}
	}

	return func(h http.Handler) http.Handler {
		for _, mw := range mws {
			h = mw(h)
		}
		return h
	}
}

// drainBucket applies all accumulated middlewares in the bucket
// to the given handler and clears the bucket.
func (m *mux) drainBucket() []func(http.Handler) http.Handler {
	var mws []func(http.Handler) http.Handler
	if m.volatile != nil {
		for i := len(m.volatile.Middlewares) - 1; i >= 0; i-- {
			mws = append(mws, m.volatile.Middlewares[i])
		}
		m.volatile.Middlewares = m.volatile.Middlewares[:0]
		m.volatile = nil
	}

	return mws
}

func (m *mux) Use(middleware func(http.Handler) http.Handler) internal.Mux {
	m.server.mu.Lock()
	defer m.server.mu.Unlock()

	if m.volatile != nil {
		m.volatile.Middlewares = append(m.volatile.Middlewares, middleware)
	} else {
		m.volatile = &bucket{Middlewares: []func(http.Handler) http.Handler{middleware}}
		m.buckets = append(m.buckets, m.volatile)
	}
	return m
}

func (m *mux) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	m.server.mu.Lock()
	defer m.server.mu.Unlock()

	// Record the registered paths
	m.server.InjectContext(func(ctx context.Context) context.Context {
		value := ctx.Value(_CTXKEY)
		if value == nil {
			return context.WithValue(ctx, _CTXKEY, &[]string{pattern})
		}

		paths, ok := value.(*[]string)
		if !ok {
			panic("unreachable")
		}
		*paths = append(*paths, pattern)
		return ctx
	})

	mws := m.drainBucket()
	var handler http.Handler = handlerFunc
	for _, mw := range mws {
		handler = mw(handler)
	}

	m.inner.HandleFunc(path.Join(m.prefix, pattern), handler.ServeHTTP)
}

func (m *mux) Handle(pattern string, handler http.Handler) {
	m.HandleFunc(pattern, handler.ServeHTTP)
}

func (m *mux) Get(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodGet+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Post(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodPost+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Put(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodPut+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Delete(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodDelete+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Patch(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodPatch+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Head(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodHead+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Trace(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodTrace+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Options(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodOptions+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Connect(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodConnect+" "+pattern, handler.ServeHTTP)
}

func (m *mux) Group(prefix string) internal.Mux {
	m.server.mu.Lock()
	defer m.server.mu.Unlock()

	m.volatile = nil
	return &mux{
		inner:   m.inner,
		prefix:  path.Join(m.prefix, prefix),
		server:  m.server,
		buckets: append([]*bucket{}, m.buckets...),
	}
}
