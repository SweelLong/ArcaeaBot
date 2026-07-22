package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	colorReset  = "\033[0m"
	colorGray   = "\033[90m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
)

type ColorHandler struct {
	w      io.Writer
	level  slog.Leveler
	mu     *sync.Mutex
	attrs  []slog.Attr
	groups []string
}

func Setup() {
	slog.SetDefault(slog.New(NewColorHandler(os.Stderr, slog.LevelInfo)))
}

func NewColorHandler(w io.Writer, level slog.Leveler) *ColorHandler {
	return &ColorHandler{w: w, level: level, mu: &sync.Mutex{}}
}

func (h *ColorHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *ColorHandler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	buf.WriteString(t.Format("2006/01/02 15:04:05"))
	buf.WriteByte(' ')
	buf.WriteString(colorForLevel(r.Level))
	buf.WriteString(levelText(r.Level))
	buf.WriteString(colorReset)
	buf.WriteByte(' ')
	buf.WriteString(r.Message)

	for _, attr := range h.attrs {
		h.appendAttr(&buf, attr)
	}
	r.Attrs(func(attr slog.Attr) bool {
		h.appendAttr(&buf, attr)
		return true
	})
	buf.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf.Bytes())
	return err
}

func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *ColorHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.groups = append(append([]string{}, h.groups...), name)
	return &next
}

func (h *ColorHandler) appendAttr(buf *bytes.Buffer, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		for _, child := range attr.Value.Group() {
			child.Key = attr.Key + "." + child.Key
			h.appendAttr(buf, child)
		}
		return
	}
	key := attr.Key
	if len(h.groups) > 0 {
		key = strings.Join(append(h.groups, key), ".")
	}
	buf.WriteByte(' ')
	buf.WriteString(colorGray)
	buf.WriteString(key)
	buf.WriteString(colorReset)
	buf.WriteByte('=')
	buf.WriteString(formatValue(attr.Value))
}

func levelText(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN"
	case level <= slog.LevelDebug:
		return "DEBUG"
	default:
		return "INFO"
	}
}

func colorForLevel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return colorRed
	case level >= slog.LevelWarn:
		return colorYellow
	case level <= slog.LevelDebug:
		return colorGray
	default:
		return colorGreen
	}
}

func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return quoteIfNeeded(v.String())
	case slog.KindBool:
		return strconv.FormatBool(v.Bool())
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(v.Float64(), 'f', -1, 64)
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	default:
		return quoteIfNeeded(fmt.Sprint(v.Any()))
	}
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if unicode.IsSpace(r) || r == '=' || r == '"' || unicode.IsControl(r) {
			return strconv.Quote(s)
		}
	}
	return s
}
