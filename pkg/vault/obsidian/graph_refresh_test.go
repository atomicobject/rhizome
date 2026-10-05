package obsidian

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefreshGraphAnalysisDerivedUpdatesAnchorAndTopAuthority(t *testing.T) {
	analysis := &GraphAnalysis{
		Nodes: map[string]GraphNode{
			"a.md": {
				Path:      "a.md",
				Authority: 0.1,
				Community: "L",
				Neighbors: []string{"b.md"},
			},
			"b.md": {
				Path:      "b.md",
				Authority: 0.2,
				Community: "L",
				Neighbors: nil,
			},
		},
	}

	RefreshGraphAnalysisDerived(analysis)
	require.Len(t, analysis.Communities, 1)
	require.Equal(t, "b.md", analysis.Communities[0].Anchor)
	require.Len(t, analysis.Communities[0].TopAuthority, 2)
	require.Equal(t, "b.md", analysis.Communities[0].TopAuthority[0].Path)

	// Change authority and ensure derived fields reflect the new ordering.
	a := analysis.Nodes["a.md"]
	a.Authority = 0.9
	analysis.Nodes["a.md"] = a

	RefreshGraphAnalysisDerived(analysis)
	require.Len(t, analysis.Communities, 1)
	require.Equal(t, "a.md", analysis.Communities[0].Anchor)
	require.Len(t, analysis.Communities[0].TopAuthority, 2)
	require.Equal(t, "a.md", analysis.Communities[0].TopAuthority[0].Path)
}
