package actions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semstore "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

// stubIntel implements CodeRelatednessIntel for tests.
type stubIntel struct {
	symbolsByFile map[string][]string
	anchors       []codeanchor.Anchor
	anchorScores  map[string]float64
	docScores     map[string]semstore.GraphDocScore
}

func (s *stubIntel) CallsFromFile(ctx context.Context, file string) ([]codeanchor.CallSite, error) {
	return nil, nil
}

func (s *stubIntel) SymbolsByFile(ctx context.Context, file string) ([]string, error) {
	return s.symbolsByFile[file], nil
}

func (s *stubIntel) Ancestors(ctx context.Context, fqn string) ([]string, error) {
	return nil, nil
}

func (s *stubIntel) AnchorsBySymbols(ctx context.Context, symbols []string) (map[int64][]string, error) {
	out := make(map[int64][]string)
	for _, a := range s.anchors {
		if a.BaseSym == nil {
			continue
		}
		for _, sym := range symbols {
			fqn := codeanchor.Symbol{Pkg: a.BaseSym.Pkg, Name: a.BaseSym.Name}.NormalizeFQN()
			if sym == fqn {
				out[a.ID] = []string{sym}
			}
		}
	}
	return out, nil
}

func (s *stubIntel) AnchorsByCallFiles(ctx context.Context, files []string) ([]int64, error) {
	return nil, nil
}

func (s *stubIntel) AnchorsByPathPrefix(ctx context.Context, target string) ([]int64, error) {
	return nil, nil
}

func (s *stubIntel) AnchorsByGlobMatch(ctx context.Context, target string) ([]int64, error) {
	return nil, nil
}

func (s *stubIntel) Anchors(ctx context.Context) ([]codeanchor.Anchor, error) { return s.anchors, nil }

func (s *stubIntel) NotesForAnchor(ctx context.Context, anchorID int64) ([]codeanchor.Note, error) {
	return nil, nil
}

func (s *stubIntel) IndexedFilePaths(ctx context.Context) ([]string, error) {
	var out []string
	for f := range s.symbolsByFile {
		out = append(out, f)
	}
	return out, nil
}

func (s *stubIntel) GraphDocScoresByPaths(ctx context.Context, paths []string) (map[string]semstore.GraphDocScore, error) {
	out := make(map[string]semstore.GraphDocScore)
	for _, p := range paths {
		if sc, ok := s.docScores[p]; ok {
			out[p] = sc
		}
	}
	return out, nil
}

func (s *stubIntel) AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error) {
	out := make(map[string]float64)
	for _, id := range anchorIDs {
		if sc, ok := s.anchorScores[id]; ok {
			out[id] = sc
		}
	}
	return out, nil
}

func TestNormalizePkgName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "slash-separated path unchanged",
			input:    "backend/src/charm/domain/accounts",
			expected: "backend/src/charm/domain/accounts",
		},
		{
			name:     "dot-separated Python module converted to slashes",
			input:    "backend.src.charm.domain.accounts.model",
			expected: "backend/src/charm/domain/accounts/model",
		},
		{
			name:     "leading dot-slash removed",
			input:    "./backend/src/charm",
			expected: "backend/src/charm",
		},
		{
			name:     "leading slash removed",
			input:    "/backend/src/charm",
			expected: "backend/src/charm",
		},
		{
			name:     "whitespace trimmed",
			input:    "  backend.src.charm  ",
			expected: "backend/src/charm",
		},
		{
			name:     "mixed dots and slashes normalized",
			input:    "backend/src.charm.domain",
			expected: "backend/src/charm/domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePkgName(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchInternalPkg(t *testing.T) {
	// Set up internal packages discovered from directory structure (slash-separated)
	aliasToCanonical := map[string]string{
		"backend/src/charm/domain/accounts": "backend/src/charm/domain/accounts",
		"backend/src/charm/domain/auth":     "backend/src/charm/domain/auth",
		"backend/src/charm/domain/message":  "backend/src/charm/domain/message",
		// Aliases from stripCommonRoots
		"charm/domain/accounts": "backend/src/charm/domain/accounts",
		"charm/domain/auth":     "backend/src/charm/domain/auth",
		"charm/domain/message":  "backend/src/charm/domain/message",
	}

	tests := []struct {
		name        string
		calleePkg   string // After normalizePkgName (dots → slashes)
		expectMatch bool
		expectCanon string
	}{
		{
			name:        "exact match",
			calleePkg:   "backend/src/charm/domain/accounts",
			expectMatch: true,
			expectCanon: "backend/src/charm/domain/accounts",
		},
		{
			name:        "Python callee with model suffix matches ancestor",
			calleePkg:   "backend/src/charm/domain/accounts/model", // from backend.src.charm.domain.accounts.model
			expectMatch: true,
			expectCanon: "backend/src/charm/domain/accounts",
		},
		{
			name:        "Python callee with deep path matches ancestor",
			calleePkg:   "backend/src/charm/domain/message/wire/v1", // from backend.src.charm.domain.message.wire.v1
			expectMatch: true,
			expectCanon: "backend/src/charm/domain/message",
		},
		{
			name:        "alias match",
			calleePkg:   "charm/domain/accounts",
			expectMatch: true,
			expectCanon: "backend/src/charm/domain/accounts",
		},
		{
			name:        "no match for external package",
			calleePkg:   "external/lib/foo",
			expectMatch: false,
			expectCanon: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canon, ok := matchInternalPkg(tt.calleePkg, aliasToCanonical)
			require.Equal(t, tt.expectMatch, ok, "match status mismatch")
			if tt.expectMatch {
				require.Equal(t, tt.expectCanon, canon, "canonical package mismatch")
			}
		})
	}
}

func TestCodeRelatedness_UsesRelativePathsAndGraphSignals(t *testing.T) {
	ctx := context.Background()
	vault := t.TempDir()

	relFile := filepath.ToSlash(filepath.Join("backend", "svc", "app.py"))
	absFile := filepath.Join(vault, filepath.FromSlash(relFile))
	require.NoError(t, os.MkdirAll(filepath.Dir(absFile), 0o755))
	require.NoError(t, os.WriteFile(absFile, []byte("print('hi')\n"), 0o644))

	intel := &stubIntel{
		symbolsByFile: map[string][]string{
			relFile: {"backend.svc.app:Handler"},
		},
		anchors: []codeanchor.Anchor{{
			ID:    1,
			Lang:  codeanchor.LangPy,
			Label: "Handler",
			BaseSym: &codeanchor.SymbolRef{
				Lang: codeanchor.LangPy,
				Pkg:  "backend.svc",
				Name: "app:Handler",
			},
		}},
		anchorScores: map[string]float64{"1": 0.8},
		docScores: map[string]semstore.GraphDocScore{
			relFile: {DocPath: relFile, DocType: "code", Authority: 0.4, Hub: 0.2},
		},
	}
	if resolved := paths.ResolveSymlinks(absFile); resolved != "" && resolved.String() != absFile {
		intel.docScores[resolved.String()] = semstore.GraphDocScore{DocPath: resolved.String(), DocType: "code", Authority: 0.4, Hub: 0.2}
	}

	report, err := CodeRelatedness(ctx, intel, CodeRelatednessOptions{
		VaultPath: vault,
		Roots:     []string{filepath.Join(vault, "backend")},
	})
	require.NoError(t, err)
	require.Len(t, report.Packages, 1)

	pkg := report.Packages[0]
	require.Contains(t, pkg.Langs, string(codeanchor.LangPy))
	require.Equal(t, 1, pkg.AnchorLabels)
	require.Greater(t, pkg.GraphAuthority, 0.0)
	require.Greater(t, pkg.AnchorPageRank, 0.0)
	require.True(t, pkg.GraphSignalHint > 0)
}
