package web

import (
	"context"
	"encoding/json"
	"strconv"

	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// cachedViewExecution executes a configured view without an edit session
// through the read cache, keyed on the generation and the whole execution
// request (SPEC-0136 R4). Query-recipe views are executed once per concurrent
// burst but never stored: a recipe file edit moves no generation component.
// Executions whose answer depends on the clock are never stored either.
func (s *Server) cachedViewExecution(ctx context.Context, generation string, cacheable bool, viewID string, req appviews.ExecuteRequest) ([]byte, error) {
	req.EditSession = nil
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	queryHash := readQueryHash("view", viewID)
	run := func(ctx context.Context) ([]byte, bool, error) {
		service, err := s.configuredViewsService(ctx, nil)
		if err != nil {
			return nil, false, err
		}
		resp, err := service.Execute(ctx, viewID, req)
		if err != nil {
			return nil, false, err
		}
		body, err := encodeJSONBody(resp)
		if err != nil {
			return nil, false, err
		}
		storable := resp.View.Source.Kind != viewconfig.SourceKindQueryRecipe && !resp.ClockDependent && s.readStillCurrent(ctx, generation)
		return body, storable, nil
	}
	if !cacheable {
		s.reads.bypass(queryHash)
		body, _, err := run(ctx)
		return body, err
	}
	key := readCacheKey(generation, "view", viewID, string(encoded), strconv.FormatBool(req.PageSet))
	return s.reads.do(ctx, key, queryHash, run)
}
