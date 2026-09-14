package config

import (
	"context"
	"errors"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizuconnect/restful/filekit"
	"github.com/humbornjo/mizu/mizucue"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/x/logx"
	"github.com/humbornjo/mizu/mizuoai"
	"github.com/humbornjo/mizu/mizuotel"
	"google.golang.org/protobuf/encoding/protojson"

	"example.com/mizu/package/debug"
	"example.com/mizu/protogen"
)

const ServiceName = "example-app"

// The Config type is generated from config.cue — see
// cue_types_gen.go. local.yaml decodes into it through koanf's json
// tags, and mizucue validates the decoded value below.
func Initialize(paths ...string) {
	// Dependency Injection --------------------------------------------
	// koanf decodes into CUE-generated structs, whose tags are json.
	mizudi.DEFAULT_UNMARSHAL_TAG = "json"
	if err := mizudi.Initialize("config", paths...); err != nil {
		panic(err)
	}

	if err := mizudi.RevealConfig(os.Stdout); err != nil {
		if !errors.Is(err, mizudi.ErrNotInitialized) {
			panic(err)
		}
	}

	// The same #Config that generated the struct now guards the
	// loaded values.
	module := mizudi.MustRetrieve[mizucue.Module]()
	c := mizudi.Enchant[Config](nil)
	if err := module.MustExtract("config").Validate(c); err != nil {
		panic(err)
	}
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
			runtime.WithMarshalerOption("*", filekit.NewFileMarshaler(
				protojson.MarshalOptions{UseProtoNames: true},
				protojson.UnmarshalOptions{DiscardUnknown: true},
			)),
			runtime.WithMarshalerOption("multipart/form-data", filekit.NewFormMarshaler(
				protojson.MarshalOptions{UseProtoNames: true},
				protojson.UnmarshalOptions{DiscardUnknown: true},
			)),
		),
		// mizuconnect.WithCrpcVanguard(""),

		mizuconnect.WithCrpcHandlerOptions(
			connect.WithInterceptors(debug.NewInterceptor()),
		),
	)
	mizudi.Register(func() (*mizuconnect.Scope, error) { return scope, nil })

	// OPENAPI ---------------------------------------------------------
	// The connect-openapi document preloads the base; every CUE
	// package in the module contributes its component schemas on
	// top, keyed by the same canonical names reflection produces.
	options := []mizuoai.DocumentOption{
		mizuoai.WithDocumentRenderHTML(),
		mizuoai.WithDocumentBase(protogen.OPENAPI),
	}
	for importPath, doc := range module.MustOpenAPIs(nil) {
		options = append(options, mizuoai.WithDocumentPatch(importPath, doc))
	}
	if err := mizuoai.Initialize(server, "mizu_example", options...); err != nil {
		panic(err)
	}

	// Opentelemetry ---------------------------------------------------
	if err := mizuotel.Initialize(); err != nil {
		panic(err)
	}

	// Logging ---------------------------------------------------------
	logx.Initialize(nil, logx.WithLogLevel(c.Level))

	// Other Registrations ---------------------------------------------
	// e.g. Register Default Database using mizudi.Register and use
	// them across services.
	// ...
}
