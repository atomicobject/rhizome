package searchengine

import (
	"context"
	"errors"
	"fmt"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/relevance"
	"github.com/atomicobject/rhizome/pkg/search/rerank"
)

var ErrTimedOutBeforePlanning = fmt.Errorf("search timed out before planning completed")

// EngineRequest contains the prepared dependencies and policy needed to run
// one canonical search. Adapters own resource lifetimes and serialization; the
// application layer owns planner execution, ranker decoration, and rollup.
type Request struct {
	Dependencies searchplanner.Deps
	Options      searchplanner.Options
	Spec         search.QuerySpec
	VaultPath    string
	IntelStore   *semdb.Store
	DeferPacking bool
}

// ExecuteEngine is the shared planner-to-service execution path for every
// search adapter.
func Execute(ctx context.Context, request Request) (search.Response, error) {
	planner := searchplanner.Planner{Deps: request.Dependencies, Options: request.Options}
	plan, err := planner.Plan(ctx, request.Spec)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return search.Response{}, ErrTimedOutBeforePlanning
		}
		return search.Response{}, err
	}
	ranker := plan.Ranker
	if request.IntelStore != nil {
		ranker = &relevance.GraphAnchorScoreRanker{Base: ranker, Store: request.IntelStore}
		ranker = &relevance.GraphDocScoreRanker{Base: ranker, Store: request.IntelStore}
		ranker = &relevance.LinkedCorroborationRanker{Base: ranker, Store: request.IntelStore}
	}
	if reranker, rerankCfg, enabled := rerank.NewFromEnv(); enabled {
		ranker = &relevance.RerankingRanker{Base: ranker, Reranker: reranker, Model: rerankCfg.Model}
	}
	packer := plan.Packer
	if request.DeferPacking {
		packer = nil
	}
	svc := search.Service{
		Retrievers: plan.Retrievers,
		Ranker:     ranker,
		Shaper:     plan.Shaper,
		Rollupper:  engineRollupper(request.Spec.Intent, request.IntelStore, request.VaultPath),
		Packer:     packer,
	}
	response, err := svc.Search(ctx, request.Spec)
	if err == nil && request.DeferPacking {
		response.DeferredPacker = plan.Packer
	}
	return response, err
}

func engineRollupper(intent search.Intent, intelStore *semdb.Store, vaultPath string) search.Rollupper {
	if intelStore == nil {
		return nil
	}
	if intent == search.IntentOverview || intent == search.IntentSubsystemOverview {
		return &search.CodeRollupper{
			Intel:          intelStore,
			Root:           vaultPath,
			SmallFileBytes: 20000,
			SpreadRatio:    0.7,
			SpreadCount:    2,
		}
	}
	return &search.CodeRollupper{Intel: intelStore, Root: vaultPath}
}
