package mizu

import (
	"net/http"
	"strings"
	"sync"
)

// INFO: It should be noted that Mux is just a abstraction, it is not
// tied to the http.ServeMux, instead, it can be any implementation.
// It is planned to support delegate the registration to Mux
// provided by user in the future.
type Mux interface {
	http.Handler

	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handlerFunc http.HandlerFunc)
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

var _ Mux = (*mux)(nil)

type mux struct {
	mu    *sync.Mutex // passed from server to prevent concurrent access
	inner *http.ServeMux
}

func (m *mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.inner.ServeHTTP(w, r)
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

// handle registers the handler for the given pattern with prefix and middlewares from server
func (m *mux) handle(method string, pattern string, handler http.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := pattern

	// Add method prefix if specified
	if method != "" {
		path = strings.Join([]string{method, path}, " ")
	}

	m.inner.HandleFunc(path, handler.ServeHTTP)
}
