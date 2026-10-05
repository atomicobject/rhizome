package indexingperf

import (
	"runtime"
	"strings"
)

func inferDBWriteOp() string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(2, pcs)
	if n == 0 {
		return "unknown"
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		fn := strings.TrimSpace(frame.Function)
		if isDBWriteHelperFrame(fn) {
			if !more {
				break
			}
			continue
		}
		if label := callerLabel(fn); label != "" {
			return label
		}
		if !more {
			break
		}
	}
	return "unknown"
}

func isDBWriteHelperFrame(fn string) bool {
	if fn == "" {
		return true
	}
	return strings.Contains(fn, "runtime.") ||
		strings.HasSuffix(fn, ".ObserveDBWrite") ||
		strings.HasSuffix(fn, ".withWrite") ||
		strings.HasSuffix(fn, ".withWriteTx") ||
		strings.HasSuffix(fn, ".WithWrite") ||
		strings.HasSuffix(fn, ".WithWriteTx")
}

func callerLabel(fn string) string {
	// These labels are intentionally diagnostic-only. They should point us at
	// the missed write path in timings without pretending to be a curated stable
	// metric name.
	if fn == "" {
		return ""
	}
	if i := strings.Index(fn, "/pkg/"); i >= 0 {
		fn = fn[i+1:]
	} else if i := strings.Index(fn, "/cmd/"); i >= 0 {
		fn = fn[i+1:]
	} else if i := strings.LastIndex(fn, "/"); i >= 0 {
		fn = fn[i+1:]
	}
	fn = strings.TrimPrefix(fn, "pkg/")
	fn = strings.TrimPrefix(fn, "cmd/")
	fn = strings.ReplaceAll(fn, "(*Store).", "")
	fn = strings.ReplaceAll(fn, "(*Runtime).", "")
	fn = strings.ReplaceAll(fn, "(*", "")
	fn = strings.ReplaceAll(fn, ").", ".")
	fn = strings.ReplaceAll(fn, "/", ".")
	fn = strings.TrimSuffix(fn, ".func1")
	if strings.TrimSpace(fn) == "" {
		return ""
	}
	return "auto." + fn
}
