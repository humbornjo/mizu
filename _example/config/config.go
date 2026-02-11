package config

import (
	"context"
	// "context"
	"errors"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizulog"
	"github.com/humbornjo/mizu/mizuoai"
	"github.com/humbornjo/mizu/mizuotel"
	"google.golang.org/protobuf/encoding/protojson"

	"mizu.example/package/debug"
	"mizu.example/protogen"
)

const ServiceName = "example-app"

type Config struct {
	Env   string `yaml:"env"`
	Port  string `yaml:"port"`
	Level string `yaml:"level"`
}

func Initialize(paths ...string) {
	// Dependency Injection --------------------------------------------
	if err := mizudi.Initialize("config", paths...); err != nil {
		panic(err)
	}

	if err := mizudi.RevealConfig(os.Stdout); err != nil {
		if !errors.Is(err, mizudi.ErrNotInitialized) {
			panic(err)
		}
	}

	c := mizudi.Enchant[Config](nil)
	mizudi.Register(func() (*Config, error) { return c, nil })

	// Server ----------------------------------------------------------
	server := mizu.NewServer(
		ServiceName,

		// You can even use chi.Mux, as long as you don't mind sort out
		// the differences of the routing rules.
		mizu.WithCustomMux(chi.NewMux()),

		mizu.WithRevealRoutes(),
		mizu.WithProfilingHandlers(),
		mizu.WithReadinessDrainDelay(-1*time.Second),

		// Force Protocol can useful when dev locally
		// (Go STD use HTTP/1 by default when TLS is disabled)
		mizu.WithServerProtocols(mizu.PROTOCOLS_HTTP2_UNENCRYPTED),
	)
	mizudi.Register(func() (*mizu.Server, error) { return server, nil })

	// Connect RPC -----------------------------------------------------
	scope := mizuconnect.NewScope(server,
		// Use wildcard when enable chi.Mux
		mizuconnect.WithSuffix("/*"),

		mizuconnect.WithCrpcValidate(),
		mizuconnect.WithGrpcHealth(),
		mizuconnect.WithGrpcReflect(),

		// Use either vanguard or gRPC-gateway as REST transcoder
		mizuconnect.WithGrpcGateway(
			context.TODO(), "", c.Port,
			// multipart/form-data
			runtime.WithMarshalerOption("multipart/form-data", &runtime.HTTPBodyMarshaler{
				Marshaler: &runtime.JSONPb{
					MarshalOptions: protojson.MarshalOptions{
						EmitUnpopulated: false,
					},
					UnmarshalOptions: protojson.UnmarshalOptions{
						DiscardUnknown: true,
					},
				},
			}),
		),
		// mizuconnect.WithCrpcVanguard(""),

		mizuconnect.WithCrpcHandlerOptions(
			connect.WithInterceptors(debug.NewInterceptor()),
		),
	)
	mizudi.Register(func() (*mizuconnect.Scope, error) { return scope, nil })

	// OPENAPI ---------------------------------------------------------
	if err := mizuoai.Initialize(server, "mizu_example",
		mizuoai.WithOaiDocumentation(),
		mizuoai.WithOaiPreLoad(protogen.OPENAPI)); err != nil {
		panic(err)
	}

	// Opentelemetry ---------------------------------------------------
	if err := mizuotel.Initialize(); err != nil {
		panic(err)
	}

	// Logging ---------------------------------------------------------
	mizulog.Initialize(nil, mizulog.WithLogLevel(c.Level))

	// Other Registrations ---------------------------------------------
	// e.g. Register Default Database using mizudi.Register and use
	// them across services.
	// ...
}
