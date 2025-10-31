package recovermw

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

func New() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bad := true // Incase some dickhead use panic("")
			defer func() {
				if !bad {
					return
				}

				rcv := recover()
				if rcv == http.ErrAbortHandler {
					// we don't recover http.ErrAbortHandler so the response
					// to the client is aborted, this should not be logged
					panic(rcv)
				}

				debugStack := debug.Stack()
				out, err := parse(debugStack, rcv)
				if err == nil {
					os.Stderr.Write(out)
				} else {
					os.Stderr.Write(debugStack)
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
	fmt.Fprintf(buf, "\n panic: %v\n\n", rcv)

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
		fmt.Fprintf(buf, "%s", l)
	}
	return buf.Bytes(), nil
}
