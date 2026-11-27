package logx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/humbornjo/mizu/x/logx"
	"github.com/stretchr/testify/require"
)

func TestLogx_LogContextSiblingIsolation(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(logx.New(slog.NewJSONHandler(&buf, nil)))

	base := make([]slog.Attr, 1, 2)
	base[0] = slog.String("scope", "base")

	baseCtx := logx.LogContext(context.Background(), base...)
	aliceCtx := logx.LogContext(baseCtx, slog.String("user_id", "alice"))
	bobCtx := logx.LogContext(baseCtx, slog.String("user_id", "bob"))

	// Mutating the caller's slice after LogContext must not affect derived contexts.
	base[0] = slog.String("scope", "mutated")

	logger.InfoContext(aliceCtx, "alice-event")
	var aliceRecord map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &aliceRecord))
	require.Equal(t, "base", aliceRecord["scope"])
	require.Equal(t, "alice", aliceRecord["user_id"])

	buf.Reset()
	logger.InfoContext(bobCtx, "bob-event")
	var bobRecord map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &bobRecord))
	require.Equal(t, "base", bobRecord["scope"])
	require.Equal(t, "bob", bobRecord["user_id"])
}

func TestLogx_HandlerWithAttrsAndWithGroupSiblingIsolation(t *testing.T) {
	var aliceBuf, bobBuf bytes.Buffer

	baseHandler := logx.New(
		slog.NewJSONHandler(&aliceBuf, nil),
		logx.WithAttributes([]slog.Attr{slog.String("service", "auth")}),
	)

	aliceHandler := baseHandler.WithAttrs([]slog.Attr{slog.String("user_id", "alice")})
	bobHandler := baseHandler.WithAttrs([]slog.Attr{slog.String("user_id", "bob")})

	slog.New(aliceHandler).Info("alice-msg")
	var aliceRecord map[string]any
	require.NoError(t, json.Unmarshal(aliceBuf.Bytes(), &aliceRecord))
	require.Equal(t, "auth", aliceRecord["service"])
	require.Equal(t, "alice", aliceRecord["user_id"])

	aliceBuf.Reset()
	slog.New(bobHandler).Info("bob-msg")
	var bobRecord map[string]any
	require.NoError(t, json.Unmarshal(aliceBuf.Bytes(), &bobRecord))
	require.Equal(t, "auth", bobRecord["service"])
	require.Equal(t, "bob", bobRecord["user_id"])

	// Derive grouped siblings from a shared base handler.
	groupBase := logx.New(
		slog.NewJSONHandler(&bobBuf, nil),
		logx.WithAttributes([]slog.Attr{slog.String("service", "auth")}),
	)
	groupedAlice := groupBase.WithGroup("req").WithAttrs([]slog.Attr{slog.String("user_id", "alice")})
	groupedBob := groupBase.WithGroup("req").WithAttrs([]slog.Attr{slog.String("user_id", "bob")})

	slog.New(groupedAlice).Info("grouped-alice")
	var groupedRecord map[string]any
	require.NoError(t, json.Unmarshal(bobBuf.Bytes(), &groupedRecord))
	reqGroup, ok := groupedRecord["req"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "auth", reqGroup["service"])
	require.Equal(t, "alice", reqGroup["user_id"])

	bobBuf.Reset()
	slog.New(groupedBob).Info("grouped-bob")
	groupedRecord = nil
	require.NoError(t, json.Unmarshal(bobBuf.Bytes(), &groupedRecord))
	reqGroup, ok = groupedRecord["req"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "auth", reqGroup["service"])
	require.Equal(t, "bob", reqGroup["user_id"])
}

func TestLogx_ConcurrentSiblingDerivation(t *testing.T) {
	base := make([]slog.Attr, 1, 2)
	base[0] = slog.String("scope", "base")
	baseCtx := logx.LogContext(context.Background(), base...)

	var mu sync.Mutex
	var buf bytes.Buffer
	syncWriter := &syncBuffer{mu: &mu, buf: &buf}
	baseHandler := logx.New(
		slog.NewJSONHandler(syncWriter, nil),
		logx.WithAttributes([]slog.Attr{slog.String("service", "auth")}),
	)

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		userId := "alice"
		if i%2 == 1 {
			userId = "bob"
		}
		go func(uid string) {
			defer wg.Done()
			ctx := logx.LogContext(baseCtx, slog.String("user_id", uid))
			h := baseHandler.WithGroup("req").WithAttrs([]slog.Attr{slog.String("principal", uid)})
			r := slog.NewRecord(time.Time{}, slog.LevelInfo, "concurrent", 0)
			require.NoError(t, h.Handle(ctx, r))
		}(userId)
	}
	wg.Wait()

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.Len(t, lines, workers)
	for _, line := range lines {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		reqGroup, ok := rec["req"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "base", reqGroup["scope"])
		require.Equal(t, "auth", reqGroup["service"])
		require.Equal(t, reqGroup["user_id"], reqGroup["principal"])
	}
}

type syncBuffer struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}
