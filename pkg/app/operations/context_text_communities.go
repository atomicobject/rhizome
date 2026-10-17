package operations

import (
	"fmt"
	"sort"
	"strings"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func renderCommunitiesBlock(noteMgr obsidian.NoteReader, analysis *vaultgraph.GraphAnalysis, defaults VaultContextSummaryDefaults) string {
	if analysis == nil || len(analysis.Communities) == 0 {
		return ""
	}

	// Filter out tiny communities (≤2 notes) and test/fixture communities.
	const minCommunitySize = 3
	filtered := make([]vaultgraph.CommunitySummary, 0, len(analysis.Communities))
	for _, c := range analysis.Communities {
		if len(c.Nodes) < minCommunitySize {
			continue
		}
		// Skip communities whose anchor is in testdata/mocks/fixtures.
		anchorPath := c.Anchor
		if anchorPath == "" && len(c.TopAuthority) > 0 {
			anchorPath = c.TopAuthority[0].Path
		}
		if anchorPath != "" && isTestOrFixturePath(anchorPath) {
			continue
		}
		filtered = append(filtered, c)
	}
	if len(filtered) == 0 {
		return ""
	}

	maxComms := defaults.MaxCommunities
	if maxComms <= 0 || maxComms > len(filtered) {
		maxComms = len(filtered)
	}

	var b strings.Builder
	b.WriteString("## Communities\n")

	for i := 0; i < maxComms; i++ {
		c := filtered[i]
		b.WriteString("\n\n### ")
		if c.Anchor != "" {
			b.WriteString(c.Anchor)
		} else {
			b.WriteString(c.ID)
		}
		b.WriteString(fmt.Sprintf(" (%d notes)\n", len(c.Nodes)))

		// Compact one-line stats (density + authority range)
		var stats []string
		if c.Density > 0 {
			stats = append(stats, fmt.Sprintf("density=%.2f", c.Density))
		}
		if c.AuthorityStats != nil && c.AuthorityStats.Max > 0 {
			stats = append(stats, fmt.Sprintf("authority: %.2f–%.2f", c.AuthorityStats.Mean, c.AuthorityStats.Max))
		}
		if c.Recency != nil && c.Recency.RecentCount > 0 {
			stats = append(stats, fmt.Sprintf("recent(30d): %d", c.Recency.RecentCount))
		}
		if len(stats) > 0 {
			b.WriteString("_")
			b.WriteString(strings.Join(stats, " | "))
			b.WriteString("_\n")
		}

		// Top notes (inline, more compact)
		top := c.TopAuthority
		if defaults.CommunityTopNotes > 0 && len(top) > defaults.CommunityTopNotes {
			top = top[:defaults.CommunityTopNotes]
		}
		if len(top) > 0 {
			for _, n := range top {
				title := paths.NoteTitle(n.Path)
				summary := blessedSummary(noteMgr, n.Path)
				b.WriteString("- ")
				b.WriteString(title)
				b.WriteString(" (")
				b.WriteString(n.Path)
				b.WriteString(")")
				if summary != "" {
					b.WriteString(" — ")
					// Truncate long summaries
					if len(summary) > 100 {
						summary = summary[:97] + "…"
					}
					b.WriteString(summary)
				}
				b.WriteString("\n")
			}
		}

		// Top tags (inline, compact)
		if len(c.TopTags) > 0 && defaults.CommunityTopTags != 0 {
			tags := c.TopTags
			if defaults.CommunityTopTags > 0 && len(tags) > defaults.CommunityTopTags {
				tags = tags[:defaults.CommunityTopTags]
			}
			if len(tags) > 0 {
				b.WriteString("Tags: ")
				for idx, t := range tags {
					if idx > 0 {
						b.WriteString(", ")
					}
					b.WriteString(t.Tag)
				}
				b.WriteString("\n")
			}
		}
	}

	return strings.TrimSpace(b.String())
}

func findKeyNotesByPatterns(vault repoconfig.VaultManager, noteMgr obsidian.NoteReader, patterns []string) ([]KeyNoteMatch, error) {
	var out []KeyNoteMatch
	seen := make(map[string]struct{})
	for _, pat := range patterns {
		args := strings.Fields(strings.TrimSpace(pat))
		if len(args) == 0 {
			continue
		}
		inputs, expr, err := ParseInputsWithExpression(args)
		if err != nil {
			return nil, err
		}
		matches, err := ListFiles(vault, noteMgr, ListParams{
			Inputs:      inputs,
			Expression:  expr,
			MaxDepth:    0,
			SkipAnchors: false,
			SkipEmbeds:  false,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			norm := string(paths.EnsureNoteExtension(m, ".md"))
			if _, ok := seen[norm]; ok {
				continue
			}
			seen[norm] = struct{}{}
			out = append(out, KeyNoteMatch{Path: norm, Pattern: pat})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Pattern < out[j].Pattern
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func renderMOCs(patterns []string, mocs []KeyNoteMatch) string {
	var b strings.Builder
	b.WriteString("## Key patterns\n\n")
	for _, p := range patterns {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\n## Matches\n")
	for _, m := range mocs {
		b.WriteString("\n- ")
		b.WriteString(m.Path)
		if m.Pattern != "" {
			b.WriteString(" (")
			b.WriteString(m.Pattern)
			b.WriteString(")")
		}
	}
	return strings.TrimSpace(b.String())
}
