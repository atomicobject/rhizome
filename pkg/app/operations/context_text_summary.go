package operations

import (
	"fmt"
	"sort"
	"strings"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func renderHeader(toolName, vaultName, vaultPath, profile string, budget int, bodyLen int, trimmed bool) string {
	used := bodyLen
	// Include a conservative join overhead for header separator.
	used += 200
	if used > budget {
		used = budget
	}
	trimStr := "false"
	if trimmed {
		trimStr = "true"
	}
	var b strings.Builder
	b.WriteString("# Rhizome context\n\n")
	b.WriteString(fmt.Sprintf("- tool: %s\n", toolName))
	if vaultName != "" {
		b.WriteString(fmt.Sprintf("- vault: %s\n", vaultName))
	}
	if vaultPath != "" {
		b.WriteString(fmt.Sprintf("- vaultPath: %s\n", vaultPath))
	}
	if profile != "" {
		b.WriteString(fmt.Sprintf("- profile: %s\n", profile))
	}
	b.WriteString(fmt.Sprintf("- budgetChars: %d\n", budget))
	b.WriteString(fmt.Sprintf("- trimmed: %s\n", trimStr))
	return strings.TrimSpace(b.String())
}

func renderVaultStatsBlock(analysis *vaultgraph.GraphAnalysis) string {
	if analysis == nil {
		return "## Stats\n\nError: graph analysis unavailable"
	}
	return fmt.Sprintf("## Stats\n\n- notes: %d\n- links: %d\n- orphans: %d", analysis.Stats.NodeCount, analysis.Stats.EdgeCount, len(analysis.Orphans))
}

func renderOntologyStatsBlock(summary *OntologyVaultSummary) string {
	if summary == nil {
		return "## Stats\n\nError: ontology summary unavailable"
	}
	return fmt.Sprintf(
		"## Stats\n\n- notes: %d\n- typed: %d\n- untyped: %d\n- ontologyTypes: %d\n- ontologyIssues: %d",
		summary.TotalNotes,
		summary.TypedNotes,
		summary.UntypedNotes,
		len(summary.TypeCounts),
		summary.IssueCount,
	)
}

func renderOntologyTypeCountsBlock(summary *OntologyVaultSummary, limit int) string {
	if summary == nil || len(summary.TypeCounts) == 0 {
		return ""
	}
	counts := append([]OntologyTypeCount(nil), summary.TypeCounts...)
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count == counts[j].Count {
			return counts[i].TypeName < counts[j].TypeName
		}
		return counts[i].Count > counts[j].Count
	})
	if limit > 0 && len(counts) > limit {
		counts = counts[:limit]
	}

	var b strings.Builder
	b.WriteString("## Ontology types\n")
	for _, entry := range counts {
		b.WriteString(fmt.Sprintf("\n- %s (%d)", entry.TypeName, entry.Count))
		if len(entry.Examples) > 0 {
			b.WriteString(fmt.Sprintf(": %s", strings.Join(entry.Examples, ", ")))
		}
	}
	return b.String()
}

func renderOntologyIssuesBlock(summary *OntologyVaultSummary, limit int) string {
	if summary == nil || len(summary.Issues) == 0 {
		return ""
	}
	issues := append([]ontology.ValidationIssue(nil), summary.Issues...)
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		if issues[i].NotePath != issues[j].NotePath {
			return issues[i].NotePath < issues[j].NotePath
		}
		return issues[i].FieldName < issues[j].FieldName
	})
	total := len(issues)
	if limit > 0 && len(issues) > limit {
		issues = issues[:limit]
	}

	var b strings.Builder
	b.WriteString("## Ontology issues\n")
	for _, issue := range issues {
		location := issue.NotePath
		if strings.TrimSpace(location) == "" {
			location = issue.TypeName
		}
		if strings.TrimSpace(location) == "" {
			location = "<schema>"
		}
		b.WriteString(fmt.Sprintf("\n- [%s] %s", issue.Code, location))
		if issue.FieldName != "" {
			b.WriteString(fmt.Sprintf(" field=%s", issue.FieldName))
		}
		if issue.Line > 0 {
			b.WriteString(fmt.Sprintf(" line=%d", issue.Line))
		}
		if issue.Message != "" {
			b.WriteString(fmt.Sprintf(": %s", issue.Message))
		}
	}
	if total > len(issues) {
		b.WriteString(fmt.Sprintf("\n- ... %d more", total-len(issues)))
	}
	return b.String()
}

func renderTopList(title string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n")
	for _, it := range items {
		b.WriteString("\n- ")
		b.WriteString(it)
	}
	return b.String()
}

func topAuthorityPaths(nodes map[string]vaultgraph.GraphNode, limit int) []string {
	if len(nodes) == 0 || limit == 0 {
		return nil
	}
	type pr struct {
		path string
		val  float64
	}
	list := make([]pr, 0, len(nodes))
	for p, n := range nodes {
		list = append(list, pr{path: p, val: n.Authority})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].val == list[j].val {
			return list[i].path < list[j].path
		}
		return list[i].val > list[j].val
	})
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.path)
	}
	return out
}

func renderKeyNotes(noteMgr obsidian.NoteReader, notePaths []string) string {
	if len(notePaths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Key notes\n")
	for _, p := range notePaths {
		title := paths.NoteTitle(p)
		summary := blessedSummary(noteMgr, p)
		b.WriteString("\n- ")
		b.WriteString(title)
		b.WriteString(" (")
		b.WriteString(p)
		b.WriteString(")")
		if summary != "" {
			b.WriteString(" — ")
			b.WriteString(summary)
		}
	}
	return b.String()
}

func blessedSummary(noteMgr obsidian.NoteReader, notePath string) string {
	if noteMgr == nil || notePath == "" {
		return ""
	}
	fact, ok := NoteFactsFromReader(noteMgr).LookupFact(notePath)
	if !ok || len(fact.Frontmatter) == 0 {
		return ""
	}
	blessed := frontmatter.FilterBlessed(fact.Frontmatter)
	if len(blessed) == 0 {
		return ""
	}
	for _, k := range []string{"summary", "synopsis", "about", "description", "desc", "doc"} {
		if v, ok := blessed[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}
