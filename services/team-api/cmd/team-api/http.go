package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/auth"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
)

// Trust exactly one ALB hop, only from the configured ALB subnet CIDRs. ALB
// append mode puts the connection's client IP last, after any spoofed prefix.
// Task ingress must also allow only the managed ALB security group.
func trustedForwardedAddress(cidrs []string, next http.Handler) http.Handler {
	var trusted []netip.Prefix
	for _, cidr := range cidrs {
		p, err := netip.ParsePrefix(cidr)
		if err == nil {
			trusted = append(trusted, p)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		peer, _ := netip.ParseAddr(host)
		for _, prefix := range trusted {
			if !prefix.Contains(peer.Unmap()) {
				continue
			}
			headers := r.Header.Values("X-Forwarded-For")
			if len(headers) != 1 {
				break
			}
			entries := strings.Split(headers[len(headers)-1], ",")
			last := strings.TrimSpace(entries[len(entries)-1])
			client, err := netip.ParseAddr(last)
			if err != nil {
				if ap, e := netip.ParseAddrPort(last); e == nil {
					client, err = ap.Addr(), nil
				}
			}
			if err == nil {
				r.RemoteAddr = net.JoinHostPort(client.Unmap().String(), "0")
			}
			break
		}
		next.ServeHTTP(w, r)
	})
}

type responseLog struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseLog) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseLog) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseLog) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func requestDiagnostics(rec *diagnostics.Recorder, next http.Handler) http.Handler {
	return auth.WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &core.RequestDiagnostics{}
		r = r.WithContext(core.WithRequestDiagnostics(r.Context(), info))
		response := &responseLog{ResponseWriter: w}
		// Log from a defer so an aborted request (a panic, including
		// http.ErrAbortHandler) still gets its record; the panic continues.
		defer func() {
			panicked := recover()
			if response.status == 0 && panicked == nil {
				response.status = http.StatusOK
			}
			logRequest(rec, w, r, info, response, start, panicked != nil)
			if panicked != nil {
				panic(panicked)
			}
		}()
		next.ServeHTTP(response, r)
	}))
}

func logRequest(rec *diagnostics.Recorder, w http.ResponseWriter, r *http.Request, info *core.RequestDiagnostics, response *responseLog, start time.Time, aborted bool) {
	route := info.Route
	if route == "" {
		route = r.Pattern
	}
	if route == "" {
		route = "unmatched"
	}
	metrics, _ := json.Marshal(map[string]any{"bytes_in": r.ContentLength, "bytes_out": response.bytes, "input_count": info.Inputs, "duration_ms": float64(time.Since(start).Microseconds()) / 1000, "authentication_ms": float64(info.Authentication.Microseconds()) / 1000})
	rec.EventMetrics(context.WithoutCancel(r.Context()), slog.LevelInfo, "teamapi", "teamapi.request", "", metrics,
		slog.String("request_id", w.Header().Get(wire.HeaderRequestID)), slog.String("route", route),
		slog.String("sub", core.LogIdentifier(info.Sub)), slog.String("session_id", core.LogIdentifier(info.SessionID)), slog.String("model", info.Model),
		slog.Int("status", response.status), slog.Int("upstream_status", info.UpstreamStatus), slog.String("gateway_code", w.Header().Get(wire.HeaderGatewayError)),
		slog.String("client_version", core.ClientVersion(r.UserAgent())), slog.Bool("aborted", aborted))
}
