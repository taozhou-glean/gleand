package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

const (
	ansiYellow = "\033[33m"
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
)

type debugHandler struct {
	w     io.Writer
	mu    sync.Mutex
	attrs []slog.Attr
	group string
}

func NewDebugLogHandler(w io.Writer) *debugHandler {
	return &debugHandler{w: w}
}

func (h *debugHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelDebug
}

func (h *debugHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ts := r.Time.Format(time.TimeOnly)
	level := r.Level.String()

	line := fmt.Sprintf("%s%s %s%-5s%s %s%s",
		ansiDim, ts, ansiYellow, level, ansiReset, r.Message, ansiReset)

	r.Attrs(func(a slog.Attr) bool {
		line += fmt.Sprintf(" %s%s%s=%v", ansiYellow, a.Key, ansiReset, a.Value)
		return true
	})

	for _, a := range h.attrs {
		line += fmt.Sprintf(" %s%s%s=%v", ansiYellow, a.Key, ansiReset, a.Value)
	}

	_, err := fmt.Fprintln(h.w, line)
	return err
}

func (h *debugHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &debugHandler{
		w:     h.w,
		attrs: append(h.attrs, attrs...),
		group: h.group,
	}
}

func (h *debugHandler) WithGroup(name string) slog.Handler {
	return &debugHandler{
		w:     h.w,
		attrs: h.attrs,
		group: name,
	}
}
