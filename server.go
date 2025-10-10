package mizu

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/humbornjo/mizu/internal"
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

// Server is the main HTTP server that implements the Mux
// interface. It provides HTTP routing, middleware support, and
// graceful shutdown capabilities.
type Server struct {
	internal.Mux

	mu          *sync.Mutex
	initialized atomic.Bool

	ctx                  context.Context
	name                 string
	config               serverConfig
	isShuttingDown       atomic.Bool
	hookOnStartup        []func(context.Context, *Server)
	hookOnExtractHandler []func(context.Context, *Server)
}

// Name returns the name of the server.
func (s *Server) Name() string {
	return s.name
}

// InjectContext modifies the server's initialization context
// using the provided injector function. This context is only
// used during server setup and lifecycle hooks - it has nothing
// to do with request contexts at runtime.
//
// NOTE: This is an advanced function that most users won't need.
// It's primarily used by sub-packages like mizuconnect for
// managing initialization state.
func (s *Server) InjectContext(injector func(context.Context) context.Context) {
	s.ctx = injector(s.ctx)
}

// HookOnStartup registers a hook function that will be called
// right before server starts. Hooks are executed in the order
// they are registered. Useful for logging startup completion,
// displaying registered routes, or performing final
// initialization tasks.
func (s *Server) HookOnStartup(hook func(context.Context, *Server)) {
	s.hookOnStartup = append(s.hookOnStartup, hook)
}

// HookOnExtractHandler registers a hook function that will be
// called when Handler() is invoked. This is useful for
// registering services that need to be available before the
// server starts, or for logging handler extraction phase.
//
// WARNING: Duplicate path registrations will cause panics. Use
// InjectContext with a sync.Once or atomic.Bool to ensure
// idempotent registration.
//
// Example:
//
//	s.InjectContext(func(ctx context.Context) context.Context {
//		once := ctx.Value(ONCE_KEY)
//		if once == nil {
//			ctx = context.WithValue(ctx, ONCE_KEY, &atomic.Bool{})
//		}
//		return ctx
//	})
//	s.HookOnExtractHandler(func(ctx context.Context, srv *Server) {
//		once, _ := ctx.Value(ONCE_KEY).(*atomic.Bool)
//		if once.CompareAndSwap(false, true) {
//			return // Already registered
//		}
//		srv.HandleFunc("/path", handler) // Safe to register
//	})
func (s *Server) HookOnExtractHandler(hook func(context.Context, *Server)) {
	s.hookOnExtractHandler = append(s.hookOnExtractHandler, hook)
}

// Handler returns the base HTTP handler (mux) without
// middlewares. This method will be called before starting the
// server. It can also be used to extract handlers for other
// purposes.
func (s *Server) Handler() http.Handler {
	if s.initialized.CompareAndSwap(false, true) {
		s.HandleFunc(
			s.config.ReadinessPath,
			s.config.WizardHandleReadiness(&s.isShuttingDown),
		)
	}

	for _, hook := range s.hookOnExtractHandler {
		hook(s.ctx, s)
	}

	return s.Mux.Handler()
}

// Uses is a shortcut for chaining multiple middlewares.
func (s *Server) Uses(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler,
) internal.Mux {
	m := s.Use(middleware)
	for _, mw := range more {
		m = m.Use(mw)
	}
	return m
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
	for _, hook := range s.hookOnStartup {
		hook(s.ctx, s)
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
