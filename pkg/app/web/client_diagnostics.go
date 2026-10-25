package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"time"
)

// Browser batches are small aggregates; 32 KiB leaves room for every region
// and error-name bucket the client can produce.
const maxClientDiagnosticsBodyBytes = 32 << 10

// Bounds well above what the client produces, so the event stays small.
const (
	maxClientRegions    = 32
	maxClientErrorNames = 32
)

var (
	clientSessionPattern   = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
	clientBuildPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	clientRegionPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
	clientErrorNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.$]{0,63}$`)
	clientSurfaces         = map[string]bool{"desktop": true, "browser": true}
	clientRouteKinds       = map[string]bool{"notes": true, "ontology": true, "explorer": true, "agent": true, "graphql": true, "other": true}
)

// clientBatch is the browser's diagnostics window (SPEC-0116 "Web UI
// evidence"). Counts are unsigned so negative values fail decoding.
type clientBatch struct {
	Session     string                  `json:"session"`
	Build       string                  `json:"build"`
	Surface     string                  `json:"surface"`
	Route       string                  `json:"route"`
	WindowStart int64                   `json:"window_start"`
	WindowEnd   int64                   `json:"window_end"`
	Regions     map[string]clientRegion `json:"regions"`
	Frames      clientFrames            `json:"frames"`
	Refresh     clientRefresh           `json:"refresh"`
	Stability   clientStability         `json:"stability"`
}

type clientRegion struct {
	Mount               uint64  `json:"mount"`
	Update              uint64  `json:"update"`
	NestedUpdate        uint64  `json:"nested_update"`
	ActualTotalMs       float64 `json:"actual_total_ms"`
	ActualMaxMs         float64 `json:"actual_max_ms"`
	BaseTotalMs         float64 `json:"base_total_ms"`
	BaseMaxMs           float64 `json:"base_max_ms"`
	CommitsAfterRefresh uint64  `json:"commits_after_refresh"`
}

type clientFrames struct {
	Over50ms  uint64  `json:"over_50ms"`
	Over200ms uint64  `json:"over_200ms"`
	StalledMs float64 `json:"stalled_ms"`
	LongestMs float64 `json:"longest_ms"`
}

type clientRefresh struct {
	Cycles    uint64 `json:"cycles"`
	Refetched uint64 `json:"refetched"`
	Unchanged uint64 `json:"unchanged"`
	Churned   uint64 `json:"churned"`
}

type clientStability struct {
	Reconnects uint64            `json:"reconnects"`
	Errors     map[string]uint64 `json:"errors"`
	Rejections map[string]uint64 `json:"rejections"`
}

// handleClientDiagnostics records one browser batch as one web.client event.
// The global host and same-origin checks in Handler already ran.
func (s *Server) handleClientDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if s.diagnostics == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var batch clientBatch
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxClientDiagnosticsBodyBytes))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&batch)
	if err == nil && decoder.Decode(new(any)) != io.EOF {
		err = errors.New("expected exactly one JSON value")
	}
	if err == nil {
		err = batch.validate()
	}
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, fmt.Errorf("invalid diagnostics batch: %w", err))
		return
	}
	metrics, err := json.Marshal(struct {
		Regions   map[string]clientRegion `json:"regions,omitempty"`
		Frames    clientFrames            `json:"frames"`
		Refresh   clientRefresh           `json:"refresh"`
		Stability clientStability         `json:"stability"`
	}{batch.Regions, batch.Frames, batch.Refresh, batch.Stability})
	if err != nil {
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	start, end := time.UnixMilli(batch.WindowStart).UTC(), time.UnixMilli(batch.WindowEnd).UTC()
	s.diagnostics.EventMetrics(r.Context(), slog.LevelInfo, "web", "web.client", "", metrics,
		slog.String("session", batch.Session),
		slog.String("build", batch.Build),
		slog.String("surface", batch.Surface),
		slog.String("route", batch.Route),
		slog.String("window_start", start.Format(time.RFC3339Nano)),
		slog.String("window_end", end.Format(time.RFC3339Nano)),
		slog.Int64("window_ms", batch.WindowEnd-batch.WindowStart),
	)
	w.WriteHeader(http.StatusNoContent)
}

func (b clientBatch) validate() error {
	switch {
	case !clientSessionPattern.MatchString(b.Session):
		return errors.New("session is not a client session identifier")
	case !clientBuildPattern.MatchString(b.Build):
		return errors.New("build is not a build identifier")
	case !clientSurfaces[b.Surface]:
		return errors.New("surface must be desktop or browser")
	case !clientRouteKinds[b.Route]:
		return errors.New("route is not a known route kind")
	case b.WindowStart <= 0 || b.WindowEnd < b.WindowStart:
		return errors.New("window must have a positive start no later than its end")
	case len(b.Regions) > maxClientRegions:
		return errors.New("too many render regions")
	case len(b.Stability.Errors) > maxClientErrorNames || len(b.Stability.Rejections) > maxClientErrorNames:
		return errors.New("too many error names")
	}
	for name, region := range b.Regions {
		if !clientRegionPattern.MatchString(name) {
			return errors.New("region name is not a region identifier")
		}
		if !validDurations(region.ActualTotalMs, region.ActualMaxMs, region.BaseTotalMs, region.BaseMaxMs) {
			return errors.New("region durations must be non-negative")
		}
	}
	if !validDurations(b.Frames.StalledMs, b.Frames.LongestMs) {
		return errors.New("frame durations must be non-negative")
	}
	for _, counts := range []map[string]uint64{b.Stability.Errors, b.Stability.Rejections} {
		for name := range counts {
			if !clientErrorNamePattern.MatchString(name) {
				return errors.New("error name is not an identifier")
			}
		}
	}
	return nil
}

func validDurations(values ...float64) bool {
	for _, value := range values {
		if value < 0 || math.IsInf(value, 0) || math.IsNaN(value) {
			return false
		}
	}
	return true
}
