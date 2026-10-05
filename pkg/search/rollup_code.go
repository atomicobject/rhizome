package search

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

type IntelModuleLookup interface {
	ModuleAnchorIDByPath(ctx context.Context, path string) (string, bool, error)
	ModuleAnchorIDsByPaths(ctx context.Context, paths []string) (map[string]string, error)
}

// CodeRollupper optionally replaces symbol-level code hits with a module-level hit when
// relevance is spread across multiple symbols within a file.
type CodeRollupper struct {
	Intel IntelModuleLookup
	Root  string

	SmallFileBytes int64
	SpreadRatio    float64
	SpreadCount    int
}

func (r *CodeRollupper) Rollup(ctx context.Context, spec QuerySpec, results []RankedResult) ([]RankedResult, error) {
	if len(spec.Filters.ExactSymbols) > 0 {
		return results, nil
	}

	byPath := make(map[string][]RankedResult)
	var passthrough []RankedResult

	for _, rr := range results {
		if rr.Type != "code" || strings.TrimSpace(rr.Path) == "" {
			passthrough = append(passthrough, rr)
			continue
		}
		byPath[rr.Path] = append(byPath[rr.Path], rr)
	}

	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var moduleIDs map[string]string
	if r.Intel != nil {
		ids, err := r.Intel.ModuleAnchorIDsByPaths(ctx, paths)
		if err != nil {
			return nil, err
		}
		moduleIDs = ids
	}

	out := append([]RankedResult(nil), passthrough...)

	for _, p := range paths {
		group := byPath[p]
		sort.Slice(group, func(i, j int) bool {
			if group[i].FinalScore == group[j].FinalScore {
				return group[i].Handle.String() < group[j].Handle.String()
			}
			return group[i].FinalScore > group[j].FinalScore
		})

		top := group
		if len(top) > 5 {
			top = top[:5]
		}
		if len(top) == 0 {
			continue
		}

		best := top[0].FinalScore
		sum3 := 0.0
		for i := 0; i < len(top) && i < 3; i++ {
			sum3 += top[i].FinalScore
		}

		spreadRatio := r.SpreadRatio
		if spreadRatio <= 0 {
			spreadRatio = 0.85
		}
		spreadCount := r.SpreadCount
		if spreadCount <= 0 {
			spreadCount = 3
		}

		spread := 0
		for _, t := range top {
			if t.FinalScore >= best*spreadRatio {
				spread++
			}
		}

		small := false
		smallFileBytes := r.SmallFileBytes
		if smallFileBytes <= 0 {
			smallFileBytes = 6000
		}
		if r.Root != "" && smallFileBytes > 0 {
			if fi, err := os.Stat(filepath.Join(r.Root, filepath.FromSlash(p))); err == nil {
				if fi.Size() <= smallFileBytes {
					small = true
				}
			}
		}

		// An exact symbol or definition hit is the answer; a small file must not
		// replace it with its module summary.
		if candidateHasEvidence(top[0].Candidate, "symbol_exact", "symbol_match", "definition_anchor") {
			out = append(out, top[0])
			continue
		}
		chooseModule := small || spread >= spreadCount || sum3 >= best*1.8
		if !chooseModule {
			out = append(out, top[0])
			continue
		}

		moduleID := ""
		if moduleIDs != nil {
			moduleID = moduleIDs[p]
		}
		if r.Intel != nil && moduleID == "" {
			id, ok, err := r.Intel.ModuleAnchorIDByPath(ctx, p)
			if err != nil {
				return nil, err
			}
			if ok {
				moduleID = id
			}
		}
		if moduleID == "" {
			out = append(out, top[0])
			continue
		}

		synth := top[0]
		synth.Handle = knowledge.CodeChunkHandle(moduleID, "module", 0)
		synth.Owner = synth.Handle.Owner()
		synth.AnchorID = moduleID
		synth.Kind = "module"
		synth.Granularity = "module"
		synth.ChunkIndex = 0
		synth.Symbol = firstNonEmpty(top[0].Symbol, filepath.Base(p))
		synth.FQN = top[0].FQN

		for i := 1; i < len(top); i++ {
			synth.Candidate = MergeCandidate(synth.Candidate, top[i].Candidate)
		}
		synth.FinalScore = math.Max(best, sum3/2)
		out = append(out, synth)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].FinalScore == out[j].FinalScore {
			return out[i].Handle.String() < out[j].Handle.String()
		}
		return out[i].FinalScore > out[j].FinalScore
	})
	if spec.Limits.Total > 0 && len(out) > spec.Limits.Total {
		out = out[:spec.Limits.Total]
	}
	return out, nil
}
