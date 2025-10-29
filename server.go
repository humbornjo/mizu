package mizu

import (
	"context"
	"log"
	"net"
	"net/http"
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
	CustomCleanupFns      []func()
	ServerProtocols       *http.Protocols
	ShutdownPeriod        time.Duration
	ShutdownHardPeriod    time.Duration
	ReadinessDrainDelay   time.Duration
	ReadinessPath         string
	WizardHandleReadiness func(isShuttingDown *atomic.Bool) http.HandlerFunc
}

var _ Mux = (*Server)(nil)

// Server is the main HTTP server that implements the Mux
// interface. It provides HTTP routing, middleware support, and
// graceful shutdown capabilities.
type Server struct {
	inner Mux

	mu  *sync.Mutex // mutex for server initialization
	mmu *sync.Mutex // mutex for the mux that server binds to

	initialized    atomic.Bool
	isShuttingDown atomic.Bool

	ctx         context.Context
	name        string
	config      serverConfig
	hookStartup []func(*Server)
	hookHandler []func(*Server)
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

// WithHookHandler registers a hook function when Calling
// Handler.
func WithHookHandler(hook func(*Server)) hookOption {
	return func(config *hookConfig) {
		config.hookHandler = hook
	}
}

// Hook registers a hook function for the given key. If value is
// not nil, it will be registered as the value for the key.
// HookOption offer customization options for performing
// additional actions on different phases in server lifecycle.
// Returned value is the registered value for the key if value is
// not nil, otherwise triggers panic.
//
// WARN: This is a advanced function which in most cases should
// not be used.
func Hook[K any, V any](s *Server, key K, val *V, opts ...hookOption) *V {
	s.mu.Lock()
	defer s.mu.Unlock()
	if val != nil {
		s.ctx = context.WithValue(s.ctx, key, val)
	}

	if v := s.ctx.Value(key); v != nil {
		val = v.(*V)
	} else {
		panic("value not found")
	}

	config := &hookConfig{}
	for _, opt := range opts {
		opt(config)
	}
	if config.hookHandler != nil {
		s.hookHandler = append(s.hookHandler, config.hookHandler)
	}
	if config.hookStartup != nil {
		s.hookStartup = append(s.hookStartup, config.hookStartup)
	}

	return val
}

// Handler returns the base HTTP handler (mux) without
// middlewares. This method will be called before starting the
// server. It can also be used to extract handlers for other
// purposes.
func (s *Server) Handler() http.Handler {
	if s.initialized.CompareAndSwap(false, true) {
		s.inner.HandleFunc(
			s.config.ReadinessPath,
			s.config.WizardHandleReadiness(&s.isShuttingDown),
		)
	}

	for _, hook := range s.hookHandler {
		hook(s)
	}

	return s.inner.Handler()
}

// ServeContext starts the HTTP server on the given address and
// blocks until the context is cancelled. It handles graceful
// shutdown when the context is cancelled, draining connections
// before stopping.
func (s *Server) ServeContext(ctx context.Context, addr string) error {
	var server *http.Server
	var shutdownPeriod = s.config.ShutdownPeriod
	var shutdownHardPeriod = s.config.ShutdownHardPeriod
	var tickerReadinessDrainDelay = s.config.ReadinessDrainDelay

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
			BaseContext: func(_ net.Listener) context.Context {
				return ingCtx
			},
		}
	}
	if s.config.ServerProtocols != nil {
		server.Protocols = s.config.ServerProtocols
	}
	server.Handler = s.Handler()

	log.Println("🚀 [INFO] Starting HTTP server on", addr)
	for _, hook := range s.hookStartup {
		hook(s)
	}

	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Println("🚨 [ERROR] Server exited unexpectedly:", err)
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		s.isShuttingDown.Store(true)
		log.Println("✅ [INFO] Server shutting down...")

		// Give time for readiness check to propagate
		log.Println("🕸️ [INFO] Draining readiness check before shutdown...")
		<-time.After(tickerReadinessDrainDelay)
		log.Println("✅ [INFO] Readiness drained. Waiting for ongoing requests to finish...")

		// Shutdown Server, waiting for ongoing requests to finish
		downCtx, downCancel := context.WithTimeout(context.Background(), shutdownPeriod)
		defer downCancel()
		err := server.Shutdown(downCtx)

		// Cancel in-flight requests, disable it or customize it by setting http.Server via WithCustomHttpServer
		ingCancel()

		// Custom cleanup functions from WithCustomHttpServer, this block is mutually exclusive with ingCancel
		if s.config.CustomCleanupFns != nil {
			for _, fn := range s.config.CustomCleanupFns {
				fn()
			}
		}

		if err != nil {
			log.Println("⚠️ [WARN] Graceful shutdown failed:", err)
			time.Sleep(shutdownHardPeriod)
			return err
		}
		log.Println("✅ [INFO] Server shut down gracefully.")
	}

	return nil
}

func (s *Server) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	s.inner.HandleFunc(pattern, handlerFunc)
}

func (s *Server) Handle(pattern string, handler http.Handler) {
	s.inner.Handle(pattern, handler)
}

func (s *Server) Get(pattern string, handler http.HandlerFunc) {
	s.inner.Get(pattern, handler)
}

func (s *Server) Post(pattern string, handler http.HandlerFunc) {
	s.inner.Post(pattern, handler)
}

func (s *Server) Put(pattern string, handler http.HandlerFunc) {
	s.inner.Put(pattern, handler)
}

func (s *Server) Delete(pattern string, handler http.HandlerFunc) {
	s.inner.Delete(pattern, handler)
}

func (s *Server) Patch(pattern string, handler http.HandlerFunc) {
	s.inner.Patch(pattern, handler)
}

func (s *Server) Head(pattern string, handler http.HandlerFunc) {
	s.inner.Head(pattern, handler)
}

func (s *Server) Trace(pattern string, handler http.HandlerFunc) {
	s.inner.Trace(pattern, handler)
}

func (s *Server) Options(pattern string, handler http.HandlerFunc) {
	s.inner.Options(pattern, handler)
}

func (s *Server) Connect(pattern string, handler http.HandlerFunc) {
	s.inner.Connect(pattern, handler)
}

func (s *Server) Group(prefix string) Mux {
	return s.inner.Group(prefix)
}

func (s *Server) Use(middleware func(http.Handler) http.Handler) Mux {
	return s.inner.Use(middleware)
}

// Uses is a shortcut for chaining multiple middlewares.
func (s *Server) Uses(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler,
) Mux {
	m := s.inner.Use(middleware)
	for _, mw := range more {
		m = m.Use(mw)
	}
	return m
}
