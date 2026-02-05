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

// WithHookStartup registers a hook function when Calling
// ServeContext.
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

// Hook registers a hook function for the given key. If value is not
// nil, it will be registered as the value for the key. HookOption
// offer customization options for performing additional actions on
// different phases in server lifecycle. Returned value is the
// registered value for the key if value is not nil, otherwise nil
// pointer is returned.
//
// WARN: This is a advanced function which in most cases should not be
// used.
func Hook[K any, V any](s *Server, key K, val *V, opts ...hookOption) *V {
	s.mu.Lock()
	defer s.mu.Unlock()
	if val != nil {
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

	if v := s.ctx.Value(key); v != nil {
		return v.(*V)
	}
	return nil
}

// Immediate offer the typed value for the given key for user to
// access in closure, this access is concurrent safe.
//
// WARN: This is a advanced function which in most cases should not be
// used.
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

// Group add a prefix to the following serving patterns. Apply chained
// Use before Group to apply the middleware group-wise.
//
// Example:
//
//	    // middleware mw will be applied to all routes in the group
//			group := mizui.Use(mw).Group("/api")
//
//		  group.Get("/user", handlerUser)
//		  group.Get("/goods", handlerGoods)
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

// Use adds a middleware to the server. You can either consume the
// middleware in chained manner or leave it and make it apply to all
// the routes added after it.
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
			for i := len(s.volatile.Middlewares) - 1; i >= 0; i-- {
				if !yield(s.volatile.Middlewares[i]) {
					return
				}
			}
			s.volatile.Middlewares = s.volatile.Middlewares[:0]
			s.volatile = nil
		}
		for i := len(s.buckets) - 1; i >= 0; i-- {
			for j := len(s.buckets[i].Middlewares) - 1; j >= 0; j-- {
				if !yield(s.buckets[i].Middlewares[j]) {
					return
				}
			}
		}
	}
}
