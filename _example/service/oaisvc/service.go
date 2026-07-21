package oaisvc

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/humbornjo/mizu/mizuoai"
	"mizu.example/package/netkit"
)

type InputOaiScrape struct {
	Header struct {
		Key string `header:"key" desc:"a magic key"`
	} `mizu:"header"`
}

type OutputOaiScrape = string

func HandleOaiPackage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="mizu-example.tar.gz"`)
	_, _ = w.Write([]byte{0x1f, 0x8b, 0x08})
}

func HandleOaiEvents(w http.ResponseWriter, r *http.Request) {
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

func HandleOaiScrape(tx mizuoai.Tx[OutputOaiScrape], rx mizuoai.Rx[InputOaiScrape]) {
	input, err := rx.MizuRead()
	if err != nil {
		slog.Error("failed to read input", "error", err)
		_ = netkit.WriteString(tx, err.Error(), http.StatusBadRequest)
		return
	}

	_ = netkit.WriteString(tx, "Hello, "+input.Header.Key, http.StatusOK)
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
	input, err := rx.MizuRead()
	if err != nil {
		slog.Error("failed to read input", "error", err)
		_ = netkit.WriteString(tx, err.Error(), http.StatusBadRequest)
		return
	}

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
