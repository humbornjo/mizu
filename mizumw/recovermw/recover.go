package recovermw

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

var _DEFAULT_CONFIG = config{
	tx:       os.Stderr,
	maxBytes: 0,
}

type config struct {
	tx       io.WriteCloser
	maxBytes int
}

type Option func(*config)

func WithMaxBytes(maxBytes int) Option {
	return func(c *config) {
		c.maxBytes = maxBytes
	}
}

func WithWriteCloser(tx io.WriteCloser) Option {
	return func(c *config) {
		c.tx = tx
	}
}

func New(opts ...Option) func(http.Handler) http.Handler {
	config := _DEFAULT_CONFIG
	for _, opt := range opts {
		opt(&config)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bad := true // incase some dickhead use panic("")
			defer func() {
				if !bad {
					return
				}

				rcv := recover()
				// we don't recover http.ErrAbortHandler so the response to the
				// client is aborted, this should not be logged
				if rcv == http.ErrAbortHandler {
					panic(rcv)
				}

				defer config.tx.Close() // nolint: errcheck
				debugStack := debug.Stack()
				if config.maxBytes > 0 {
					debugStack = debugStack[:config.maxBytes]
				}
				out, err := parse(debugStack, rcv)
				if err == nil {
					_, _ = config.tx.Write(out)
				} else {
					_, _ = config.tx.Write(debugStack)
				}

				if r.Header.Get("Connection") != "Upgrade" {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
			bad = false
		})
	}
}

func parse(debugStack []byte, rcv any) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, "[PANIC] %v\n\n", rcv)

	// process debug stack info
	stack := strings.Split(string(debugStack), "\n")
	lines := []string{}

	// locate panic line, as we may have nested panics
	for i := len(stack) - 1; i > 0; i-- {
		lines = append(lines, stack[i])
		if strings.HasPrefix(stack[i], "panic(") {
			lines = lines[0 : len(lines)-2] // remove boilerplate
			break
		}
	}

	// reverse
	for i := len(lines)/2 - 1; i >= 0; i-- {
		opp := len(lines) - 1 - i
		lines[i], lines[opp] = lines[opp], lines[i]
	}

	for _, l := range lines {
		fmt.Fprintf(buf, "%s\n", l)
	}
	return buf.Bytes(), nil
}
