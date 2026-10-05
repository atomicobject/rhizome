package diagnostics

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
)

type handler struct {
	recorder         *Recorder
	subsystem, group string
	attrs            []slog.Attr
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return h.recorder != nil && (level >= slog.LevelWarn || (!h.recorder.opts.Disabled && level >= h.recorder.opts.Level))
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	attrs := append([]slog.Attr(nil), h.attrs...)
	name := "log"
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "event" && attr.Value.Kind() == slog.KindString {
			name = attr.Value.String()
		} else if len(attrs) < maxAttrs {
			attr.Key = h.group + attr.Key
			attrs = append(attrs, attr)
		}
		return true
	})
	h.recorder.Event(ctx, record.Level, h.subsystem, name, record.Message, attrs...)
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, attr := range attrs {
		if len(clone.attrs) == maxAttrs {
			break
		}
		attr.Key = h.group + attr.Key
		// Bound retained strings as well as serialized output.
		if attr.Value.Kind() == slog.KindString {
			value, _ := boundedString(attr.Value.String(), 1024)
			attr.Value = slog.StringValue(value)
		}
		attr.Key, _ = boundedString(attr.Key, 64)
		if attr.Value.Kind() == slog.KindAny || attr.Value.Kind() == slog.KindGroup || attr.Value.Kind() == slog.KindLogValuer {
			attr.Value = slog.StringValue("[unsupported value]")
		}
		clone.attrs = append(clone.attrs, attr)
	}
	return &clone
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.group, _ = boundedString(h.group+name+".", 128)
	return &clone
}

type stdlogWriter struct {
	recorder  *Recorder
	subsystem string
}

func (w *stdlogWriter) Write(data []byte) (int, error) {
	if w.recorder == nil {
		return len(data), nil
	}
	remaining := data
	for len(remaining) > 0 {
		line, rest, found := bytes.Cut(remaining, []byte{'\n'})
		if !found {
			rest = nil
		}
		remaining = rest
		if len(line) == 0 {
			continue
		}
		// log.Logger writes one full record at a time; retain no partial buffer.
		prefix := line
		if len(prefix) > 64 {
			prefix = prefix[:64]
		}
		text := strings.ToLower(string(prefix))
		level := slog.LevelInfo
		if strings.Contains(text, "error:") || strings.Contains(text, "fatal:") || strings.Contains(text, "panic:") {
			level = slog.LevelError
		} else if strings.Contains(text, "warning:") || strings.Contains(text, "warn:") {
			level = slog.LevelWarn
		}
		limited := line
		if len(limited) > 4096 {
			limited = limited[:4096]
		}
		w.recorder.Event(context.Background(), level, w.subsystem, "legacy.log", string(limited), slog.Bool("legacy", true), slog.Bool("message_truncated", len(line) > len(limited)))
	}
	return len(data), nil
}
