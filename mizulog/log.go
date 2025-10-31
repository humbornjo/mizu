package mizulog

import (
	"context"
	"log/slog"
	"os"
)

type ctxkey int

const _CTXKEY ctxkey = iota

var _DEFAULT_LOG_LEVEL = slog.LevelInfo

// Option configures the mizulog handler.
type Option func(*config)

type config func(*handler) *handler

// Initialize sets the default slog logger with a mizulog handler.
// If h is nil, it uses the current default handler.
func Initialize(h slog.Handler, opts ...Option) {
	slog.SetDefault(slog.New(New(h, opts...)))
}

// New creates a new mizulog handler that wraps the provided
// slog.Handler. If h is nil, it uses the current default handler.
func New(h slog.Handler, opts ...Option) *handler {
	if h == nil {
		h = slog.NewTextHandler(os.Stdout, nil)
	}

	config := new(config)
	*config = func(h *handler) *handler {
		h.level = _DEFAULT_LOG_LEVEL
		return h
	}

	for _, opt := range opts {
		opt(config)
	}
	return (*config)(&handler{Handler: h})
}

// InjectContextAttrs adds slog attributes to the context that
// will be automatically included in log records when using
// context-aware logging functions like slog.InfoContext,
// slog.ErrorContext, slog.DebugContext, etc.
//
// Example:
//
//	ctx = mizulog.InjectContextAttrs(ctx, slog.Int("id", 123))
//	slog.InfoContext(ctx, "user action") // will include id=123
func InjectContextAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if ctx == nil {
		return ctx
	}

	value := ctx.Value(_CTXKEY)
	if value == nil {
		return context.WithValue(ctx, _CTXKEY, attrs)
	}
	ctxAttrs, _ := value.([]slog.Attr)
	return context.WithValue(ctx, _CTXKEY, append(ctxAttrs, attrs...))
}

type level interface {
	int | string
}

// WithLogLevel sets the minimum log level for the handler.
func WithLogLevel[T level](level T) Option {
	l := new(slog.Level)
	switch data := any(level).(type) {
	case int:
		*l = slog.Level(data)
	case string:
		if err := l.UnmarshalText([]byte(data)); err != nil {
			panic(err)
		}
	}
	return func(m *config) {
		old := *m
		new := func(h *handler) *handler {
			h = old(h)
			h.level = *l
			return h
		}
		*m = new
	}
}

// WithAttributes adds default attributes that will be included
// in all log records.
func WithAttributes(attrs []slog.Attr) Option {
	return func(m *config) {
		old := *m
		new := func(h *handler) *handler {
			h = old(h)
			h.attrs = append(h.attrs, attrs...)
			return h
		}
		*m = new
	}
}

type handler struct {
	slog.Handler
	level slog.Level
	attrs []slog.Attr
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(h.attrs...)
	value := ctx.Value(_CTXKEY)
	if value == nil {
		return h.Handler.Handle(ctx, r)
	}

	attrs, _ := value.([]slog.Attr)
	r.AddAttrs(attrs...)
	return h.Handler.Handle(ctx, r)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{Handler: h.Handler, level: h.level, attrs: append(h.attrs, attrs...)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{Handler: h.Handler.WithGroup(name), level: h.level, attrs: h.attrs}
}
