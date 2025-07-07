package mizuconnect

import (
	"context"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"

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
	_CTXKEY_HEALTH ctxkey = iota
	_CTXKEY_VANGUARD

	_CTXKEY_VANGUARD_PATTERN
	_CTXKEY_VANGUARD_TRANSCODER_OPTS

	_CTXKEY_HEALTH_INITIALIZED
	_CTXKEY_VANGUARD_INITIALIZED
)

var (
	_DEFAULT_CONFIG = config{
		enableVanguard:     false,
		enableProtoHealth:  false,
		enableProtoReflect: false,
	}
)

type config struct {
	enableVanguard     bool
	enableProtoHealth  bool
	enableProtoReflect bool

	connectOpts            []connect.HandlerOption
	reflectOpts            []connect.HandlerOption
	vanguardPattern        string
	vanguardServiceOpts    []vanguard.ServiceOption
	vanguardTranscoderOpts []vanguard.TranscoderOption
}

// Option configures the mizuconnect scope.
type Option func(*config)

// WithVanguard enables Vanguard transcoding for REST API
// compatibility. This allows Connect RPC services to be accessed
// via HTTP/JSON. The pattern parameter specifies the path,
// should be mounted on "/" in most cases to achieve RESTful.
//
// Example:
//
//	scope.WithVanguard("/", nil, nil)
func WithVanguard(pattern string, svcOpts []vanguard.ServiceOption, transOpts []vanguard.TranscoderOption) Option {
	return func(m *config) {
		m.enableVanguard = true
		m.vanguardPattern = pattern
		m.vanguardServiceOpts = append(m.vanguardServiceOpts, svcOpts...)
		m.vanguardTranscoderOpts = append(m.vanguardTranscoderOpts, transOpts...)
	}
}

// WithHealth enables gRPC health checks for the registered
// services.
func WithHealth() Option {
	return func(m *config) {
		m.enableProtoHealth = true
	}
}

// WithReflect enables gRPC reflection for the registered
// services. This allows clients to discover service definitions
// at runtime.
func WithReflect(opts ...connect.HandlerOption) Option {
	return func(m *config) {
		m.enableProtoReflect = true
		m.reflectOpts = append(m.reflectOpts, opts...)
	}
}

// WithValidate enables buf proto validation for the registered
// services.
func WithValidate() Option {
	return func(m *config) {
		interceptor, err := validate.NewInterceptor()
		if err != nil {
			panic(err)
		}
		m.connectOpts = append(m.connectOpts, connect.WithInterceptors(interceptor))
	}
}

// WithHandlerOptions adds Connect handler options that will be
// applied to all registered services in this scope.
func WithHandlerOptions(opts ...connect.HandlerOption) Option {
	return func(m *config) {
		m.connectOpts = append(m.connectOpts, opts...)
	}
}

type scope struct {
	*mizu.Server
	config config
}

// NewScope creates a new Connect RPC scope with the given mizu
// server. The scope manages registration of Connect services
// with optional features like health checks, reflection,
// validation, and Vanguard transcoding.
func NewScope(srv *mizu.Server, opts ...Option) *scope {
	config := _DEFAULT_CONFIG
	for _, opt := range opts {
		opt(&config)
	}

	return &scope{
		Server: srv,
		config: config,
	}
}

// Register registers a Connect RPC service with the scope. impl
// is the service implementation, newFunc is the generated
// Connect constructor (e.g., greetv1connect.NewGreetServiceHandler),
// and opts are additional handler options. The service is
// automatically configured with validation, health checks,
// reflection, and Vanguard transcoding based on the scope's
// configuration.
//
// Example:
//
//	scope := mizuconnect.NewScope(server)
//	impl := &GreetServiceImpl{}
//	scope.Register(impl, greetv1connect.NewGreetServiceHandler)
func (s *scope) Register(impl any, newFunc any, opts ...connect.HandlerOption) {
	opts = append(opts, s.config.connectOpts...)

	pattern, handler := invoke(impl, newFunc, opts...)
	sd := detect(pattern)

	// Register grpcreflect
	if s.config.enableProtoReflect {
		reflector := grpcreflect.NewStaticReflector(string(sd.Name()))
		s.Handle(grpcreflect.NewHandlerV1(reflector, s.config.reflectOpts...))
		s.Handle(grpcreflect.NewHandlerV1Alpha(reflector, s.config.reflectOpts...))
	}

	// Register grpchealth
	if s.config.enableProtoHealth {
		s.InjectContext(func(ctx context.Context) context.Context {
			once := ctx.Value(_CTXKEY_HEALTH_INITIALIZED)
			if once == nil {
				ctx = context.WithValue(ctx, _CTXKEY_HEALTH_INITIALIZED, &atomic.Bool{})
			}

			value := ctx.Value(_CTXKEY_HEALTH)
			if value == nil {
				return context.WithValue(ctx, _CTXKEY_HEALTH, &[]string{string(sd.Name())})
			}
			services, ok := value.(*[]string)
			if !ok {
				panic("invalid value in context")
			}
			*services = append(*services, string(sd.Name()))
			return ctx
		})

		s.HookOnExtractHandler(func(ctx context.Context, server *mizu.Server) {
			once, _ := ctx.Value(_CTXKEY_HEALTH_INITIALIZED).(*atomic.Bool)
			if once.CompareAndSwap(true, false) {
				return
			}

			value := ctx.Value(_CTXKEY_HEALTH)
			if value == nil {
				return
			}
			serviceNames, ok := value.(*[]string)
			if !ok {
				panic("unreachable")
			}
			checker := grpchealth.NewStaticChecker(*serviceNames...)
			server.Handle(grpchealth.NewHandler(checker))
		})
	}

	// Register vanguard transcoder
	if s.config.enableVanguard {
		vanService := vanguard.NewService(pattern, handler, s.config.vanguardServiceOpts...)
		s.InjectContext(func(ctx context.Context) context.Context {
			valueOnce := ctx.Value(_CTXKEY_VANGUARD_INITIALIZED)
			if valueOnce == nil {
				ctx = context.WithValue(ctx, _CTXKEY_VANGUARD_INITIALIZED, &atomic.Bool{})
			}

			valuePattern := ctx.Value(_CTXKEY_VANGUARD_PATTERN)
			if valuePattern == nil {
				ctx = context.WithValue(ctx, _CTXKEY_VANGUARD_PATTERN, s.config.vanguardPattern)
			} else {
				log.Printf("⚠️ [WARN] Vanguard pattern get reset.")
				ctx = context.WithValue(ctx, _CTXKEY_VANGUARD_PATTERN, s.config.vanguardPattern)
			}

			valueTransOpts := ctx.Value(_CTXKEY_VANGUARD_TRANSCODER_OPTS)
			if valueTransOpts == nil {
				ctx = context.WithValue(ctx, _CTXKEY_VANGUARD_TRANSCODER_OPTS, s.config.vanguardTranscoderOpts)
			} else {
				log.Println("⚠️ [WARN] Vanguard transcoder options get reset.")
				ctx = context.WithValue(ctx, _CTXKEY_VANGUARD_TRANSCODER_OPTS, s.config.vanguardTranscoderOpts)
			}

			value := ctx.Value(_CTXKEY_VANGUARD)
			if value == nil {
				return context.WithValue(ctx, _CTXKEY_VANGUARD, &[]*vanguard.Service{vanService})
			}
			services, ok := value.(*[]*vanguard.Service)
			if !ok {
				panic("invalid value in context")
			}
			*services = append(*services, vanService)
			return ctx
		})

		s.HookOnExtractHandler(func(ctx context.Context, server *mizu.Server) {
			once, _ := ctx.Value(_CTXKEY_VANGUARD_INITIALIZED).(*atomic.Bool)
			if once.CompareAndSwap(true, false) {
				return
			}

			pattern := "/"
			valuePattern := ctx.Value(_CTXKEY_VANGUARD_PATTERN)
			if valuePattern != nil {
				pattern = valuePattern.(string)
			}

			opts := []vanguard.TranscoderOption{}
			valueTransOpts := ctx.Value(_CTXKEY_VANGUARD_TRANSCODER_OPTS)
			if valueTransOpts != nil {
				opts = valueTransOpts.([]vanguard.TranscoderOption)
			}

			value := ctx.Value(_CTXKEY_VANGUARD)
			if value == nil {
				return
			}
			services, ok := value.(*[]*vanguard.Service)
			if !ok {
				panic("unreachable")
			}
			transcoder, err := vanguard.NewTranscoder(*services, opts...)
			if err != nil {
				panic(err)
			}
			server.Handle(pattern, transcoder)
		})
	}

	// Register service
	s.Handle(pattern, handler)
}

// detect extracts the protobuf service descriptor from the
// Connect service pattern. It looks up the service in the global
// protobuf registry to enable features like health checks and
// reflection.
func detect(pattern string) protoreflect.ServiceDescriptor {
	nameSvc := strings.Trim(pattern, "/")
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(nameSvc))
	if err != nil {
		panic("descriptor not found:" + " " + nameSvc)
	}

	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		panic("descriptor not indicates service:" + " " + nameSvc)
	}
	return sd
}

// invoke dynamically calls the Connect handler constructor
// function using reflection. It validates the function signature
// and arguments, then returns the service pattern and HTTP
// handler. This allows for type-safe registration of any Connect
// service without requiring code generation for each service
// type.
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
