package semantic

import (
	"context"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

const codeCallSummaryLimit = 8

func prefetchFileCallees(ctx context.Context, lookup CallLookup, paths []string) map[string][]string {
	if lookup == nil {
		return nil
	}
	started := time.Now()
	defer func() { indexingperf.ObserveLatency(ctx, "plan.call_lookup_files", time.Since(started)) }()
	if batch, ok := lookup.(CallLookupBatch); ok {
		rows, err := batch.CalleesForFiles(ctx, paths, codeCallSummaryLimit)
		if err != nil {
			return nil
		}
		return rows
	}
	rows := make(map[string][]string, len(paths))
	for _, path := range paths {
		if callees, err := lookup.CalleesForFile(ctx, path, codeCallSummaryLimit); err == nil && len(callees) > 0 {
			rows[path] = callees
		}
	}
	return rows
}

// CallLookup provides best-effort call graph summaries used during synthesis.
type CallLookup interface {
	CalleesForOwnerFQN(ctx context.Context, lang, ownerFQN string, limit int) ([]string, error)
	CalleesForFile(ctx context.Context, path string, limit int) ([]string, error)
}

type CallLookupBatch interface {
	CallLookup
	CalleesForOwnerFQNs(ctx context.Context, lang string, ownerFQNs []string, limitPerOwner int) (map[string][]string, error)
	CalleesForFiles(ctx context.Context, paths []string, limitPerFile int) (map[string][]string, error)
}

func prefetchOwnerCallees(
	ctx context.Context,
	lookup CallLookup,
	moduleByPath map[string][]codeanchor.IntelAnchor,
) map[string]map[string][]string {
	if lookup == nil || len(moduleByPath) == 0 {
		return nil
	}

	ownersByLang := make(map[string][]string)
	for _, anchors := range moduleByPath {
		for _, anchor := range anchors {
			if strings.EqualFold(anchor.Kind, "module") {
				continue
			}
			owner := strings.TrimSpace(anchor.FQN)
			if owner == "" {
				continue
			}
			lang := strings.TrimSpace(string(anchor.Lang))
			ownersByLang[lang] = append(ownersByLang[lang], owner)
		}
	}
	if len(ownersByLang) == 0 {
		return nil
	}

	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "plan.call_lookup_owners", time.Since(started))
	}()

	out := make(map[string]map[string][]string, len(ownersByLang))
	if batch, ok := lookup.(CallLookupBatch); ok {
		for lang, owners := range ownersByLang {
			rows, err := batch.CalleesForOwnerFQNs(ctx, lang, owners, codeCallSummaryLimit)
			if err != nil || len(rows) == 0 {
				continue
			}
			out[lang] = rows
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}

	for lang, owners := range ownersByLang {
		owners = dedupeNonEmptyStrings(owners)
		if len(owners) == 0 {
			continue
		}
		rows := make(map[string][]string, len(owners))
		for _, owner := range owners {
			callees, err := lookup.CalleesForOwnerFQN(ctx, lang, owner, codeCallSummaryLimit)
			if err != nil || len(callees) == 0 {
				continue
			}
			rows[owner] = callees
		}
		if len(rows) > 0 {
			out[lang] = rows
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
