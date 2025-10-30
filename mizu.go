package mizu

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ctxkey int

const _CTXKEY ctxkey = iota

const (
	_READINESS_DRAIN_DELAY = 5 * time.Second
	_SHUTDOWN_PERIOD       = 15 * time.Second
	_SHUTDOWN_HARD_PERIOD  = 3 * time.Second
)

var (
	_DEFAULT_SERVER_CONFIG = serverConfig{
		ShutdownPeriod:      _SHUTDOWN_PERIOD,
		ShutdownHardPeriod:  _SHUTDOWN_HARD_PERIOD,
		ReadinessDrainDelay: _READINESS_DRAIN_DELAY,
		ReadinessPath:       "GET /healthz",
		WizardHandleReadiness: func(isShuttingDown *atomic.Bool) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if isShuttingDown.Load() {
					http.Error(w, "Shutting down", http.StatusServiceUnavailable)
					return
				}
				_, _ = fmt.Fprintln(w, "OK")
			}
		},
	}

	// PROTOCOLS_HTTP2 supports HTTP/1 and HTTP/2 (both TLS and
	// H2C)
	PROTOCOLS_HTTP2 http.Protocols

	// PROTOCOLS_HTTP2_UNENCRYPTED supports HTTP/1 and unencrypted
	// HTTP/2
	PROTOCOLS_HTTP2_UNENCRYPTED http.Protocols
)

func init() {
	protocols := http.Protocols{}
	protocols.SetHTTP1(true)

	protocols.SetUnencryptedHTTP2(true)
	PROTOCOLS_HTTP2_UNENCRYPTED = protocols

	protocols.SetHTTP2(true)
	PROTOCOLS_HTTP2 = protocols
}

// NewServer creates a new mizu HTTP server with the given
// service name and options. The service name is used for logging
// and debugging purposes. Default configuration includes 5s
// readiness drain delay (k8s integration), graceful shutdown
// with 15s timeout and 3s hard timeout.
func NewServer(srvName string, opts ...Option) *Server {
	var config = new(config)
	*config = func(s *Server) *Server {
		s.config = _DEFAULT_SERVER_CONFIG
		return s
	}

	for _, opt := range opts {
		opt(config)
	}

	server := &Server{
		mu:  &sync.Mutex{},
		mmu: &sync.Mutex{},
		ctx: context.WithValue(
			context.Background(),
			_CTXKEY, new([]string),
		),
		name:           srvName,
		initialized:    atomic.Bool{},
		isShuttingDown: atomic.Bool{},
	}
	server.initialized.Store(false)
	server.isShuttingDown.Store(false)

	server.inner = &mux{inner: http.NewServeMux(), server: server}
	return (*config)(server)
}

// WithReadinessDrainDelay sets the delay before starting
// graceful shutdown. This allows load balancers and health
// checks time to detect the server is shutting down.
func WithReadinessDrainDelay(d time.Duration) Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)
			s.config.ReadinessDrainDelay = d
			return s
		}
		*m = new
	}
}

// WithShutdownPeriod sets the timeout for graceful shutdown. The
// server will wait this long for ongoing requests to complete
// before forcing shutdown.
func WithShutdownPeriod(d time.Duration) Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)
			s.config.ShutdownPeriod = d
			return s
		}
		*m = new
	}
}

// WithHardShutdownPeriod sets the timeout for hard shutdown
// after graceful shutdown fails. This is the final wait time
// before the server process terminates.
func WithHardShutdownPeriod(d time.Duration) Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)
			s.config.ShutdownHardPeriod = d
			return s
		}
		*m = new
	}
}

// WithPrometheusMetrics enables Prometheus metrics collection by
// registering the /metrics endpoint with the default Prometheus
// handler.
func WithPrometheusMetrics() Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)
			s.Handle("/metrics", promhttp.Handler())
			return s
		}
		*m = new
	}
}

// WithServerProtocols sets the server protocols to use.
func WithServerProtocols(protocols http.Protocols) Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)
			s.config.ServerProtocols = &protocols
			return s
		}
		*m = new
	}
}

// WithCustomHttpServer allows using a custom http.Server instead
// of the default one. This gives full control over server
// configuration like timeouts, TLS, etc. cleanupFns are called
// after the server completes shutdown, it is commonly used to
// stop the in flight requests (e.g. context.CancelFunc).
func WithCustomHttpServer(server *http.Server, cleanupFns ...func()) Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)
			s.config.CustomServer = server
			s.config.CustomCleanupFns = cleanupFns
			return s
		}
		*m = new
	}
}

// WithWizardHandleReadiness sets a custom readiness check
// handler. The wizard function receives the server's shutdown
// state and should return an HTTP handler that responds to
// readiness/health check requests.
func WithWizardHandleReadiness(pattern string, wizard func(*atomic.Bool) http.HandlerFunc) Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)
			s.config.ReadinessPath = pattern
			s.config.WizardHandleReadiness = wizard
			return s
		}
		*m = new
	}
}

// WithProfilingHandlers enables Go's built-in pprof profiling
// endpoints. This registers handlers at /debug/pprof/* for CPU,
// memory, goroutine profiling, etc. Should only be enabled in
// development or with proper access controls.
func WithProfilingHandlers() Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)
			s.HandleFunc("/debug/pprof/", pprof.Index)
			s.HandleFunc("/debug/pprof/trace", pprof.Trace)
			s.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			s.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			s.HandleFunc("/debug/pprof/profile", pprof.Profile)
			s.Handle("/debug/pprof/heap", pprof.Handler("heap"))
			s.Handle("/debug/pprof/block", pprof.Handler("block"))
			s.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
			s.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
			return s
		}
		*m = new
	}
}

// WithDisplayRoutesOnStartup enables logging of all registered
// routes when the server starts. This is useful for debugging
// and development to see what endpoints are available.
func WithRevealRoutes() Option {
	return func(m *config) {
		old := *m
		new := func(s *Server) *Server {
			s = old(s)

			routes := new([]string)
			Hook(s, _CTXKEY, routes, WithHookStartup(func(s *Server) {
				fmt.Println("📦 [INFO] Available routes:")

				slices.Sort(*routes)
				for _, path := range *routes {
					method := ""
					uri := path
					if fields := strings.Fields(path); len(fields) == 2 {
						method, uri = fields[0], fields[1]
					}
					if method == "" {
						fmt.Printf("     ➤ 📍 %-7s %s\n", "*", uri)
					} else {
						fmt.Printf("     ➤ 📍 %-7s %s\n", method, uri)
					}
				}
			}))
			return s
		}
		*m = new
	}
}
