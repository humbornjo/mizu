package config

import (
	"errors"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizulog"
	"github.com/humbornjo/mizu/mizuoai"
	"github.com/humbornjo/mizu/mizuotel"
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
	// Dependency Injection ---------------------------------------
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

	// Server -----------------------------------------------------
	server := mizu.NewServer(
		ServiceName,
		mizu.WithRevealRoutes(),
		mizu.WithProfilingHandlers(),
		mizu.WithReadinessDrainDelay(0*time.Second),
		// Force Protocol can useful when dev locally (Go use HTTP/1 by default when TLS is disabled)
		mizu.WithServerProtocols(mizu.PROTOCOLS_HTTP2_UNENCRYPTED),
	)
	mizudi.Register(func() (*mizu.Server, error) { return server, nil })

	// Connect RPC ------------------------------------------------
	scope := mizuconnect.NewScope(server,
		mizuconnect.WithGrpcHealth(),
		mizuconnect.WithGrpcReflect(),
		mizuconnect.WithCrpcValidate(),
		mizuconnect.WithCrpcVanguard("/"),
		mizuconnect.WithCrpcHandlerOptions(
			connect.WithInterceptors(debug.NewInterceptor()),
		),
	)
	mizudi.Register(func() (*mizuconnect.Scope, error) { return scope, nil })

	// OPENAPI ----------------------------------------------------
	if err := mizuoai.Initialize(server, "mizu_example",
		mizuoai.WithOaiDocumentation(),
		mizuoai.WithOaiPreLoad(protogen.OPENAPI)); err != nil {
		panic(err)
	}

	// Opentelemetry ----------------------------------------------
	if err := mizuotel.Initialize(); err != nil {
		panic(err)
	}

	// Logging ----------------------------------------------------
	mizulog.Initialize(nil, mizulog.WithLogLevel(c.Level))

	// Other Registrations ----------------------------------------
	// e.g. Register Default Database using mizudi.Register and use
	// them across services.
	// ...
}
