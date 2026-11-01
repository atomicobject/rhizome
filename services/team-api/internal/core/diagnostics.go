package core

import (
	"context"
	"strings"
	"time"
)

// RequestDiagnostics contains only fields approved for request logging. Handlers
// fill it synchronously; the outer HTTP handler emits it after they return.
type RequestDiagnostics struct {
	Sub, SessionID, Model  string
	Inputs, UpstreamStatus int
	Authentication         time.Duration
}
type requestDiagnosticsKey struct{}

func WithRequestDiagnostics(ctx context.Context, info *RequestDiagnostics) context.Context {
	return context.WithValue(ctx, requestDiagnosticsKey{}, info)
}
func DiagnosticsFrom(ctx context.Context) *RequestDiagnostics {
	info, _ := ctx.Value(requestDiagnosticsKey{}).(*RequestDiagnostics)
	return info
}

// ClientVersion deliberately rejects arbitrary User-Agent text, including email
// addresses and tokens, rather than copying a caller-controlled header to logs.
func ClientVersion(ua string) string {
	first, _, _ := strings.Cut(ua, " ")
	v, ok := strings.CutPrefix(first, "rzm/")
	if !ok || len(v) > 64 {
		return ""
	}
	if v == "dev" {
		return v
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+' || c >= 'a' && c <= 'z') {
			return ""
		}
	}
	if len(v) == 0 || v[0] < '0' || v[0] > '9' {
		return ""
	}
	return v
}

// LogIdentifier bounds operational identifiers and rejects credential/email/URL
// syntax. Invalid caller-supplied session and admin targets must not reach logs.
func LogIdentifier(value string) string {
	if len(value) > 128 || strings.HasPrefix(value, "rzt_") {
		return ""
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.') {
			return ""
		}
	}
	return value
}
