package retrieval

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

// TestsForCodeRetriever finds test files related to code seeds using path heuristics.
type TestsForCodeRetriever struct {
	Store     *semdb.Store
	VaultPath string
	Limit     int
}

func (r *TestsForCodeRetriever) Name() string { return "tests_for_code" }

func (r *TestsForCodeRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.VaultPath == "" && r.Store == nil {
		return nil, nil
	}
	if len(spec.Seeds) == 0 && len(spec.ExplicitSeedPaths) == 0 {
		return nil, nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 20
	}

	fileSeeds := make([]string, 0, len(spec.Seeds))
	anchorSeeds := make([]string, 0, len(spec.Seeds))
	for _, seed := range spec.Seeds {
		switch seed.Kind {
		case knowledge.KindFile:
			if p := strings.TrimSpace(seed.ID); p != "" {
				fileSeeds = append(fileSeeds, filepath.ToSlash(p))
			}
		case knowledge.KindAnchor:
			id := strings.TrimSpace(seed.ID)
			if r.Store != nil && id != "" {
				anchorSeeds = append(anchorSeeds, id)
			}
		}
	}
	anchorSeeds = dedupeStrings(anchorSeeds)
	anchorsByID := map[string]codeanchor.IntelAnchor{}
	if len(anchorSeeds) > 0 {
		var err error
		anchorsByID, err = r.Store.IntelAnchorsByIDs(ctx, anchorSeeds)
		if err != nil {
			return nil, err
		}
		for _, id := range anchorSeeds {
			if p := strings.TrimSpace(anchorsByID[id].Path); p != "" {
				fileSeeds = append(fileSeeds, filepath.ToSlash(p))
			}
		}
	}
	if len(fileSeeds) == 0 {
		for _, seedPath := range spec.ExplicitSeedPaths {
			if p := strings.TrimSpace(seedPath); p != "" {
				fileSeeds = append(fileSeeds, filepath.ToSlash(p))
			}
		}
	}
	fileSeeds = dedupeStrings(fileSeeds)
	if len(fileSeeds) == 0 {
		return nil, nil
	}

	out := make([]search.Candidate, 0, limit)
	seen := make(map[string]int, limit)
	directFileRelationship := false
	addDirectCandidate := func(caller codeanchor.IntelAnchor, targetID, targetFQN string) {
		path := filepath.ToSlash(strings.TrimSpace(caller.Path))
		if !search.IsTestPath(path) || !spec.Filters.AllowsPath(path) ||
			!spec.Filters.AllowsTestPath(path) || !spec.Filters.AllowsCandidateType("anchor") {
			return
		}
		h := knowledge.AnchorHandle(caller.AnchorID)
		evidence := search.Evidence{Type: "tests_path", RawScore: 1.2, Source: "tests_for_code", Details: map[string]string{
			"relationship": "tests", "target_anchor_id": targetID, "target_fqn": targetFQN,
		}}
		if index, exists := seen[h.String()]; exists {
			for _, existing := range out[index].Evidence {
				if existing.Type == evidence.Type && existing.Details["target_anchor_id"] == targetID {
					return
				}
			}
			out[index].Evidence = append(out[index].Evidence, evidence)
			return
		}
		if len(out) >= limit {
			return
		}
		seen[h.String()] = len(out)
		out = append(out, search.Candidate{
			Handle: h, Owner: h, Evidence: []search.Evidence{evidence},
			Type: "anchor", Path: path, Title: caller.Symbol, Symbol: caller.Symbol, FQN: caller.FQN,
			Kind: caller.Kind, ChunkIndex: -1, AnchorID: caller.AnchorID,
		})
	}
	if len(anchorSeeds) == 0 && r.Store != nil {
		anchorsByPath, err := r.Store.IntelAnchorsByPaths(ctx, fileSeeds)
		if err != nil {
			return nil, err
		}
		for _, fileSeed := range fileSeeds {
			for _, target := range anchorsByPath[fileSeed] {
				if !r.directRelationshipTargetIsUnambiguous(ctx, target) {
					continue
				}
				callers, err := r.Store.CallerTestAnchorsByCalleeIDs(ctx, []string{target.AnchorID}, spec.Filters.PathPrefixes, limit)
				if err != nil {
					return nil, err
				}
				for _, caller := range rankTestAnchorsForQuery(spec.Text, callers[target.AnchorID]) {
					before := len(out)
					addDirectCandidate(caller, target.AnchorID, target.FQN)
					directFileRelationship = directFileRelationship || len(out) > before
				}
			}
		}
	}
	requiresDirectRelationship := spec.ResolvedTarget != nil &&
		(strings.TrimSpace(spec.ResolvedTarget.FQN) != "" || strings.TrimSpace(spec.ResolvedTarget.Symbol) != "")
	requiresDirectRelationship = requiresDirectRelationship || directFileRelationship

	if requiresDirectRelationship && len(anchorSeeds) > 0 {
		callers, err := r.Store.CallerTestAnchorsByCalleeIDs(ctx, anchorSeeds, spec.Filters.PathPrefixes, limit)
		if err != nil {
			return nil, err
		}
		for _, targetID := range anchorSeeds {
			target, ok := anchorsByID[targetID]
			if !ok || !r.directRelationshipTargetIsUnambiguous(ctx, target) {
				continue
			}
			for _, caller := range callers[targetID] {
				addDirectCandidate(caller, targetID, target.FQN)
			}
		}
	}

	addCandidate := func(path, seed string) {
		if path == "" {
			return
		}
		h := knowledge.FileHandle(path)
		key := h.String()
		if _, ok := seen[key]; ok {
			return
		}
		if len(out) >= limit {
			return
		}
		seen[key] = len(out)
		evidenceType := testEvidenceType(path, seed)
		rawScore := 1.1
		if requiresDirectRelationship {
			evidenceType = "code_anchor"
			rawScore = 0.35
		}
		out = append(out, search.Candidate{
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{{
				Type:     evidenceType,
				RawScore: rawScore,
				Source:   "tests_for_code",
				Details: map[string]string{
					"seed": seed,
				},
			}},
			Type:  "code",
			Path:  path,
			Title: path,
		})
	}

	seedCandidates := make(map[string][]string, len(fileSeeds))
	orderedSeeds := make([]string, 0, len(fileSeeds))
	for _, seed := range fileSeeds {
		candidates := testPathCandidates(seed)
		candidates = append(candidates, goPackageTestCandidates(r.VaultPath, seed)...)
		if len(candidates) == 0 {
			continue
		}
		valid := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if testPathExists(ctx, r.VaultPath, r.Store, candidate) {
				valid = append(valid, candidate)
			}
		}
		if len(valid) == 0 {
			continue
		}
		seedCandidates[seed] = valid
		orderedSeeds = append(orderedSeeds, seed)
	}

	for round := 0; ; round++ {
		added := false
		for _, seed := range orderedSeeds {
			candidates := seedCandidates[seed]
			if round >= len(candidates) {
				continue
			}
			addCandidate(candidates[round], seed)
			added = true
			if len(out) >= limit {
				return out, nil
			}
		}
		if !added {
			break
		}
	}

	return out, nil
}

func rankTestAnchorsForQuery(query string, anchors []codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	out := append([]codeanchor.IntelAnchor(nil), anchors...)
	frame := queryframe.Extract(query)
	sort.SliceStable(out, func(i, j int) bool {
		left := queryframe.ScoreFields(frame, queryframe.Fields{Path: out[i].Path, Title: out[i].Symbol, Symbol: out[i].Symbol, FQN: out[i].FQN}).Value
		right := queryframe.ScoreFields(frame, queryframe.Fields{Path: out[j].Path, Title: out[j].Symbol, Symbol: out[j].Symbol, FQN: out[j].FQN}).Value
		if left != right {
			return left > right
		}
		return strings.Join([]string{out[i].Path, out[i].FQN, out[i].AnchorID}, "\x00") < strings.Join([]string{out[j].Path, out[j].FQN, out[j].AnchorID}, "\x00")
	})
	return out
}

// directRelationshipTargetIsUnambiguous prevents a package-local Go method
// fallback from turning one syntactic call into proof for every same-named
// receiver method. Exact persisted edges remain useful when the method name has
// one target in its package.
func (r *TestsForCodeRetriever) directRelationshipTargetIsUnambiguous(ctx context.Context, target codeanchor.IntelAnchor) bool {
	if target.Lang != codeanchor.LangGo || !strings.EqualFold(target.Kind, "method") {
		return true
	}
	parts := strings.Split(strings.TrimSpace(target.FQN), ".")
	if len(parts) < 3 || strings.TrimSpace(target.Symbol) == "" {
		return false
	}
	pkg := strings.Join(parts[:len(parts)-2], ".")
	matches, err := r.Store.IntelAnchorIDsByGoMethodNameInPkg(ctx, pkg, target.Symbol, 2)
	if err != nil {
		return false
	}
	return len(matches) == 1 && matches[0] == target.AnchorID
}

func testPathCandidates(seed string) []string {
	seed = filepath.ToSlash(strings.TrimSpace(seed))
	if seed == "" {
		return nil
	}
	dir := filepath.Dir(seed)
	base := strings.TrimSuffix(filepath.Base(seed), filepath.Ext(seed))
	ext := filepath.Ext(seed)
	if base == "" || ext == "" {
		return nil
	}

	candidates := []string{
		filepath.ToSlash(filepath.Join(dir, base+"_test"+ext)),
		filepath.ToSlash(filepath.Join(dir, "test_"+base+ext)),
		filepath.ToSlash(filepath.Join(dir, base+".test"+ext)),
		filepath.ToSlash(filepath.Join(dir, base+".spec"+ext)),
		filepath.ToSlash(filepath.Join(dir, base+"_spec"+ext)),
		filepath.ToSlash(filepath.Join(dir, "__tests__", base+ext)),
		filepath.ToSlash(filepath.Join(dir, "__tests__", base+".test"+ext)),
		filepath.ToSlash(filepath.Join(dir, "__tests__", base+".spec"+ext)),
	}
	return dedupeStrings(candidates)
}

func goPackageTestCandidates(vaultPath, seed string) []string {
	if strings.ToLower(filepath.Ext(seed)) != ".go" || strings.TrimSpace(vaultPath) == "" {
		return nil
	}
	absSeed := filepath.Join(vaultPath, filepath.FromSlash(seed))
	info, err := os.Stat(absSeed)
	if err != nil || info.IsDir() {
		return nil
	}
	symbols := exportedGoSymbols(absSeed)
	if len(symbols) == 0 {
		return nil
	}
	dir := filepath.Dir(absSeed)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	relDir := filepath.ToSlash(filepath.Dir(seed))
	hits := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		absTest := filepath.Join(dir, entry.Name())
		body, err := os.ReadFile(absTest)
		if err != nil {
			continue
		}
		if goTestReferencesAnySymbol(body, symbols) {
			hits = append(hits, filepath.ToSlash(filepath.Join(relDir, entry.Name())))
		}
	}
	sort.Strings(hits)
	return hits
}

func exportedGoSymbols(path string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	symbols := make([]string, 0)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil && d.Name.IsExported() {
				symbols = append(symbols, d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name != nil && s.Name.IsExported() {
						symbols = append(symbols, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if name != nil && name.IsExported() {
							symbols = append(symbols, name.Name)
						}
					}
				}
			}
		}
	}
	return dedupeStrings(symbols)
}

func goTestReferencesAnySymbol(body []byte, symbols []string) bool {
	text := string(body)
	for _, symbol := range symbols {
		if strings.Contains(text, symbol) || strings.Contains(text, "Test"+symbol) {
			return true
		}
	}
	return false
}

func testEvidenceType(path, seed string) string {
	if strings.ToLower(filepath.Ext(seed)) == ".go" &&
		filepath.Dir(filepath.ToSlash(path)) == filepath.Dir(filepath.ToSlash(seed)) &&
		filepath.Base(path) != strings.TrimSuffix(filepath.Base(seed), filepath.Ext(seed))+"_test.go" {
		return "tests_package"
	}
	return "tests_path"
}

func testPathExists(ctx context.Context, vaultPath string, store *semdb.Store, rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return false
	}
	if strings.TrimSpace(vaultPath) != "" {
		abs := filepath.Join(vaultPath, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err == nil && !info.IsDir() {
			return true
		}
	}
	if store == nil {
		return false
	}
	paths, err := store.FilesByPathPrefix(ctx, rel, 1)
	if err != nil || len(paths) == 0 {
		return false
	}
	return paths[0] == rel
}
