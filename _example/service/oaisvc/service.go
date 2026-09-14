package oaisvc

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizucue"
	"github.com/humbornjo/mizu/mizuoai"
)

// Service carries the package's CUE schema: every typed handler
// validates its decoded input against the same definition that
// generated the Go type and the OpenAPI component.
type Service struct {
	schema mizucue.Schema
}

// HandleScrape echoes the magic header back.
func (s *Service) HandleScrape(_ http.ResponseWriter, rx mizuoai.Rx[ScrapeRequest]) (ScrapeResponse, error) {
	input, err := rx.Xread()
	if err != nil {
		return ScrapeResponse{}, mizu.NewError(http.StatusBadRequest, err)
	}
	if err := s.schema.Validate(input); err != nil {
		return ScrapeResponse{}, mizu.NewError(http.StatusBadRequest, err)
	}
	return ScrapeResponse{Message: "Hello, " + input.Header.Key}, nil
}

// HandleCreateOrder logs one order per request location.
func (s *Service) HandleCreateOrder(_ http.ResponseWriter, rx mizuoai.Rx[CreateOrderRequest]) (CreateOrderResponse, error) {
	input, err := rx.Xread()
	if err != nil {
		return CreateOrderResponse{}, mizu.NewError(http.StatusBadRequest, err)
	}
	if err := s.schema.Validate(input); err != nil {
		return CreateOrderResponse{}, mizu.NewError(http.StatusBadRequest, err)
	}

	slog.Info(
		"received order",
		"user_id", input.Path.UserId, "region", input.Header.Region,
		"timestamp", time.Unix(input.Query.UnixTime, 0),
		"id", input.Body.Id, "amount", input.Body.Amount, "comment", input.Body.Comment,
	)
	return CreateOrderResponse{Amount: 1}, nil
}

// HandlePackage streams a fake gzip header; the operation contract
// lives in CUE (#DownloadPackageOperation).
func (s *Service) HandlePackage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="mizu-example.tar.gz"`)
	_, _ = w.Write([]byte{0x1f, 0x8b, 0x08})
}

// HandleEvents sends three server-sent events and flushes each.
func (s *Service) HandleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for index, message := range []string{"connected", "working", "complete"} {
		if index > 0 {
			select {
			case <-ticker.C:
			case <-r.Context().Done():
				return
			}
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", message); err != nil {
			return
		}
		flusher.Flush()
	}
}
