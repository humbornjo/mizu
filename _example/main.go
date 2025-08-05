package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizuoai"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"mizu.example/protogen/app_foo/greet/v1/greetv1connect"
	"mizu.example/svc"
)

type InputOaiScrape struct {
	Name string `mizu:"body"`
}

type OutputOaiScrape = string

func MiddlewareLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received request:", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

var MiddlewareOtelHttp = otelhttp.NewMiddleware("example-app")

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serviceName := "example-app"
	server := mizu.NewServer(
		serviceName,
		mizu.WithProfilingHandlers(),
		mizu.WithDisplayRoutesOnStartup(),
	)

	// Apply middleware to all handlers
	server.Use(MiddlewareOtelHttp)

	// Chain middleware on one handler only
	server.Use(MiddlewareLogging).Get(
		"/scrape",
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"message": "Hello, world!"}`))
		},
	)

	// Create Connect RPC register scope
	crpcScope := mizuconnect.NewScope(
		server,
		mizuconnect.WithHealth(),
		mizuconnect.WithReflect(),
		mizuconnect.WithValidate(),
		mizuconnect.WithVanguard("/", nil, nil),
	)
	crpcService := svc.NewService()
	crpcScope.Register(crpcService, greetv1connect.NewGreetServiceHandler)

	// Create Openapi register scope
	oaiScope := mizuoai.NewScope(
		server, "/",
		mizuoai.WithOaiDocumentation(),
	)
	mizuoai.Get(oaiScope, "/oai/scrape", func(tx mizuoai.Tx[OutputOaiScrape], rx mizuoai.Rx[InputOaiScrape]) {
		input := rx.Read()
		ret := "Hello, " + input.Name
		_ = tx.Write(&ret)
	})

	errChan := make(chan error, 1)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	go func() {
		defer cancel()
		defer close(errChan)
		if err := server.ServeContext(ctx, ":8080"); err != nil {
			errChan <- err
		}
	}()

	<-ctx.Done()
	stop()
	if err := <-errChan; err != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s exit unexpectedly", serviceName), slog.String("error", err.Error()))
	}
}
