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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"mizu.example/protogen/greet/v1/greetv1connect"
	"mizu.example/svc"
)

func MiddlewareLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received request:", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func MiddlewareOtelHttp() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware("example-app")
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serviceName := "example-app"
	server := mizu.NewServer(
		serviceName,
		mizu.WithDisplayRoutesOnStartup(),
	)

	// Apply middleware to all routes at outermost
	server.Use(MiddlewareOtelHttp())

	// Chain middleware to a single handler
	server.Use(MiddlewareLogging).Get(
		"/scrape",
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"message": "Hello, world!"}`))
		},
	)

	// Create a connect register scope
	connectScope := mizuconnect.NewScope(
		server,
		mizuconnect.WithHealth(),
		mizuconnect.WithReflect(),
		mizuconnect.WithValidate(),
		mizuconnect.WithVanguard("/", nil, nil),
	)

	// Register the service
	connectService := svc.NewService()
	connectScope.Register(connectService, greetv1connect.NewGreetServiceHandler)

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
