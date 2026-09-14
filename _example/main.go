package main

import (
	"context"
	"embed"
	"log/slog"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizucue"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizumw/compressmw"
	"github.com/humbornjo/mizu/mizumw/recovermw"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"example.com/mizu/config"
	"example.com/mizu/service/filesvc"
	"example.com/mizu/service/greetsvc"
	"example.com/mizu/service/httpsvc"
	"example.com/mizu/service/namastesvc"
	"example.com/mizu/service/oaisvc"
)

// REPO_FS carries the whole CUE tree — cue.mod, the app config
// contract, and every service's schema — into the binary, so the
// mizucue module loads identically no matter where it runs.
//
//go:embed all:config all:service all:cue.mod
var REPO_FS embed.FS

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// One CUE module is the single source of truth: Go types are
	// generated from it (cue exp gengotypes), config and requests
	// are validated against it, and its OpenAPI documents assemble
	// into the served spec. Registered before config.Initialize —
	// the config contract itself is validated through it.
	module, err := mizucue.LoadModule(REPO_FS)
	if err != nil {
		panic(err)
	}
	mizudi.Register(func() (mizucue.Module, error) { return module, nil })

	config.Initialize()

	// Below two entities are registered in config.Initialize
	srv := mizudi.MustRetrieve[*mizu.Server]()
	global := mizudi.MustRetrieve[*config.Config]()

	// HTTP global middleware ------------------------------------------

	// Apply middleware to all handlers
	srv.Use(recovermw.New())
	srv.Use(otelhttp.NewMiddleware(config.ServiceName))
	srv.Use(compressmw.New(compressmw.WithContentTypes("text/*")))

	// Initialize services ---------------------------------------------
	oaisvc.Initialize(global)
	httpsvc.Initialize(global)
	filesvc.Initialize(global)
	greetsvc.Initialize(global)
	namastesvc.Initialize(global)

	errChan := make(chan error, 1)
	go func() {
		defer cancel()
		defer close(errChan)
		if err := srv.ServeContext(ctx, global.Port); err != nil {
			errChan <- err
		}
	}()

	<-ctx.Done()

	if err := <-errChan; err != nil {
		slog.ErrorContext(ctx, config.ServiceName+" exit unexpectedly", "error", err)
	}
}
