package httpsvc

import (
	"net/http"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizudi"

	"mizu.example/config"
)

func Initialize(_ *config.Config) {
	srv := mizudi.MustRetrieve[*mizu.Server]()

	srv.Use(MiddlewareLogging).Get("/scrape", // Chain middleware on one handler only
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"message": "Hello, world!"}`))
		},
	)
}
