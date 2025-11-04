package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizumw/compressmw"

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

	srv := mizudi.MustRetrieve[*mizu.Server]()
	cfg := mizudi.MustRetrieve[*config.Config]()

	// HTTP global middleware -------------------------------------

	// Apply middleware to all handlers
	srv.Use(otelhttp.NewMiddleware(config.ServiceName))
	srv.Use(compressmw.New(compressmw.EncodingGzip{}))

	// Initialize services ----------------------------------------
	oaisvc.Initialize()
	httpsvc.Initialize()
	filesvc.Initialize()
	greetsvc.Initialize()
	namastesvc.Initialize()

	errChan := make(chan error, 1)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	go func() {
		defer cancel()
		defer close(errChan)
		if err := srv.ServeContext(ctx, cfg.Port); err != nil {
			errChan <- err
		}
	}()

	<-ctx.Done()
	stop()

	if err := <-errChan; err != nil {
		slog.ErrorContext(ctx, config.ServiceName+" exit unexpectedly", "error", err)
	}
}
