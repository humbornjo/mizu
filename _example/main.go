package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuconnect"
	"github.com/humbornjo/mizu/mizuoai"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"mizu.example/package/debug"
	"mizu.example/protogen/barapp/file/v1/filev1connect"
	"mizu.example/protogen/barapp/greet/v1/greetv1connect"
	"mizu.example/protogen/fooapp/namaste/v1/namastev1connect"
	"mizu.example/service/filesvc"
	"mizu.example/service/greetsvc"
	"mizu.example/service/namastesvc"
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

type InputOaiOrder struct {
	Path struct {
		UserId string `path:"user_id" desc:"user id" required:"true"`
	} `mizu:"path"`
	Query struct {
		UnixTime int64 `query:"timestamp" desc:"unix timestamp"`
	} `mizu:"query"`
	Header struct {
		Region string `header:"X-Region" desc:"where the order is from"`
	} `mizu:"header"`
	Body struct {
		Id      string `json:"id" desc:"order id" required:"true"`
		Amount  int    `json:"amount" desc:"order amount" required:"true"`
		Comment string `json:"comment" desc:"order comment"`
	} `mizu:"body"`
}

type OutputOaiOrder struct {
	Amount int `json:"amount" desc:"order amount can be processed"`
}

func HandleOaiOrder(tx mizuoai.Tx[OutputOaiOrder], rx mizuoai.Rx[InputOaiOrder]) {
	input := rx.MizuRead()

	userId := input.Path.UserId
	region := input.Header.Region
	timestamp := time.Unix(input.Query.UnixTime, 0)

	id := input.Body.Id
	amount := input.Body.Amount
	comment := input.Body.Comment

	slog.Info(
		"Received order",
		"user_id", userId, "region", region, "timestamp", timestamp,
		"id", id, "amount", amount, "comment", comment,
	)

	_ = tx.MizuWrite(&OutputOaiOrder{Amount: 1})
}

func MiddlewareLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("Received request", "method", r.Method, "path", r.URL.Path)
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
		mizu.WithRevealRoutes(),
		mizu.WithProfilingHandlers(),
		mizu.WithReadinessDrainDelay(0*time.Second),
		// Force Protocol can useful when dev locally (Go use HTTP/1 by default when TLS is disabled)
		mizu.WithServerProtocols(mizu.PROTOCOLS_HTTP2_UNENCRYPTED),
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
		mizuconnect.WithGrpcHealth(),
		mizuconnect.WithGrpcReflect(),
		mizuconnect.WithCrpcValidate(),
		mizuconnect.WithCrpcVanguard("/"),
		mizuconnect.WithCrpcHandlerOptions(
			connect.WithInterceptors(debug.NewInterceptor()),
		),
	)
	fileSvc := filesvc.NewService()
	crpcScope.Register(fileSvc, filev1connect.NewFileServiceHandler)
	greetSvc := greetsvc.NewService()
	crpcScope.Register(greetSvc, greetv1connect.NewGreetServiceHandler)
	namasteSvc := namastesvc.NewService()
	crpcScope.Register(namasteSvc, namastev1connect.NewNamasteServiceHandler)

	// Create Openapi register instance
	oai := mizuoai.NewOai(
		server, "mizu_example",
		mizuoai.WithOaiDocumentation(),
	)
	mizuoai.Get(oai, "/oai/scrape", HandleOaiScrape,
		mizuoai.WithOperationTags("scrape"),
		mizuoai.WithOperationSummary("mizu_example http scrape"),
		mizuoai.WithOperationDescription("nobody knows scrape more than I do"),
	)
	mizuoai.Post(oai, "/oai/user/{user_id}/order", HandleOaiOrder,
		mizuoai.WithOperationTags("bisiness", "order"),
		mizuoai.WithOperationSummary("mizu_example order service"),
		mizuoai.WithOperationDescription("nobody knows order more than I do"),
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
