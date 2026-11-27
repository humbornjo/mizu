package recovermw_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/humbornjo/mizu/mizumw/recovermw"
)

const _CHILD_ENV = "_MIZU_RECOVERMW_STDERR_CHILD"

type recordingWriteCloser struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed bool
}

func (r *recordingWriteCloser) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *recordingWriteCloser) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func (r *recordingWriteCloser) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func TestRecoverMw_SharedStderrNotClosed(t *testing.T) {
	if os.Getenv(_CHILD_ENV) == "1" {
		runStderrChild(t)
		return
	}

	dir := t.TempDir()
	stderrPath := filepath.Join(dir, "stderr.log")
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr file: %v", err)
	}
	defer stderrFile.Close()

	stdoutPath := filepath.Join(dir, "stdout.log")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatalf("create stdout file: %v", err)
	}
	defer stdoutFile.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestRecoverMw_SharedStderrNotClosed$")
	cmd.Env = append(os.Environ(), _CHILD_ENV+"=1")
	cmd.Stderr = stderrFile
	cmd.Stdout = stdoutFile

	if err := cmd.Run(); err != nil {
		data, _ := os.ReadFile(stderrPath)
		t.Fatalf("child failed: %v\nstderr:\n%s", err, string(data))
	}

	stderrBytes, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read stderr file: %v", err)
	}
	stderrLog := string(stderrBytes)
	for _, want := range []string{"[PANIC] boom-1", "[PANIC] boom-2", "probe-1", "probe-2"} {
		if !strings.Contains(stderrLog, want) {
			t.Fatalf("stderr log missing %q:\n%s", want, stderrLog)
		}
	}

	stdoutBytes, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("read stdout file: %v", err)
	}
	stdoutLog := string(stdoutBytes)
	for _, want := range []string{"[PANIC] stdout-boom-1", "[PANIC] stdout-boom-2", "stdout-probe"} {
		if !strings.Contains(stdoutLog, want) {
			t.Fatalf("stdout log missing %q:\n%s", want, stdoutLog)
		}
	}
}

func runStderrChild(t *testing.T) {
	t.Helper()

	mw := recovermw.New()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/panic-1":
			panic("boom-1")
		case "/panic-2":
			panic("boom-2")
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/panic-1", nil))
	if rec1.Code != http.StatusInternalServerError {
		t.Fatalf("panic-1 status = %d, want 500", rec1.Code)
	}
	if _, err := os.Stderr.Write([]byte("probe-1\n")); err != nil {
		t.Fatalf("os.Stderr.Write after panic-1 failed: %v", err)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/panic-2", nil))
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("panic-2 status = %d, want 500", rec2.Code)
	}
	if _, err := os.Stderr.Write([]byte("probe-2\n")); err != nil {
		t.Fatalf("os.Stderr.Write after panic-2 failed: %v", err)
	}

	recOK := httptest.NewRecorder()
	handler.ServeHTTP(recOK, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if recOK.Code != http.StatusOK || recOK.Body.String() != "ok" {
		t.Fatalf("ok response = (%d, %q), want (200, \"ok\")", recOK.Code, recOK.Body.String())
	}

	stdoutMW := recovermw.New(recovermw.WithWriteCloser(os.Stdout))
	stdoutHandler := stdoutMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("stdout-" + strings.TrimPrefix(r.URL.Path, "/"))
	}))

	for _, path := range []string{"/boom-1", "/boom-2"} {
		rec := httptest.NewRecorder()
		stdoutHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s status = %d, want 500", path, rec.Code)
		}
	}
	if _, err := os.Stdout.Write([]byte("stdout-probe\n")); err != nil {
		t.Fatalf("os.Stdout.Write after panics failed: %v", err)
	}
}

func TestRecoverMw_SharedWriteCloserNotClosed(t *testing.T) {
	wc := &recordingWriteCloser{}
	mw := recovermw.New(recovermw.WithWriteCloser(wc))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		panic("panic-" + strings.TrimPrefix(r.URL.Path, "/"))
	}))

	var wg sync.WaitGroup
	for _, path := range []string{"/1", "/2", "/3"} {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("%s status = %d, want 500", p, rec.Code)
			}
		}(path)
	}
	wg.Wait()

	if wc.closed {
		t.Fatalf("shared WriteCloser was closed on panic")
	}

	out := wc.String()
	for _, want := range []string{"[PANIC] panic-1", "[PANIC] panic-2", "[PANIC] panic-3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("writer output missing %q:\n%s", want, out)
		}
	}

	recOK := httptest.NewRecorder()
	handler.ServeHTTP(recOK, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if recOK.Code != http.StatusOK || recOK.Body.String() != "ok" {
		t.Fatalf("ok response = (%d, %q), want (200, \"ok\")", recOK.Code, recOK.Body.String())
	}
}

func TestRecoverMw_ErrAbortHandlerPropagates(t *testing.T) {
	wc := &recordingWriteCloser{}
	mw := recovermw.New(recovermw.WithWriteCloser(wc))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		rcv := recover()
		if rcv != http.ErrAbortHandler {
			t.Fatalf("recovered = %v, want http.ErrAbortHandler", rcv)
		}
		if wc.closed {
			t.Fatalf("writer was closed on ErrAbortHandler")
		}
		if wc.String() != "" {
			t.Fatalf("unexpected log output on ErrAbortHandler: %q", wc.String())
		}
	}()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
