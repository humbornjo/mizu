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
		mizu.WithRevealRoutesOnStartup(),
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
		mizuconnect.WithGrpcHealth(),
		mizuconnect.WithGrpcReflect(),
		mizuconnect.WithValidate(),
		mizuconnect.WithVanguard("/", nil, nil),
	)
	crpcService := svc.NewService()
	crpcScope.Register(crpcService, greetv1connect.NewGreetServiceHandler)

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
	mizuoai.Post(oai, "/oai/order", HandleOaiOrder,
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
