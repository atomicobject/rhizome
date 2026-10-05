package obsidian

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeGraphStatsAndOrphans(t *testing.T) {
	vaultPath := filepath.Join("..", "..", "..", "mocks", "vaults", "graph")
	vaultDef := VaultDefinition{Name: "graph", Path: vaultPath}
	stats, err := ComputeGraphStats(vaultDef, &Note{}, DefaultWikilinkOptions)
	require.NoError(t, err)

	expected := map[string]NodeStats{
		"alpha.md":    {Inbound: 1, Outbound: 1},
		"beta.md":     {Inbound: 2, Outbound: 1},
		"gamma.md":    {Inbound: 0, Outbound: 1},
		"orphan.md":   {Inbound: 0, Outbound: 0},
		"selflink.md": {Inbound: 0, Outbound: 0},
	}

	assert.Equal(t, expected, stats.Nodes)

	expectedComponents := [][]string{
		{"alpha.md", "beta.md"},
		{"gamma.md"},
		{"orphan.md"},
		{"selflink.md"},
	}
	assert.Equal(t, expectedComponents, stats.Components)

	assert.Equal(t, []string{"orphan.md", "selflink.md"}, stats.Orphans())
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte("[[target#details]]\n[[other]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.md"), []byte("# Details\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "other.md"), []byte("# Other\n"), 0o644))
	anchorDef := VaultDefinition{Name: "anchors", Path: root}
	all, err := ComputeGraphStats(anchorDef, &Note{}, DefaultWikilinkOptions)
	require.NoError(t, err)
	skipped, err := ComputeGraphStats(anchorDef, &Note{}, WikilinkOptions{SkipAnchors: true})
	require.NoError(t, err)
	assert.Equal(t, 2, all.Nodes["source.md"].Outbound)
	assert.Equal(t, 1, all.Nodes["target.md"].Inbound)
	assert.Equal(t, 1, all.Nodes["other.md"].Inbound)
	assert.Equal(t, 1, skipped.Nodes["source.md"].Outbound)
	assert.Equal(t, 0, skipped.Nodes["target.md"].Inbound)
	assert.Equal(t, 1, skipped.Nodes["other.md"].Inbound)
	snapshot, err := BuildGraphSnapshot(anchorDef, &Note{}, GraphAnalysisOptions{})
	require.NoError(t, err)
	fromSnapshot := ComputeGraphStatsFromSnapshot(snapshot, WikilinkOptions{SkipAnchors: true})
	assert.Equal(t, 1, fromSnapshot.Nodes["source.md"].Outbound)
	assert.Equal(t, 0, fromSnapshot.Nodes["target.md"].Inbound)
	assert.Equal(t, 1, fromSnapshot.Nodes["other.md"].Inbound)
}

func TestCommunitiesLabelPropagation(t *testing.T) {
	vaultPath := filepath.Join("..", "..", "..", "mocks", "vaults", "graph")
	vaultDef := VaultDefinition{Name: "graph", Path: vaultPath}
	stats, err := ComputeGraphStats(vaultDef, &Note{}, DefaultWikilinkOptions)
	require.NoError(t, err)

	communities := stats.Communities()

	var cluster []string
	for _, c := range communities {
		if len(c.Nodes) >= 3 {
			cluster = c.Nodes
			break
		}
	}

	assert.ElementsMatch(t, []string{"alpha.md", "beta.md", "gamma.md"}, cluster)
}

func TestGraphAnalysisRespectsExcludes(t *testing.T) {
	vaultPath := filepath.Join("..", "..", "..", "mocks", "vaults", "graph")
	vaultDef := VaultDefinition{Name: "graph", Path: vaultPath}
	analysis, err := ComputeGraphAnalysis(vaultDef, &Note{}, GraphAnalysisOptions{
		WikilinkOptions: DefaultWikilinkOptions,
		ExcludedPaths: map[string]struct{}{
			"gamma.md": {},
		},
		RecencyCascade:    true,
		RecencyCascadeSet: true,
	})
	require.NoError(t, err)

	_, hasGamma := analysis.Nodes["gamma.md"]
	assert.False(t, hasGamma)
}

func TestTopAuthorityNodes(t *testing.T) {
	nodes := map[string]GraphNode{
		"a.md": {Authority: 0.9, Hub: 0.2},
		"b.md": {Authority: 0.7, Hub: 0.5},
		"c.md": {Authority: 0.7, Hub: 0.1},
		"d.md": {Authority: 0.4, Hub: 0.9},
	}
	members := []string{"a.md", "b.md", "c.md", "d.md"}

	top := topAuthorityNodes(members, nodes, 3)

	require.Len(t, top, 3)
	assert.Equal(t, AuthorityScore{Path: "a.md", Authority: 0.9, Hub: 0.2}, top[0])
	// Tie on authority breaks by path
	assert.Equal(t, AuthorityScore{Path: "b.md", Authority: 0.7, Hub: 0.5}, top[1])
	assert.Equal(t, AuthorityScore{Path: "c.md", Authority: 0.7, Hub: 0.1}, top[2])
}

func TestCommunityRecency(t *testing.T) {
	now := time.Now()
	modTimes := map[string]time.Time{
		"a.md": now.Add(-48 * time.Hour),      // 2 days ago
		"b.md": now.Add(-10 * 24 * time.Hour), // 10 days ago
		"c.md": now.Add(-40 * 24 * time.Hour), // 40 days ago (outside 30d window)
	}
	members := []string{"a.md", "b.md", "c.md"}

	recency := communityRecency(members, modTimes, 30)

	require.NotNil(t, recency)
	assert.Equal(t, "a.md", recency.LatestPath)
	assert.InDelta(t, 2.0, recency.LatestAgeDays, 0.3)
	assert.Equal(t, 2, recency.RecentCount) // a and b are within 30d
	assert.Equal(t, 30, recency.WindowDays)
}

func TestResolveContentTimePrefersFrontmatter(t *testing.T) {
	content := `---
date: 2025-05-01
event_date: 2025-06-01
---
Body`
	ts, ok := ResolveContentTime("Notes/sample.md", content)
	require.True(t, ok)
	assert.Equal(t, time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), ts.UTC())
}

func TestResolveContentTimeFromFilename(t *testing.T) {
	ts, ok := ResolveContentTime("Log/2024-12-31 Planning.md", "Body")
	require.True(t, ok)
	assert.Equal(t, time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC), ts.UTC())
}

func TestResolveContentTimeFromHeading(t *testing.T) {
	content := "# 2024-11-25\nSome text"
	ts, ok := ResolveContentTime("Notes/heading.md", content)
	require.True(t, ok)
	assert.Equal(t, time.Date(2024, 11, 25, 0, 0, 0, 0, time.UTC), ts.UTC())
}

func TestApplyNeighborRecencyBoostsUndated(t *testing.T) {
	now := time.Date(2025, time.January, 15, 12, 0, 0, 0, time.UTC)
	adjacency := map[string]map[string]struct{}{
		"moc.md":    {"recent.md": {}},
		"recent.md": {},
	}
	base := map[string]time.Time{
		"recent.md": now.Add(-24 * time.Hour),
	}

	effective := applyNeighborRecency(adjacency, base, now, true)

	require.Contains(t, effective, "recent.md")
	assert.WithinDuration(t, now.Add(-24*time.Hour), effective["recent.md"], time.Second)

	moc, ok := effective["moc.md"]
	require.True(t, ok)
	ageDays := now.Sub(moc).Hours() / 24.0
	assert.InDelta(t, 8.0, ageDays, 0.5)
}

func TestApplyNeighborRecencyCascadesTwoHops(t *testing.T) {
	now := time.Date(2025, time.January, 15, 12, 0, 0, 0, time.UTC)
	adjacency := map[string]map[string]struct{}{
		"top-moc.md":   {"mid-moc.md": {}},
		"mid-moc.md":   {"leaf.md": {}},
		"leaf.md":      {},
		"unrelated.md": {},
	}
	base := map[string]time.Time{
		"leaf.md": now.Add(-24 * time.Hour),
	}

	effective := applyNeighborRecency(adjacency, base, now, true)

	require.Contains(t, effective, "leaf.md")
	top, ok := effective["top-moc.md"]
	require.True(t, ok)

	// Two hops means we see two staleness offsets (one per hop) applied.
	ageDays := now.Sub(top).Hours() / 24.0
	assert.InDelta(t, 15.0, ageDays, 0.6) // two 7-day hops plus the leaf's 1-day age
	assert.NotContains(t, effective, "unrelated.md")
}

func TestComputeGraphAnalysisEmptyVault(t *testing.T) {
	vaultDir := t.TempDir()

	analysis, err := ComputeGraphAnalysis(VaultDefinition{Name: "empty", Path: vaultDir}, &Note{}, GraphAnalysisOptions{
		WikilinkOptions: DefaultWikilinkOptions,
	})
	require.NoError(t, err)
	assert.Empty(t, analysis.Nodes)
	assert.Empty(t, analysis.EffectiveTimes)
}

func TestAuthorityStatsAndBucketsAdaptive(t *testing.T) {
	nodes := map[string]GraphNode{
		"a.md": {Authority: 1.0},
		"b.md": {Authority: 0.9},
		"c.md": {Authority: 0.8},
		"d.md": {Authority: 0.7},
		"e.md": {Authority: 0.6},
		"f.md": {Authority: 0.5},
		"g.md": {Authority: 0.4},
		"h.md": {Authority: 0.3},
		"i.md": {Authority: 0.2},
		"j.md": {Authority: 0.1},
	}
	members := []string{"a.md", "b.md", "c.md", "d.md", "e.md", "f.md", "g.md", "h.md", "i.md", "j.md"}

	buckets, stats := authorityBuckets(members, nodes)

	require.NotNil(t, stats)
	require.Len(t, buckets, 5)
	total := 0
	for _, bucket := range buckets {
		total += bucket.Count
		assert.Contains(t, members, bucket.Example)
	}
	assert.Equal(t, len(members), total)
	for i, want := range []struct{ high, low float64 }{
		{1.0, 0.9}, {0.8, 0.7}, {0.6, 0.5}, {0.4, 0.3}, {0.2, 0.1},
	} {
		assert.InDelta(t, want.high, buckets[i].High, 1e-9)
		assert.InDelta(t, want.low, buckets[i].Low, 1e-9)
	}
	assert.InDelta(t, 0.55, stats.Mean, 1e-9)
	assert.InDelta(t, 0.5, stats.P50, 1e-9)
	assert.InDelta(t, 1.0, stats.P95, 1e-9)
	assert.InDelta(t, 1.0, stats.Max, 1e-9)
}
