package oaisvc

import (
	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizuoai"
	"mizu.example/config"
)

func Initialize(_ *config.Config) {
	srv := mizudi.MustRetrieve[*mizu.Server]()

	g := srv.Group("/oai")
	mizuoai.Get(g, "/scrape", HandleOaiScrape,
		mizuoai.WithOperationTags("scrape"),
		mizuoai.WithOperationSummary("mizu_example http scrape"),
		mizuoai.WithOperationDescription("nobody knows scrape more than I do"),
	)

	guser := g.Group("/user")
	mizuoai.Post(guser, "/{user_id}/order", HandleOaiOrder,
		mizuoai.WithOperationTags("bisiness", "order"),
		mizuoai.WithOperationSummary("mizu_example order service"),
		mizuoai.WithOperationDescription("nobody knows order more than I do"),
	)
}
