package mizuconnect_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"connectrpc.com/vanguard"
	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	_GREET_SERVICE_NAME   = "test.greet.v1.GreetService"
	_GREET_PATTERN        = "/" + _GREET_SERVICE_NAME + "/"
	_GREET_PROCEDURE      = _GREET_PATTERN + "Greet"
	_AUTH_HEADER          = "Authorization"
	_AUTH_CREDENTIAL      = "Bearer test-token"
	_TEST_PREFIX          = "/api"
	_TEST_SUFFIX          = "/*"
	_VANGUARD_CONTENTTYPE = "application/json"
)

func init() {
	registerTestGreetDescriptor()
}

func registerTestGreetDescriptor() {
	if _, err := protoregistry.GlobalFiles.FindDescriptorByName(_GREET_SERVICE_NAME); err == nil {
		return
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Name:    new("test/greet/v1/greet.proto"),
		Package: new("test.greet.v1"),
		Syntax:  new("proto3"),
		Dependency: []string{
			"google/protobuf/empty.proto",
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: new("GreetService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       new("Greet"),
						InputType:  new(".google.protobuf.Empty"),
						OutputType: new(".google.protobuf.Empty"),
					},
				},
			},
		},
	}

	fd, err := protodesc.NewFile(fileProto, protoregistry.GlobalFiles)
	if err != nil {
		panic(err)
	}
	if err := protoregistry.GlobalFiles.RegisterFile(fd); err != nil {
		panic(err)
	}
}

type greetService interface {
	Greet(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error)
}

type dummyGreetService struct {
	calls int
}

func (s *dummyGreetService) Greet(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	s.calls++
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func newDummyGreetHandler(svc greetService, opts ...connect.HandlerOption) (string, http.Handler) {
	sd, err := protoregistry.GlobalFiles.FindDescriptorByName(_GREET_SERVICE_NAME)
	if err != nil {
		panic(err)
	}
	unary := connect.NewUnaryHandler(
		_GREET_PROCEDURE,
		svc.Greet,
		connect.WithSchema(sd.ParentFile().Services().ByName("GreetService").Methods().ByName("Greet")),
		connect.WithHandlerOptions(opts...),
	)
	return _GREET_PATTERN, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == _GREET_PROCEDURE {
			unary.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

type recordingMux struct {
	inner    *http.ServeMux
	patterns []string
}

func newRecordingMux() *recordingMux {
	return &recordingMux{inner: http.NewServeMux()}
}

func (m *recordingMux) Handle(pattern string, handler http.Handler) {
	m.patterns = append(m.patterns, pattern)
	m.inner.Handle(pattern, handler)
}

func (m *recordingMux) HandleFunc(pattern string, handler http.HandlerFunc) {
	m.patterns = append(m.patterns, pattern)
	m.inner.HandleFunc(pattern, handler)
}

func (m *recordingMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.inner.ServeHTTP(w, r)
}

func (m *recordingMux) Get(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodGet+" "+pattern, handler)
}

func (m *recordingMux) Post(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodPost+" "+pattern, handler)
}

func (m *recordingMux) Put(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodPut+" "+pattern, handler)
}

func (m *recordingMux) Delete(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodDelete+" "+pattern, handler)
}

func (m *recordingMux) Patch(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodPatch+" "+pattern, handler)
}

func (m *recordingMux) Head(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodHead+" "+pattern, handler)
}

func (m *recordingMux) Trace(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodTrace+" "+pattern, handler)
}

func (m *recordingMux) Options(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodOptions+" "+pattern, handler)
}

func (m *recordingMux) Connect(pattern string, handler http.HandlerFunc) {
	m.HandleFunc(http.MethodConnect+" "+pattern, handler)
}

func TestMizuconnect_UseVanguardRouteScope(t *testing.T) {
	tests := []struct {
		name               string
		prefix             string
		suffix             string
		vanguardPattern    string
		reqPath            string
		authHeader         string
		wantStatus         int
		wantPolicyCalls    int
		wantServiceCalls   int
		wantBareRegistered bool
		wantPrefixedRoute  string
	}{
		{
			name:               "bare native procedure without credentials is not routed",
			prefix:             _TEST_PREFIX,
			reqPath:            _GREET_PROCEDURE,
			wantStatus:         http.StatusNotFound,
			wantPolicyCalls:    0,
			wantServiceCalls:   0,
			wantBareRegistered: false,
			wantPrefixedRoute:  _TEST_PREFIX + "/" + _GREET_SERVICE_NAME,
		},
		{
			name:               "prefixed procedure without credentials triggers ingress policy",
			prefix:             _TEST_PREFIX,
			reqPath:            _TEST_PREFIX + _GREET_PROCEDURE,
			wantStatus:         http.StatusUnauthorized,
			wantPolicyCalls:    1,
			wantServiceCalls:   0,
			wantBareRegistered: false,
			wantPrefixedRoute:  _TEST_PREFIX + "/" + _GREET_SERVICE_NAME,
		},
		{
			name:               "prefixed procedure with credentials passes ingress policy",
			prefix:             _TEST_PREFIX,
			reqPath:            _TEST_PREFIX + _GREET_PROCEDURE,
			authHeader:         _AUTH_CREDENTIAL,
			wantStatus:         http.StatusNotFound,
			wantPolicyCalls:    1,
			wantServiceCalls:   0,
			wantBareRegistered: false,
			wantPrefixedRoute:  _TEST_PREFIX + "/" + _GREET_SERVICE_NAME,
		},
		{
			name:               "bare native procedure with suffix is not routed",
			prefix:             _TEST_PREFIX,
			suffix:             _TEST_SUFFIX,
			reqPath:            _GREET_PROCEDURE,
			wantStatus:         http.StatusNotFound,
			wantPolicyCalls:    0,
			wantServiceCalls:   0,
			wantBareRegistered: false,
			wantPrefixedRoute:  _TEST_PREFIX + "/" + _GREET_SERVICE_NAME + _TEST_SUFFIX,
		},
		{
			name:               "prefixed procedure with suffix without credentials triggers policy",
			prefix:             _TEST_PREFIX,
			suffix:             _TEST_SUFFIX,
			reqPath:            _TEST_PREFIX + _GREET_PROCEDURE,
			wantStatus:         http.StatusUnauthorized,
			wantPolicyCalls:    1,
			wantServiceCalls:   0,
			wantBareRegistered: false,
			wantPrefixedRoute:  _TEST_PREFIX + "/" + _GREET_SERVICE_NAME + _TEST_SUFFIX,
		},
		{
			name:               "unprefixed scope still serves native procedure",
			prefix:             "",
			vanguardPattern:    "/",
			reqPath:            _GREET_PROCEDURE,
			wantStatus:         http.StatusOK,
			wantPolicyCalls:    0,
			wantServiceCalls:   1,
			wantBareRegistered: false,
			wantPrefixedRoute:  "/" + _GREET_SERVICE_NAME,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := newRecordingMux()
			srv := mizu.NewServer("test", mizu.WithCustomMux(mux))

			opts := []mizuconnect.Option{
				mizuconnect.WithCrpcVanguard(tc.vanguardPattern),
			}
			if tc.prefix != "" {
				opts = append(opts, mizuconnect.WithPrefix(tc.prefix))
			}
			if tc.suffix != "" {
				opts = append(opts, mizuconnect.WithSuffix(tc.suffix))
			}

			svc := &dummyGreetService{}
			scope := mizuconnect.NewScope(srv, opts...)
			scope.UseVanguard(vanguard.WithTargetProtocols(vanguard.ProtocolConnect)).Register(svc, newDummyGreetHandler)

			baseHandler := srv.Handler()
			policyCalls := 0
			ingress := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, _TEST_PREFIX+"/") {
					policyCalls++
					if r.Header.Get(_AUTH_HEADER) != _AUTH_CREDENTIAL {
						http.Error(w, "unauthorized", http.StatusUnauthorized)
						return
					}
				}
				baseHandler.ServeHTTP(w, r)
			})

			req := httptest.NewRequest(http.MethodPost, tc.reqPath, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", _VANGUARD_CONTENTTYPE)
			req.Header.Set("Connect-Protocol-Version", "1")
			if tc.authHeader != "" {
				req.Header.Set(_AUTH_HEADER, tc.authHeader)
			}
			rec := httptest.NewRecorder()
			ingress.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantPolicyCalls, policyCalls)
			assert.Equal(t, tc.wantServiceCalls, svc.calls)
			if tc.wantBareRegistered {
				assert.Contains(t, mux.patterns, _GREET_PATTERN)
			} else {
				assert.NotContains(t, mux.patterns, _GREET_PATTERN)
			}
			assert.Contains(t, mux.patterns, tc.wantPrefixedRoute)
		})
	}
}

func TestMizuconnect_UseVanguardHealthCheck(t *testing.T) {
	mux := newRecordingMux()
	srv := mizu.NewServer("test", mizu.WithCustomMux(mux))
	scope := mizuconnect.NewScope(
		srv,
		mizuconnect.WithPrefix(_TEST_PREFIX),
		mizuconnect.WithCrpcVanguard(""),
	)
	checker := grpchealth.NewStaticChecker(_GREET_SERVICE_NAME)
	scope.UseVanguard().Register(checker, grpchealth.NewHandler)

	_ = srv.Handler()
	require.NotContains(t, mux.patterns, "/grpc.health.v1.Health/")
	require.Contains(t, mux.patterns, _TEST_PREFIX+"/grpc.health.v1.Health")
}
