package search

import "context"

type SubsystemOverviewShaper struct{}

func (s *SubsystemOverviewShaper) Shape(ctx context.Context, spec QuerySpec, results []RankedResult) ([]RankedResult, error) {
	_ = ctx
	if spec.Intent != IntentSubsystemOverview || !spec.HasExplicitSeeds || len(results) < 2 {
		return results, nil
	}

	ordered := make([]RankedResult, 0, len(results))
	deferred := make([]RankedResult, 0, len(results))
	seenFamilies := map[string]bool{}
	used := make([]bool, len(results))

	appendBucket := func(pred func(RankedResult) bool) {
		for i, rr := range results {
			if used[i] || !pred(rr) {
				continue
			}
			used[i] = true
			fam := resultFamilyKey(rr)
			if !seenFamilies[fam] {
				seenFamilies[fam] = true
				ordered = append(ordered, rr)
				continue
			}
			deferred = append(deferred, rr)
		}
	}

	appendBucket(func(rr RankedResult) bool {
		return rr.DocClass == DocClassModule && rr.PrimaryDoc && SeedPathProximity(rr.Path, spec.ExplicitSeedPaths) > 0
	})
	appendBucket(func(rr RankedResult) bool {
		return isDocCandidate(rr.Path, rr.Type) && SeedPathProximity(rr.Path, spec.ExplicitSeedPaths) > 0
	})
	appendBucket(func(rr RankedResult) bool {
		return rr.Type == "code" && !IsTestPath(rr.Path) && SeedPathProximity(rr.Path, spec.ExplicitSeedPaths) > 0
	})
	appendBucket(func(rr RankedResult) bool {
		return rr.DocClass == DocClassHub || rr.DocClass == DocClassReference
	})
	appendBucket(func(rr RankedResult) bool {
		return rr.DocClass != DocClassRepoGlobal && rr.DocClass != DocClassGenerated
	})
	for i, rr := range results {
		if used[i] {
			continue
		}
		used[i] = true
		fam := resultFamilyKey(rr)
		if !seenFamilies[fam] {
			seenFamilies[fam] = true
			ordered = append(ordered, rr)
			continue
		}
		deferred = append(deferred, rr)
	}

	ordered = append(ordered, deferred...)
	ensureLocalCodeEarly(spec, ordered)
	if spec.Limits.Total > 0 && len(ordered) > spec.Limits.Total {
		ordered = ordered[:spec.Limits.Total]
	}
	return ordered, nil
}

func ensureLocalCodeEarly(spec QuerySpec, results []RankedResult) {
	if len(results) < 3 {
		return
	}
	codeIdx := -1
	for i, rr := range results {
		if rr.Type == "code" && !IsTestPath(rr.Path) && SeedPathProximity(rr.Path, spec.ExplicitSeedPaths) > 0 {
			codeIdx = i
			break
		}
	}
	if codeIdx <= 0 || codeIdx < min(len(results), 4) {
		return
	}
	target := min(2, len(results)-1)
	code := results[codeIdx]
	copy(results[target+1:codeIdx+1], results[target:codeIdx])
	results[target] = code
}

func resultFamilyKey(rr RankedResult) string {
	switch rr.Type {
	case "note":
		return "note:" + rr.Path
	case "code":
		return "code:" + rr.Path
	default:
		return rr.Type + ":" + rr.Path + ":" + rr.AnchorID
	}
}
