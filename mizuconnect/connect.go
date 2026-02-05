package mizuconnect

import (
	"net/http"
	"path"
	"reflect"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"connectrpc.com/grpcreflect"
	"connectrpc.com/validate"
	"connectrpc.com/vanguard"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/humbornjo/mizu"
)

type ctxkey int

const (
	_CTXKEY_GRPC_HEALTH ctxkey = iota
	_CTXKEY_GRPC_REFLECT
	_CTXKEY_CRPC_VANGUARD
)

var (
	_DEFAULT_CONFIG = config{
		enableCrpcVanguard: false,
		enableGrpcHealth:   false,
		enableGrpcReflect:  false,
	}
)

type config struct {
	prefix string

	enableCrpcVanguard bool
	enableGrpcHealth   bool
	enableGrpcReflect  bool

	grpcHealthSubPattern  string
	grpcReflectSubPattern string
	vanguardPattern       string

	connectOpts            []connect.HandlerOption
	reflectOpts            []connect.HandlerOption
	vanguardTranscoderOpts []vanguard.TranscoderOption
}

// Option configures the mizuconnect scope.
type Option func(*config)

// WithGrpcHealth enables gRPC health checks for the registered
// services. subPattern is used to either:
//
//   - Restrict the access to a specific path, or
//   - Conform to the routing rule of customized Mux
//
// In most cases, subPattern should simply be "".
func WithGrpcHealth(subPattern string) Option {
	return func(m *config) {
		m.enableGrpcHealth = true
		m.grpcHealthSubPattern = subPattern
	}
}

// WithGrpcReflect enables gRPC reflection for the registered services.
// This allows clients to discover service definitions at runtime.
// subPattern is used to either:
//
//   - Restrict the access to a specific path, or
//   - Conform to the routing rule of customized Mux
//
// In most cases, subPattern should simply be "".
func WithGrpcReflect(subPattern string, opts ...connect.HandlerOption) Option {
	return func(m *config) {
		m.enableGrpcReflect = true
		m.grpcReflectSubPattern = subPattern
		m.reflectOpts = append(m.reflectOpts, opts...)
	}
}

// WithCrpcValidate enables buf proto validation for the registered
// services.
func WithCrpcValidate() Option {
	return func(m *config) {
		interceptor := validate.NewInterceptor()
		m.connectOpts = append(m.connectOpts, connect.WithInterceptors(interceptor))
	}
}

// WithCrpcVanguard enables Vanguard transcoding for REST API
// compatibility. This allows Connect RPC services to be accessed via
// HTTP/JSON. The pattern parameter specifies the path, should be
// mounted on "/" in most cases. Service Option can be applied with
// vanguard.WithDefaultServiceOptions or scope.Uses, therefore only
// transcoder options are required on initializing scope.
//
// WARN: If pattern is non-empty, it will override the default
// Vanguard pattern without applying scope prefix.
//
// Example:
//
//	scope.WithCrpcVanguard("")
func WithCrpcVanguard(pattern string, transOpts ...vanguard.TranscoderOption) Option {
	return func(m *config) {
		m.enableCrpcVanguard = true
		m.vanguardPattern = pattern
		m.vanguardTranscoderOpts = append(m.vanguardTranscoderOpts, transOpts...)
	}
}

// WithCrpcHandlerOptions adds Connect handler options that will be
// applied to all registered services in this scope.
func WithCrpcHandlerOptions(opts ...connect.HandlerOption) Option {
	return func(m *config) {
		m.connectOpts = append(m.connectOpts, opts...)
	}
}

// WithPrefix sets the prefix for the scope. All service registered in
// the scope will inherit this prefix. Prefix will also apply to
// Vanguard pattern if Vanguard is enabled and the pattern is not
// provided (Default of Vanguard pattern is `prefix`).
func WithPrefix(prefix string) Option {
	return func(m *config) {
		m.prefix = prefix
	}
}

type Scope struct {
	srv *mizu.Server

	config           *config
	serviceNames     []string
	vanguardServices []*vanguard.Service
}

// NewScope creates a new Connect RPC scope with the given mizu server.
// The scope manages registration of Connect services with optional
// features like health checks, reflection, validation, and Vanguard
// transcoding.
func NewScope(srv *mizu.Server, opts ...Option) *Scope {
	config := _DEFAULT_CONFIG
	for _, opt := range opts {
		opt(&config)
	}

	scope := &Scope{
		srv:    srv,
		config: &config,
	}

	if config.enableGrpcReflect {
		once := sync.Once{}
		mizu.Hook(srv, _CTXKEY_GRPC_REFLECT, &once, mizu.WithHookHandler(func(srv *mizu.Server) {
			once.Do(func() {
				reflector := grpcreflect.NewStaticReflector(scope.serviceNames...)
				if scope.config.grpcReflectSubPattern == "" {
					srv.Handle(grpcreflect.NewHandlerV1(reflector, scope.config.reflectOpts...))
					srv.Handle(grpcreflect.NewHandlerV1Alpha(reflector, scope.config.reflectOpts...))
				} else {
					pv1, hv1 := grpcreflect.NewHandlerV1(reflector, scope.config.reflectOpts...)
					srv.Handle(path.Join(pv1, scope.config.grpcReflectSubPattern), hv1)
					pv1a, hv1a := grpcreflect.NewHandlerV1Alpha(reflector, scope.config.reflectOpts...)
					srv.Handle(path.Join(pv1a, scope.config.grpcReflectSubPattern), hv1a)
				}
			})
		}))
	}

	if config.enableGrpcHealth {
		once := sync.Once{}
		mizu.Hook(srv, _CTXKEY_GRPC_HEALTH, &once, mizu.WithHookHandler(func(srv *mizu.Server) {
			once.Do(func() {
				checker := grpchealth.NewStaticChecker(scope.serviceNames...)
				if scope.config.grpcHealthSubPattern == "" {
					srv.Handle(grpchealth.NewHandler(checker))
				} else {
					pc, hc := grpchealth.NewHandler(checker)
					srv.Handle(path.Join(pc, scope.config.grpcHealthSubPattern), hc)
				}
			})
		}))
	}

	if config.enableCrpcVanguard {
		once := sync.Once{}
		mizu.Hook(srv, _CTXKEY_CRPC_VANGUARD, &once, mizu.WithHookHandler(func(srv *mizu.Server) {
			once.Do(func() {
				pattern := path.Join(scope.config.prefix, "/")
				if scope.config.vanguardPattern != "" {
					pattern = scope.config.vanguardPattern
				}
				transcoder, err := vanguard.NewTranscoder(scope.vanguardServices, scope.config.vanguardTranscoderOpts...)
				if err != nil {
					panic(err)
				}
				srv.Handle(pattern, transcoder)
			})
		}))
	}

	return scope
}

// Register registers a Connect RPC service with the scope. impl is
// the service implementation, newFunc is the generated Connect
// constructor (e.g., greetv1connect.NewGreetServiceHandler), and opts
// are additional handler options. The service is automatically
// configured with validation, health checks, reflection, and Vanguard
// transcoding based on the scope's configuration.
//
// Example:
//
//	scope := mizuconnect.NewScope(server)
//	impl := &GreetServiceImpl{}
//	scope.Register(impl, greetv1connect.NewGreetServiceHandler)
func (s *Scope) Register(impl any, newFunc any, opts ...connect.HandlerOption) {
	opts = append(opts, s.config.connectOpts...)

	pattern, handler := invoke(impl, newFunc, opts...)
	fullyQualifiedServiceName, _ := detect(pattern)
	s.serviceNames = append(s.serviceNames, fullyQualifiedServiceName)

	// Register vanguard service
	if s.config.enableCrpcVanguard {
		vanService := vanguard.NewService(pattern, handler)
		s.vanguardServices = append(s.vanguardServices, vanService)
	}

	// Register service
	s.srv.Handle(path.Join(s.config.prefix, pattern), handler)
}

type relayScope struct {
	inner   *Scope
	svcOpts []vanguard.ServiceOption
}

// Use creates a new relay scope with the given service option.
// ServiceOption will not be applied to all the following registered
// services. The Scopt level service options should be configured with
// WithCrpcVanguard("/", vanguard.WithDefaultServiceOptions(...)) on
// initialization.
func (s *Scope) Use(svcOpt vanguard.ServiceOption) *relayScope {
	if !s.config.enableCrpcVanguard {
		panic("invalid call: vanguard is not enabled")
	}
	return &relayScope{inner: s, svcOpts: []vanguard.ServiceOption{svcOpt}}
}

// Uses creates a new relay scope with the given service options.
// ServiceOption will not be applied to all the following registered
// services. The Scopt level service options should be configured with
// WithCrpcVanguard("/", vanguard.WithDefaultServiceOptions(...)) on
// initialization.
func (s *Scope) Uses(svcOpts ...vanguard.ServiceOption) *relayScope {
	if !s.config.enableCrpcVanguard {
		panic("invalid call: vanguard is not enabled")
	}
	return &relayScope{inner: s, svcOpts: svcOpts}
}

// Register registers a Connect RPC service with the relay scope.
// Which will apply vanguard service options to the registered service.
func (s relayScope) Register(impl any, newFunc any, opts ...connect.HandlerOption) {
	opts = append(opts, s.inner.config.connectOpts...)

	pattern, handler := invoke(impl, newFunc, opts...)
	fullyQualifiedServiceName, _ := detect(pattern)
	s.inner.serviceNames = append(s.inner.serviceNames, fullyQualifiedServiceName)

	// Register vanguard service
	if s.inner.config.enableCrpcVanguard {
		vanService := vanguard.NewService(pattern, handler, s.svcOpts...)
		s.inner.vanguardServices = append(s.inner.vanguardServices, vanService)
	}

	// Register service
	s.inner.srv.Handle(pattern, handler)
}

// detect extracts the protobuf service descriptor from the Connect
// service pattern. It looks up the service in the global protobuf
// registry to enable features like health checks and reflection.
func detect(pattern string) (string, protoreflect.ServiceDescriptor) {
	nameSvc := strings.Trim(pattern, "/")
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(nameSvc))
	if err != nil {
		panic("descriptor not found:" + " " + nameSvc)
	}

	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		panic("descriptor not indicates service:" + " " + nameSvc)
	}
	return nameSvc, sd
}

// invoke dynamically calls the Connect handler constructor function
// using reflection. It validates the function signature and arguments,
// then returns the service pattern and HTTP handler. This allows for
// type-safe registration of any Connect service without requiring
// code generation for each service type.
func invoke(impl any, newFunc any, opts ...connect.HandlerOption) (string, http.Handler) {
	reflectImpl := reflect.ValueOf(impl)
	reflectFunc := reflect.ValueOf(newFunc)

	if reflectFunc.Kind() != reflect.Func {
		panic("newFunc must be a function")
	}

	// Ensure legal input signature
	if reflectFunc.Type().NumIn() != 2 {
		panic("connect NewHandler function take 2 argument")
	}
	reflectFuncArg1 := reflectFunc.Type().In(0)
	reflectFuncArg2 := reflectFunc.Type().In(1)

	// check first argument qualification
	if !reflectImpl.Type().Implements(reflectFuncArg1) {
		panic("first argument of connect NewHandler function must be the service implementation")
	}

	// check second argument qualification
	if reflectFuncArg2.Kind() != reflect.Slice ||
		!reflectFuncArg2.Elem().Implements(reflect.TypeOf((*connect.HandlerOption)(nil)).Elem()) {
		panic("second argument of connect NewHandler function must be elipses slice of connect.HandlerOption")
	}

	// Ensure legal output signature
	if reflectFunc.Type().NumOut() != 2 {
		panic("connect NewHandler function must return 2 values")
	}
	reflectFuncRet1 := reflectFunc.Type().Out(0)
	reflectFuncRet2 := reflectFunc.Type().Out(1)

	// check first return value
	if reflectFuncRet1.Kind() != reflect.String {
		panic("first return value of connect NewHandler function must be a string")
	}

	// check second return value
	if !reflectFuncRet2.Implements(reflect.TypeOf((*http.Handler)(nil)).Elem()) {
		panic("second return value of connect NewHandler function must be http.Handler")
	}

	// Call connect NewHandler function
	args := []reflect.Value{reflect.ValueOf(impl)}
	for _, opt := range opts {
		args = append(args, reflect.ValueOf(opt))
	}
	ret := reflectFunc.Call(args)

	return ret[0].Interface().(string), ret[1].Interface().(http.Handler)
}
