package web

import (
	"context"
	"log/slog"
	"net/http"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
)

type diagnosticResponse struct {
	http.ResponseWriter
	status, bytes int
}

func (w *diagnosticResponse) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *diagnosticResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

type diagnosticFlusher struct{ *diagnosticResponse }

func (w diagnosticFlusher) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *diagnosticResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) observeRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.diagnostics == nil {
			handler.ServeHTTP(w, r)
			return
		}
		_, route := s.mux.Handler(r)
		if route == "" {
			route = "unmatched"
		}
		method := r.Method
		switch method {
		case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		default:
			method = "other"
		}
		op := diagnostics.NewOperation("http.request", route)
		if s.controlAuthorized(r) && isLoopbackHostname(requestHostname(r.Host)) {
			trace, parent := appruntime.RequestDiagnosticIdentity(r)
			if trace != "" {
				op.TraceID = trace
			}
			op.ParentID = parent
		}
		ctx := diagnostics.WithRecorder(r.Context(), s.diagnostics)
		ctx = diagnostics.WithOperation(ctx, op)
		s.diagnostics.Event(ctx, slog.LevelInfo, "http", "request.started", "", slog.String("route", route), slog.String("method", method))
		observed := &diagnosticResponse{ResponseWriter: w}
		defer func() {
			panicked := recover()
			status, reason := "success", ""
			level := slog.LevelInfo
			code := observed.status
			if code == 0 {
				code = http.StatusOK
			}
			if code >= 400 {
				status = "error"
				reason = "http_error"
			}
			if code >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			if ctx.Err() != nil {
				status = "canceled"
				level = slog.LevelInfo
				reason = logging.ClassifyError(ctx.Err())
			}
			if panicked != nil {
				level = slog.LevelError
				status = "error"
				reason = "handler_panicked"
				if observed.status == 0 {
					code = http.StatusInternalServerError
				}
			}
			logging.CompleteEventQuietAtLevel(context.WithoutCancel(ctx), op, level, status, reason, map[string]any{"route": route, "method": method, "http_status": code, "response_bytes": observed.bytes})
			if panicked != nil {
				panic(panicked)
			}
		}()
		var response http.ResponseWriter = observed
		if _, ok := w.(http.Flusher); ok {
			response = diagnosticFlusher{observed}
		}
		handler.ServeHTTP(response, r.WithContext(ctx))
	})
}
