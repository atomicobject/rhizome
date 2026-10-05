package obsidian

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectBacklinksFromGraphPreservesWikilinkPreferenceAndFragments(t *testing.T) {
	snapshot := &GraphSnapshot{
		Nodes: map[string]GraphSnapshotNode{
			"target.md": {Path: "target.md"},
			"source.md": {Path: "source.md", Tags: []string{"keep"}},
		},
		Edges: []GraphSnapshotEdge{
			{Source: "source.md", Target: "target.md", LinkType: "mdlink", Subtype: BacklinkTypeBasic},
			{Source: "source.md", Target: "target.md", LinkType: "wikilink", Subtype: BacklinkTypeAlias},
			{Source: "source.md", Target: "target.md", LinkType: "wikilink", Subtype: BacklinkTypeHeading, Fragment: "details"},
		},
	}
	snapshot.Normalize()

	require.Equal(t, []Backlink{
		{Referrer: "source.md", LinkType: BacklinkTypeAlias},
		{Referrer: "source.md", LinkType: BacklinkTypeHeading, Fragment: "details"},
	}, CollectBacklinksFromGraph(snapshot, []string{"target.md"}, nil)["target.md"])
}

func TestCollectBacklinksFromGraphRespectsIndexedTags(t *testing.T) {
	snapshot := &GraphSnapshot{
		Nodes: map[string]GraphSnapshotNode{
			"target.md": {Path: "target.md"},
			"hidden.md": {Path: "hidden.md", Tags: []string{"no-prompt"}},
		},
		Edges: []GraphSnapshotEdge{
			{Source: "hidden.md", Target: "target.md", LinkType: "wikilink", Subtype: BacklinkTypeBasic},
		},
	}
	snapshot.Normalize()

	require.Empty(t, CollectBacklinksFromGraph(snapshot, []string{"target.md"}, []string{"no-prompt"})["target.md"])
}

func TestGraphSnapshotNormalizePreservesMixedCaseAuthoredPath(t *testing.T) {
	snapshot := &GraphSnapshot{
		Nodes: map[string]GraphSnapshotNode{
			"Decision.MD": {Path: "Decision.MD"},
			"source.md":   {Path: "source.md"},
		},
		Edges: []GraphSnapshotEdge{{
			Source:   "source.md",
			Target:   "Decision.MD",
			LinkType: "wikilink",
			Subtype:  BacklinkTypeBasic,
		}},
	}

	snapshot.Normalize()

	require.Contains(t, snapshot.Nodes, "Decision.MD")
	require.NotContains(t, snapshot.Nodes, "Decision.MD.md")
	require.Equal(t, "Decision.MD", snapshot.Edges[0].Target)
}
