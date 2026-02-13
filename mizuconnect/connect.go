package mizuconnect

import (
	"context"
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

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/humbornjo/mizu"
)

type ctxkey int

const (
	_CTXKEY_SERVICE_NAMES ctxkey = iota
	_CTXKEY_CRPC_VANGUARD
	_CTXKEY_GRPC_HEALTH
	_CTXKEY_GRPC_REFLECT
	_CTXKEY_GRPC_GATEWAY
)

var (
	_DEFAULT_CONFIG = config{
		enableCrpcVanguard: false,
		enableGrpcHealth:   false,
		enableGrpcReflect:  false,
	}
)

type config struct {
	prefix      string
	suffix      string
	connectOpts []connect.HandlerOption

	enableGrpcHealth bool

	enableGrpcReflect bool
	reflectOpts       []connect.HandlerOption

	enableCrpcVanguard     bool
	vanguardPattern        string
	vanguardTranscoderOpts []vanguard.TranscoderOption

	enableGrpcGateway bool
	gatewayMux        *runtime.ServeMux
	gatewayPort       string
	gatewayPattern    string
	gatewayContext    context.Context
}

// Option configures the mizuconnect scope.
type Option func(*config)

// WithCrpcValidate enables buf proto validation for the registered
// services.
func WithCrpcValidate() Option {
	return func(m *config) {
		interceptor := validate.NewInterceptor()
		m.connectOpts = append(m.connectOpts, connect.WithInterceptors(interceptor))
	}
}

// WithGrpcHealth enables gRPC health checks for the registered
func WithGrpcHealth() Option {
	return func(m *config) {
		m.enableGrpcHealth = true
	}
}

// WithGrpcReflect enables gRPC reflection for the registered services.
// This allows clients to discover service definitions at runtime.
func WithGrpcReflect(opts ...connect.HandlerOption) Option {
	return func(m *config) {
		m.enableGrpcReflect = true
		m.reflectOpts = append(m.reflectOpts, opts...)
	}
}

// WithGrpcGateway enables gRPC gateway for the registered services.
// ctx controls the lifespan of the proxy connections (see
// `Register[SERVICE]HandlerFromEndpoint` of the gRPC gateway compiling
// output). port is where the requests will be proxied. It should be
// the same port used by the ConnectRPC server or gRPC server.
func WithGrpcGateway(ctx context.Context, pattern string, port string, opts ...runtime.ServeMuxOption) Option {
	return func(m *config) {
		if ctx == nil {
			ctx = context.Background()
		}
		m.enableGrpcGateway = true
		m.gatewayContext = ctx
		m.gatewayPort = port
		m.gatewayPattern = pattern
		m.gatewayMux = runtime.NewServeMux(opts...)
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
//
// WARN: prefix will not be applied to gRPC-reflection and gRPC-health.
func WithPrefix(prefix string) Option {
	return func(m *config) {
		m.prefix = prefix
	}
}

// WithSuffix sets the suffix for the scope. All service registered in
// the scope will inherit this suffix.
func WithSuffix(suffix string) Option {
	return func(m *config) {
		m.suffix = suffix
	}
}

// Scope is a mizu scope for Connect RPC services over mizu.Server.
// Multiple scopes can be derived from a single mizu.Server as long as
// the routes are well-managed. Transcoder like Vanguard and gRPC-gateway
// is considered scope-wise and should be registered with care.
type Scope struct {
	srv *mizu.Server

	config           *config
	vanguardServices []*vanguard.Service
}

// NewScope creates a new Connect RPC scope with the given mizu server.
// The scope manages registration of Connect services with optional
// features like health checks, reflection, validation, and Vanguard
// transcoding.
//
// One mizu Server can derive multiple scopes, all the scope share the
// same registered service names slices, which comes with the side
// effect that one scope with feature enabled (e.g., reflection) will
// also reveal all the services registered in other scopes.
func NewScope(srv *mizu.Server, opts ...Option) *Scope {
	config := _DEFAULT_CONFIG
	for _, opt := range opts {
		opt(&config)
	}
	scope := &Scope{srv: srv, config: &config}

	serviceNames := mizu.Hook(srv, _CTXKEY_SERVICE_NAMES, &[]string{})
	if config.enableGrpcReflect {
		once := sync.Once{}
		mizu.Hook(srv, _CTXKEY_GRPC_REFLECT, &once, mizu.WithHookHandler(func(srv *mizu.Server) {
			once.Do(func() {
				reflector := grpcreflect.NewStaticReflector(*serviceNames...)
				if scope.config.suffix == "" {
					srv.Handle(grpcreflect.NewHandlerV1(reflector, scope.config.reflectOpts...))
					srv.Handle(grpcreflect.NewHandlerV1Alpha(reflector, scope.config.reflectOpts...))
				} else {
					pv1, hv1 := grpcreflect.NewHandlerV1(reflector, scope.config.reflectOpts...)
					srv.Handle(path.Join(pv1, scope.config.suffix), hv1)
					pv1a, hv1a := grpcreflect.NewHandlerV1Alpha(reflector, scope.config.reflectOpts...)
					srv.Handle(path.Join(pv1a, scope.config.suffix), hv1a)
				}
			})
		}))
	}

	if config.enableGrpcHealth {
		once := sync.Once{}
		mizu.Hook(srv, _CTXKEY_GRPC_HEALTH, &once, mizu.WithHookHandler(func(srv *mizu.Server) {
			once.Do(func() {
				checker := grpchealth.NewStaticChecker(*serviceNames...)
				if scope.config.suffix == "" {
					srv.Handle(grpchealth.NewHandler(checker))
				} else {
					pc, hc := grpchealth.NewHandler(checker)
					srv.Handle(path.Join(pc, scope.config.suffix), hc)
				}
			})
		}))
	}

	if config.enableCrpcVanguard {
		once := sync.Once{}
		mizu.Hook(srv, _CTXKEY_CRPC_VANGUARD, &once, mizu.WithHookHandler(func(srv *mizu.Server) {
			once.Do(func() {
				pattern := path.Join(scope.config.prefix, "/", scope.config.suffix)
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

	if config.enableGrpcGateway {
		once := sync.Once{}
		mizu.Hook(srv, _CTXKEY_GRPC_GATEWAY, &once, mizu.WithHookHandler(func(srv *mizu.Server) {
			once.Do(func() {
				pattern := path.Join(scope.config.prefix, "/", scope.config.suffix)
				if scope.config.gatewayPattern != "" {
					pattern = scope.config.gatewayPattern
				}
				srv.Handle(pattern, scope.config.gatewayMux)
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

	mizu.Immediate(s.srv, _CTXKEY_SERVICE_NAMES, func(v *[]string) {
		*v = append(*v, fullyQualifiedServiceName)
	})

	// Register vanguard service
	if s.config.enableCrpcVanguard {
		vanService := vanguard.NewService(pattern, handler)
		s.vanguardServices = append(s.vanguardServices, vanService)
	}

	// Register service
	if s.config.suffix == "" {
		s.srv.Handle(path.Join(s.config.prefix, pattern), handler)
	} else {
		s.srv.Handle(path.Join(s.config.prefix, pattern, s.config.suffix), handler)
	}
}

type relayVanguardScope struct {
	inner   *Scope
	svcOpts []vanguard.ServiceOption
}

// UseVanguard creates a new relay scope with the given service option.
// svcOpts will only be applied to the following registered service.
// The Scope level service options should be configured with
// WithCrpcVanguard("/", vanguard.WithDefaultServiceOptions(...)) on
// initialization.
func (s *Scope) UseVanguard(svcOpts ...vanguard.ServiceOption) *relayVanguardScope {
	if !s.config.enableCrpcVanguard {
		panic("invalid call: vanguard is not enabled")
	}
	return &relayVanguardScope{inner: s, svcOpts: svcOpts}
}

// Register registers a Connect RPC service with the relay scope.
// Which will apply vanguard service options to the registered service.
func (s relayVanguardScope) Register(impl any, newFunc any, opts ...connect.HandlerOption) {
	opts = append(opts, s.inner.config.connectOpts...)

	pattern, handler := invoke(impl, newFunc, opts...)
	fullyQualifiedServiceName, _ := detect(pattern)

	mizu.Immediate(s.inner.srv, _CTXKEY_SERVICE_NAMES, func(v *[]string) {
		*v = append(*v, fullyQualifiedServiceName)
	})

	// Register vanguard service
	if s.inner.config.enableCrpcVanguard {
		vanService := vanguard.NewService(pattern, handler, s.svcOpts...)
		s.inner.vanguardServices = append(s.inner.vanguardServices, vanService)
	}

	// Register service
	s.inner.srv.Handle(pattern, handler)
}

type relayGatewayScope struct {
	inner        *Scope
	dialOpts     []grpc.DialOption
	registerFunc func(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) (err error)
}

// UseGateway creates a new relay scope with the given gRPC gateway
// configuration. dialOpts will only be applied to the following
// registered service.
func (s *Scope) UseGateway(
	registerEndpointFunc func(
		ctx context.Context,
		mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) (err error),
	dialOpts ...grpc.DialOption,
) *relayGatewayScope {
	return &relayGatewayScope{inner: s, dialOpts: dialOpts, registerFunc: registerEndpointFunc}
}

// Register registers a Connect RPC service with the relay scope.
// Which will apply gRPC gateway configuration to the registered service.
func (r *relayGatewayScope) Register(impl any, newFunc any, opts ...connect.HandlerOption) {
	if r.inner.config.gatewayMux == nil {
		panic("gRPC-gateway is not enabled")
	}

	if err := r.registerFunc(
		r.inner.config.gatewayContext,
		r.inner.config.gatewayMux, "127.0.0.1"+r.inner.config.gatewayPort, r.dialOpts,
	); err != nil {
		panic(err)
	}
	r.inner.Register(impl, newFunc, opts...)
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
