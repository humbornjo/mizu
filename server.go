package mizu

import (
	"context"
	"fmt"
	"iter"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Option configures the mizu server.
type Option func(*config)

type config func(*Server) *Server

type bucket struct {
	Middlewares []func(http.Handler) http.Handler
}

type serverConfig struct {
	CustomServer          *http.Server
	CustomCleanupFuncs    []func()
	ServerProtocols       *http.Protocols
	ShutdownPeriod        time.Duration
	ShutdownHardPeriod    time.Duration
	ReadinessDrainDelay   time.Duration
	ReadinessPath         string
	WizardHandleReadiness func(isShuttingDown *atomic.Bool) http.HandlerFunc
}

// Server is the main HTTP server that implements the Mux interface.
// It provides HTTP routing, middleware support, and graceful shutdown
// capabilities.
type Server struct {
	inner Mux

	mu  *sync.Mutex // mutex for server initialization
	mmu *sync.Mutex // mutex passed down to mux for concurrent registration

	initialized    *atomic.Bool
	isShuttingDown *atomic.Bool

	ctx         context.Context
	name        string
	config      *serverConfig
	hookStartup *[]func(*Server)
	hookHandler *[]func(*Server)

	prefix   []string
	buckets  []*bucket
	volatile *bucket
}

// Name returns the name of the server.
func (s *Server) Name() string {
	return s.name
}

type hookOption func(*hookConfig)

type hookConfig struct {
	hookStartup func(*Server)
	hookHandler func(*Server)
}

// WithHookStartup registers a hook function when Calling ServeContext.
func WithHookStartup(hook func(*Server)) hookOption {
	return func(config *hookConfig) {
		config.hookStartup = hook
	}
}

// WithHookHandler registers a hook function when Calling Handler.
func WithHookHandler(hook func(*Server)) hookOption {
	return func(config *hookConfig) {
		config.hookHandler = hook
	}
}

// Hook binds a value to the given key and returns the bound value.
// If the key is already bound, the existing value is returned and
// val is ignored. Otherwise val is bound and returned; a nil val
// binds nothing and returns nil. Use Immediate for a read-only
// lookup.
//
// HookOption offer customization options for performing additional
// actions on different phases in server lifecycle.
//
// WARN: This is advanced function which should be used with caution.
func Hook[K any, V any](s *Server, key K, val *V, opts ...hookOption) *V {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ret *V
	if v := s.ctx.Value(key); v != nil {
		ret = v.(*V)
	} else if val != nil {
		ret = val
		s.ctx = context.WithValue(s.ctx, key, val)
	}

	config := &hookConfig{}
	for _, opt := range opts {
		opt(config)
	}
	if config.hookHandler != nil {
		*s.hookHandler = append(*s.hookHandler, config.hookHandler)
	}
	if config.hookStartup != nil {
		*s.hookStartup = append(*s.hookStartup, config.hookStartup)
	}

	return ret
}

// Immediate offer the typed value for the given key for user to
// access in closure, this access is concurrently safe.
//
// WARN: This is advanced function which should be used with caution.
func Immediate[K any, V any](s *Server, key K, closure func(*V)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if v := s.ctx.Value(key); v != nil {
		closure(v.(*V))
		return
	}
	closure(nil)
}

// Handler returns the base HTTP handler (mux) without middlewares.
// This method will be called before starting the server. It can also
// be used to extract handlers for other purposes.
func (s *Server) Handler() http.Handler {
	if s.initialized.CompareAndSwap(false, true) {
		s.Get(s.config.ReadinessPath, s.config.WizardHandleReadiness(s.isShuttingDown))
	}

	for _, hook := range *s.hookHandler {
		hook(s)
	}

	return s.inner
}

// ServeContext starts the HTTP server on the given address and blocks
// until the context is cancelled. It handles graceful shutdown when
// the context is cancelled, draining connections before stopping.
func (s *Server) ServeContext(ctx context.Context, addr string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	var server *http.Server
	var shutdownPeriod = s.config.ShutdownPeriod
	var shutdownHardPeriod = s.config.ShutdownHardPeriod
	var ReadinessDrainDelayPeriod = s.config.ReadinessDrainDelay

	ingCtx, ingCancel := context.WithCancel(context.Background())
	defer ingCancel()
	if s.config.CustomServer != nil {
		server = s.config.CustomServer
	} else {
		server = &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 15 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       300 * time.Second,
			BaseContext:       func(_ net.Listener) context.Context { return ingCtx },
		}
	}
	if s.config.ServerProtocols != nil {
		server.Protocols = s.config.ServerProtocols
	}
	server.Handler = s.Handler()

	fmt.Println("🚀 [INFO] Starting HTTP server on", addr)
	for _, hook := range *s.hookStartup {
		hook(s)
	}

	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Println("🚨 [ERROR] Server exited unexpectedly:", err)
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		stop()

		s.isShuttingDown.Store(true)
		fmt.Println("✅ [INFO] Server shutting down...")

		if ReadinessDrainDelayPeriod > 0 {
			// Give time for readiness check to propagate
			fmt.Println("🕸️ [INFO] Draining readiness check before shutdown...")
			<-time.After(ReadinessDrainDelayPeriod)
			fmt.Println("✅ [INFO] Readiness drained. Waiting for ongoing requests to finish...")
		}

		// Shutdown Server, waiting for ongoing requests to finish
		downCtx, downCancel := context.WithTimeout(context.Background(), shutdownPeriod)
		defer downCancel()
		err := server.Shutdown(downCtx)

		// Custom cleanup functions from WithCustomHttpServer, mutually exclusive with ingCancel
		for _, cleanupHookFunc := range s.config.CustomCleanupFuncs {
			cleanupHookFunc()
		}

		// Cancel in-flight requests, disable it or customize it via WithCustomHttpServer
		ingCancel()

		if err != nil {
			fmt.Println("⚠️ [WARN] Graceful shutdown failed:", err)
			time.Sleep(shutdownHardPeriod)
			return err
		}
		fmt.Println("✅ [INFO] Server shutdown gracefully.")
	}

	return nil
}

func (s *Server) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handlerFunc
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add("", registeredPath, s.prefix...)
		}
		s.inner.HandleFunc(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Handle(pattern string, handler http.Handler) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add("", registeredPath, s.prefix...)
		}
		s.inner.Handle(registeredPath, registeredFunc)
	})
}

func (s *Server) Get(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodGet, registeredPath, s.prefix...)
		}
		s.inner.Get(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Post(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodPost, registeredPath, s.prefix...)
		}
		s.inner.Post(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Put(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodPut, registeredPath, s.prefix...)
		}
		s.inner.Put(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Delete(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodDelete, registeredPath, s.prefix...)
		}
		s.inner.Delete(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Patch(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodPatch, registeredPath, s.prefix...)
		}
		s.inner.Patch(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Head(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodHead, registeredPath, s.prefix...)
		}
		s.inner.Head(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Trace(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodTrace, registeredPath, s.prefix...)
		}
		s.inner.Trace(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Options(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodOptions, registeredPath, s.prefix...)
		}
		s.inner.Options(registeredPath, registeredFunc.ServeHTTP)
	})
}

func (s *Server) Connect(pattern string, handler http.HandlerFunc) {
	registeredPath := path.Join(append(s.prefix, pattern)...)
	if pattern != string(os.PathSeparator) &&
		strings.TrimSuffix(pattern, string(os.PathSeparator)) != pattern {
		registeredPath += string(os.PathSeparator)
	}

	var registeredFunc http.Handler = handler
	Immediate(s, _CTXKEY, func(v *routes) {
		for mw := range s.drain() {
			registeredFunc = mw(registeredFunc)
		}
		if v != nil {
			v.add(http.MethodConnect, registeredPath, s.prefix...)
		}
		s.inner.Connect(registeredPath, registeredFunc.ServeHTTP)
	})
}

// Group returns a server that adds prefix to every pattern registered
// through it. Middlewares already added to the receiver are inherited.
//
// To scope a middleware to the group alone, call Use on the group and
// discard the return value:
//
//	group := srv.Group("/api")
//	group.Use(mw) // discarded: mw covers every route in the group
//
//	group.Get("/user", handlerUser)
//	group.Get("/goods", handlerGoods)
//
// Chaining instead (srv.Group("/api").Use(mw)) captures a one-shot
// chain: only the next route registered through it gets the
// middleware. See Use.
func (s *Server) Group(prefix string) *Server {
	s.mmu.Lock()
	defer s.mmu.Unlock()

	ss := *s
	ss.prefix = append(s.prefix, prefix)
	ss.volatile = nil
	ss.buckets = append([]*bucket{}, s.buckets...)

	s.volatile = nil
	return &ss
}

// Pattern returns the registered pattern with prefix
func (s *Server) Pattern(pattern string) string {
	return path.Join(append(s.prefix, pattern)...)
}

// Use adds a middleware to the server. What the middleware covers
// depends on whether the return value is kept.
//
// Discard the return and the middleware is persistent: it applies to
// every route registered afterward on the receiver, including the
// routes of groups derived from it.
//
// Keep the return and it is a one-shot chain: the next route
// registered through the chain consumes the middleware, and routes
// registered later get nothing. Chained Uses accumulate until the
// first route registration:
//
//	srv.Use(mw)              // persistent: every later route gets mw
//	srv.Use(mw).Get("/a", h) // one-shot: only /a gets mw
//
// Beware srv.Group("/api").Use(mw): the chain is kept, so only the
// group's first route gets the middleware. Use the two-statement form
// shown in Group to cover a whole group.
func (s *Server) Use(middleware func(http.Handler) http.Handler) *Server {
	s.mmu.Lock()
	defer s.mmu.Unlock()

	if s.volatile != nil {
		s.volatile.Middlewares = append(s.volatile.Middlewares, middleware)
		return s
	}

	ss := *s

	b := &bucket{Middlewares: []func(http.Handler) http.Handler{middleware}}
	s.buckets = append(s.buckets, b)

	ss.volatile = b
	ss.buckets = append([]*bucket{}, s.buckets...)
	return &ss
}

// Uses is a shortcut for chaining multiple middlewares.
func (s *Server) Uses(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler,
) *Server {
	ss := s.Use(middleware)
	for _, mw := range more {
		ss = ss.Use(mw)
	}
	return ss
}

// drain applies all accumulated middlewares in the bucket to the
// given handler and clears the bucket.
func (s *Server) drain() iter.Seq[func(http.Handler) http.Handler] {
	return func(yield func(func(http.Handler) http.Handler) bool) {
		if s.volatile != nil {
			for _, middleware := range slices.Backward(s.volatile.Middlewares) {
				if !yield(middleware) {
					return
				}
			}
			s.volatile.Middlewares = s.volatile.Middlewares[:0]
			s.volatile = nil
		}
		for _, bucket := range slices.Backward(s.buckets) {
			for _, middleware := range slices.Backward(bucket.Middlewares) {
				if !yield(middleware) {
					return
				}
			}
		}
	}
}
