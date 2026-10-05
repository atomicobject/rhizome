package diagnostics

import (
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const maxAttrs = 32

// CLI recorders usually retain only a handful of events. Their reservation
// should not evict history merely because many short commands overlap.
const defaultCLISegmentBytes int64 = 64 << 10

func (r *Recorder) segmentBytes() int64 {
	if r.opts.Role == "cli" {
		return min(r.opts.MaxSegmentBytes, defaultCLISegmentBytes)
	}
	return r.opts.MaxSegmentBytes
}

var bearerSecret = regexp.MustCompile(`(?i)\bBearer\s+[^\s,;]+`)
var assignedSecret = regexp.MustCompile(`(?i)(api[_-]?key|authorization|password|secret|access[_-]?token|control[_-]?token)\s*[:=]\s*[^\s,;]+`)

func redact(value string) string {
	value = bearerSecret.ReplaceAllString(value, "Bearer [redacted]")
	return assignedSecret.ReplaceAllString(value, "[redacted credential]")
}

func boundedString(value string, max int) (string, bool) {
	if len(value) <= max {
		return value, false
	}
	end := max
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end], true
}

func sensitiveKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	return strings.Contains(k, "password") || strings.Contains(k, "secret") ||
		strings.Contains(k, "apikey") || strings.Contains(k, "authorization") ||
		strings.Contains(k, "credential") || strings.HasSuffix(k, "token") ||
		k == "body" || k == "content" || k == "prompt" || k == "response" || k == "headers"
}

func scalar(value any) (any, bool) {
	switch v := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return v, false
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, true
		}
		return v, false
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, true
		}
		return v, false
	case string:
		text, cut := boundedString(v, 1024)
		return redact(text), cut
	case time.Duration:
		return v.Milliseconds(), false
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano), false
	default:
		return "[unsupported value]", true
	}
}

func boundedAttrs(attrs map[string]any) (map[string]any, bool) {
	if len(attrs) == 0 {
		return nil, false
	}
	result := make(map[string]any, min(len(attrs), maxAttrs))
	truncated := len(attrs) > maxAttrs
	for key, value := range attrs {
		if len(result) == maxAttrs {
			break
		}
		name, cut := boundedString(key, 64)
		truncated = truncated || cut
		if sensitiveKey(name) {
			result[name] = "[redacted]"
			continue
		}
		v, cut := scalar(value)
		result[name] = v
		truncated = truncated || cut
	}
	return result, truncated
}

func attrsFromSlog(attrs []slog.Attr) (map[string]any, bool) {
	values := make(map[string]any, min(len(attrs), maxAttrs))
	truncated := len(attrs) > maxAttrs
	for _, attr := range attrs {
		if len(values) == maxAttrs {
			break
		}
		// Do not invoke arbitrary LogValuer implementations or serialize objects.
		value := attr.Value
		if value.Kind() == slog.KindLogValuer {
			values[attr.Key] = "[unsupported value]"
			truncated = true
			continue
		}
		if value.Kind() == slog.KindGroup {
			values[attr.Key] = "[group omitted]"
			truncated = true
			continue
		}
		values[attr.Key] = value.Any()
	}
	bounded, cut := boundedAttrs(values)
	return bounded, truncated || cut
}
