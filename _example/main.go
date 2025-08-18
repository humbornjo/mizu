package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizuoai"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"mizu.example/protogen/app_foo/greet/v1/greetv1connect"
	"mizu.example/svc"
)

type InputOaiScrape struct {
	Header struct {
		Key string `header:"key" desc:"a magic key"`
	} `mizu:"header"`
}

type OutputOaiScrape = string

func HandleOaiScrape(tx mizuoai.Tx[OutputOaiScrape], rx mizuoai.Rx[InputOaiScrape]) {
	input := rx.MizuRead()
	_, _ = tx.Write([]byte("Hello, " + input.Header.Key))
}

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
		mizu.WithDisplayRoutesOnStartup(),
		mizu.WithProfilingHandlers(),
		mizu.WithReadinessDrainDelay(0*time.Second),
		mizu.WithServerProtocols(mizu.PROTOCOLS_HTTP2),
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
	crpcScope := mizuconnect.NewScope(server,
		mizuconnect.WithHealth(),
		mizuconnect.WithReflect(),
		mizuconnect.WithValidate(),
		mizuconnect.WithVanguard("/", nil, nil),
	)
	crpcService := svc.NewService()
	crpcScope.Register(crpcService, greetv1connect.NewGreetServiceHandler)

	// Create Openapi register instance
	oai := mizuoai.NewOai(
		server, "/",
		mizuoai.WithOaiDocumentation(),
		mizuoai.WithOaiTitle("mizu example api"),
	)
	mizuoai.Get(oai, "/oai/scrape", HandleOaiScrape,
		mizuoai.WithOperationTags("tag_1", "tag_2"),
		mizuoai.WithOperationSummary("mizu_example http scrape"),
		mizuoai.WithOperationDescription("nobody knows scrape more than me"),
	)

	errChan := make(chan error, 1)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	go func() {
		defer cancel()
		defer close(errChan)
		if err := server.ServeContext(ctx, ":18080"); err != nil {
			errChan <- err
		}
	}()

	<-ctx.Done()
	stop()
	if err := <-errChan; err != nil {
		slog.ErrorContext(ctx, serviceName+" exit unexpectedly", "error", err)
	}
}
