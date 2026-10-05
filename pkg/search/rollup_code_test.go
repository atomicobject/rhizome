package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type stubModuleLookup struct {
	idByPath map[string]string
}

func (s stubModuleLookup) ModuleAnchorIDByPath(_ context.Context, path string) (string, bool, error) {
	id, ok := s.idByPath[path]
	return id, ok, nil
}

func (s stubModuleLookup) ModuleAnchorIDsByPaths(_ context.Context, paths []string) (map[string]string, error) {
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		if id, ok := s.idByPath[p]; ok {
			out[p] = id
		}
	}
	return out, nil
}

func TestCodeRollupper_ChoosesModuleWhenSpread(t *testing.T) {
	r := &CodeRollupper{
		Intel:          stubModuleLookup{idByPath: map[string]string{"pkg/foo.go": "m1"}},
		SpreadRatio:    0.85,
		SpreadCount:    3,
		SmallFileBytes: 0,
	}

	results := []RankedResult{
		{Candidate: Candidate{Type: "code", Path: "pkg/foo.go", AnchorID: "a1", Handle: knowledge.CodeChunkHandle("a1", "symbol", 0), Symbol: "BuildSearch", FQN: "example/pkg.BuildSearch"}, FinalScore: 0.90},
		{Candidate: Candidate{Type: "code", Path: "pkg/foo.go", AnchorID: "a2", Handle: knowledge.CodeChunkHandle("a2", "symbol", 0), Symbol: "RankSearch"}, FinalScore: 0.88},
		{Candidate: Candidate{Type: "code", Path: "pkg/foo.go", AnchorID: "a3", Handle: knowledge.CodeChunkHandle("a3", "symbol", 0), Symbol: "PackSearch"}, FinalScore: 0.87},
	}

	out, err := r.Rollup(context.Background(), QuerySpec{Limits: Limits{Total: 25}}, results)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "m1", out[0].AnchorID)
	require.Equal(t, "module", out[0].Kind)
	require.Equal(t, "module", out[0].Granularity)
	require.Equal(t, 0, out[0].ChunkIndex)
	require.Equal(t, "BuildSearch", out[0].Symbol)
	require.Equal(t, "example/pkg.BuildSearch", out[0].FQN)
	require.Equal(t, knowledge.CodeChunkHandle("m1", "module", 0).String(), out[0].Handle.String())
}

func TestCodeRollupper_ChoosesSymbolWhenDominant(t *testing.T) {
	r := &CodeRollupper{
		Intel:       stubModuleLookup{idByPath: map[string]string{"pkg/foo.go": "m1"}},
		SpreadRatio: 0.85,
		SpreadCount: 3,
	}

	results := []RankedResult{
		{Candidate: Candidate{Type: "code", Path: "pkg/foo.go", AnchorID: "a1", Handle: knowledge.CodeChunkHandle("a1", "symbol", 0)}, FinalScore: 0.90},
		{Candidate: Candidate{Type: "code", Path: "pkg/foo.go", AnchorID: "a2", Handle: knowledge.CodeChunkHandle("a2", "symbol", 0)}, FinalScore: 0.50},
	}

	out, err := r.Rollup(context.Background(), QuerySpec{Limits: Limits{Total: 25}}, results)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "a1", out[0].AnchorID)
}

func TestCodeRollupper_KeepsExactSymbolHitInSmallFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg/foo.go"), []byte("package pkg\n\nfunc Needle() {}\n"), 0o644))
	r := &CodeRollupper{Intel: stubModuleLookup{idByPath: map[string]string{"pkg/foo.go": "m1"}}, Root: root}

	for _, evidenceType := range []string{"symbol_exact", "symbol_match", "definition_anchor"} {
		t.Run(evidenceType, func(t *testing.T) {
			results := []RankedResult{
				{Candidate: Candidate{Type: "code", Path: "pkg/foo.go", AnchorID: "a1", Symbol: "Needle", Handle: knowledge.CodeChunkHandle("a1", "symbol", 0), Evidence: []Evidence{{Type: evidenceType, RawScore: 0.9}}}, FinalScore: 0.90},
			}

			out, err := r.Rollup(context.Background(), QuerySpec{Limits: Limits{Total: 25}}, results)
			require.NoError(t, err)
			require.Equal(t, results, out)
		})
	}
}
