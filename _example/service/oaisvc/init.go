package oaisvc

import (
	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizudi"
	"github.com/humbornjo/mizu/mizuoai"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
	"mizu.example/config"
)

func Initialize(_ *config.Config) {
	srv := mizudi.MustRetrieve[*mizu.Server]()
	registerRoutes(srv)
}

func registerRoutes(srv *mizu.Server) {
	g := srv.Group("/oai")
	mizuoai.Get(g, "/scrape", HandleOaiScrape,
		mizuoai.WithOperationTags("scrape"),
		mizuoai.WithOperationSummary("mizu_example http scrape"),
		mizuoai.WithOperationDescription("nobody knows scrape more than I do"),
	)
	mizuoai.GetRaw(g, "/events", HandleOaiEvents,
		mizuoai.WithOperation(&v3.Operation{
			OperationId: "streamEvents",
			Tags:        []string{"events"},
			Summary:     "Stream server-sent events",
			Description: "Sends three text events and flushes each event immediately.",
			Responses: &v3.Responses{Codes: orderedmap.ToOrderedMap(map[string]*v3.Response{
				"200": {
					Description: "Event stream",
					Content: orderedmap.ToOrderedMap(map[string]*v3.MediaType{
						"text/event-stream": {
							ItemSchema: base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}),
							Example: &yaml.Node{
								Kind:  yaml.ScalarNode,
								Tag:   "!!str",
								Style: yaml.LiteralStyle,
								Value: "data: connected\n\ndata: working\n\ndata: complete\n\n",
							},
						},
					}),
				},
			})},
		}),
	)
	mizuoai.GetRaw(g, "/package", HandleOaiPackage,
		mizuoai.WithOpenApiOperation(_CUE_OPENAPI_DOCUMENT, "downloadPackage"),
		mizuoai.WithOperationTags("package"),
		mizuoai.WithOperationSummary("Download a CUE-documented package"),
		mizuoai.WithOperationDescription("Streams a compressed example package using a CUE-owned transport contract."),
	)

	guser := g.Group("/user")
	mizuoai.Post(guser, "/{user_id}/order", HandleOaiOrder,
		mizuoai.WithOperationTags("bisiness", "order"),
		mizuoai.WithOperationSummary("mizu_example order service"),
		mizuoai.WithOperationDescription("nobody knows order more than I do"),
	)
}
