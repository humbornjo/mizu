package main

import (
	"context"
	"log/slog"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizumw/compressmw"
	"github.com/humbornjo/mizu/mizumw/recovermw"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"mizu.example/config"
	"mizu.example/service/filesvc"
	"mizu.example/service/greetsvc"
	"mizu.example/service/httpsvc"
	"mizu.example/service/namastesvc"
	"mizu.example/service/oaisvc"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
