package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/finder"
)

// finderRebuildDelay coalesces bursts of index events into one rebuild.
const finderRebuildDelay = 300 * time.Millisecond

// FindItem is one ⌘P result. Match ranges are [start, end) in UTF-16 code units.
type FindItem struct {
	Kind         finder.Kind `json:"kind"`
	Path         string      `json:"path"`
	Title        string      `json:"title"`
	Type         string      `json:"type,omitempty"`
	Lang         string      `json:"lang,omitempty"`
	Score        int         `json:"score"`
	TitleMatches [][2]int    `json:"titleMatches"`
	PathMatches  [][2]int    `json:"pathMatches"`
}

// FindResponse is the /api/v1/find payload.
type FindResponse struct {
	Items []FindItem `json:"items"`
}

type findOpenRequest struct {
	Path string `json:"path"`
}

func (s *Server) loadFinderCatalog(ctx context.Context) (*finder.Catalog, error) {
	store := s.runtime.Intel()
	if store == nil {
		return nil, errIndexInitializing
	}
	notes, code, err := store.FinderCatalogRows(ctx)
	if err != nil {
		return nil, err
	}
	return finder.NewCatalog(notes, code), nil
}

// invalidateFinderOnEvent schedules a catalog rebuild when indexed notes or
// files change.
func (s *Server) invalidateFinderOnEvent(kind string) {
	if s.finder != nil && (kind == GlobalEventIndexChanged || kind == GlobalEventNodeChanged) {
		s.finder.Invalidate()
	}
}

func (s *Server) handleFind(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := finder.DefaultLimit
	if raw := query.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > finder.MaxLimit {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, fmt.Errorf("limit must be 1..%d", finder.MaxLimit))
			return
		}
		limit = n
	}
	kind := finder.Kind(query.Get("kind"))
	if kind != "" && kind != finder.KindNote && kind != finder.KindCode {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errors.New("kind must be note or code"))
		return
	}
	catalog, err := s.finder.Catalog(r.Context())
	if err != nil {
		if errors.Is(err, errIndexInitializing) {
			w.Header().Set("Retry-After", "5")
			writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errIndexInitializing)
			return
		}
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	opens := map[string]finder.Open{}
	if s.userState != nil {
		// Frecency only reorders; a failed read still answers the query.
		if records, err := s.userState.FinderOpens(r.Context()); err == nil {
			for _, record := range records {
				opens[record.Path] = finder.Open{Count: record.Count, LastOpened: record.LastOpened}
			}
		}
	}
	results := catalog.Search(r.Context(), finder.Query{Text: query.Get("q"), Limit: limit, Kind: kind, Opens: opens, Now: time.Now()})
	resp := FindResponse{Items: make([]FindItem, len(results))}
	for i, result := range results {
		resp.Items[i] = FindItem{
			Kind: result.Kind, Path: result.Path, Title: result.Title, Type: result.Type, Lang: result.Lang,
			Score: result.Score, TitleMatches: emptyRanges(result.TitleMatches), PathMatches: emptyRanges(result.PathMatches),
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

func emptyRanges(ranges [][2]int) [][2]int {
	if ranges == nil {
		return [][2]int{}
	}
	return ranges
}

func (s *Server) handleFindOpens(w http.ResponseWriter, r *http.Request) {
	if !s.viewPreferencesReady(w, r, http.MethodPost) {
		return
	}
	var req findOpenRequest
	if err := decodeViewPreferences(http.MaxBytesReader(w, r.Body, 8<<10), &req); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if err := s.userState.RecordFinderOpen(r.Context(), req.Path, time.Now()); err != nil {
		writeViewPreferencesError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
