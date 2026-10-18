package operations

import (
	"fmt"
	"strings"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
)

func renderIndexedBootstrap(links []codeanchor.IndexedCodeNoteLink, rationales []codeanchor.IndexedRationale, edges []codeanchor.IndexedCodeEdge, notes []codeanchor.IndexedNoteMetadata, graphSummary *codeanchor.IndexedGraphSummary, ontologySummary *codeanchor.IndexedOntologySummary, budget int) string {
	var body strings.Builder
	if len(notes) > 0 {
		body.WriteString("## Indexed selected notes\n\n")
		for _, note := range notes {
			fmt.Fprintf(&body, "- `%s`", note.Path)
			if note.Title != "" {
				fmt.Fprintf(&body, ": %s", note.Title)
			}
			fmt.Fprintf(&body, " (%d bytes)\n", note.Size)
		}
	}
	if graphSummary != nil {
		if body.Len() > 0 {
			body.WriteByte('\n')
		}
		fmt.Fprintf(&body, "## Indexed graph summary\n\nDocuments: %d; notes: %d; orphans: %d.\n", graphSummary.DocumentCount, graphSummary.NoteCount, graphSummary.OrphanCount)
		for _, community := range graphSummary.Communities {
			fmt.Fprintf(&body, "- %s: %d documents, %d notes", community.ID, community.DocumentCount, community.NoteCount)
			if community.TopPath != "" {
				fmt.Fprintf(&body, " (top: `%s`)", community.TopPath)
			}
			body.WriteByte('\n')
		}
	}
	if ontologySummary != nil {
		if body.Len() > 0 {
			body.WriteByte('\n')
		}
		fmt.Fprintf(&body, "## Indexed ontology summary\n\nNotes: %d total, %d typed, %d untyped.\n", ontologySummary.TotalNotes, ontologySummary.TypedNotes, ontologySummary.UntypedNotes)
		for _, count := range ontologySummary.TypeCounts {
			fmt.Fprintf(&body, "- %s: %d", count.TypeName, count.Count)
			if len(count.Examples) > 0 {
				fmt.Fprintf(&body, " (%s)", strings.Join(count.Examples, ", "))
			}
			body.WriteByte('\n')
		}
	}
	if len(links) > 0 {
		if body.Len() > 0 {
			body.WriteByte('\n')
		}
		body.WriteString("## Indexed code-note relationships\n\n")
		for _, link := range links {
			fmt.Fprintf(&body, "- `%s` -> `%s`", link.CodePath, link.NotePath)
			if link.Label != "" {
				fmt.Fprintf(&body, " [%s]", link.Label)
			}
			if link.Snippet != "" {
				fmt.Fprintf(&body, ": %s", strings.TrimSpace(link.Snippet))
			}
			body.WriteByte('\n')
		}
	}
	if len(rationales) > 0 {
		body.WriteString("\n## Indexed rationale\n\n")
		for _, rationale := range rationales {
			fmt.Fprintf(&body, "- `%s:%d`", rationale.CodePath, rationale.StartLine)
			if rationale.SymbolFQN != "" {
				fmt.Fprintf(&body, " `%s`", rationale.SymbolFQN)
			}
			if rationale.Kind != "" {
				fmt.Fprintf(&body, " [%s]", rationale.Kind)
			}
			fmt.Fprintf(&body, ": %s\n", strings.TrimSpace(rationale.Content))
		}
	}
	if len(edges) > 0 {
		body.WriteString("\n## Indexed code relationships\n\n")
		for _, edge := range edges {
			fmt.Fprintf(&body, "- `%s` -[%s x%d]-> `%s`\n", edge.SourcePath, edge.Kind, edge.Weight, edge.TargetPath)
		}
	}
	return strings.TrimSpace(trimIndexedBytes(body.String(), budget))
}

func indexedContextWarning(freshness IndexedContextFreshness, message string) IndexedContextWarning {
	return IndexedContextWarning{Code: freshness.WarningCode, Message: message, Remediation: freshness.Remediation}
}

func appendIndexedBootstrap(minimal, rendered string, warnings []IndexedContextWarning, budget int) string {
	text, _ := appendIndexedBootstrapDetailed(minimal, rendered, warnings, budget)
	return text
}

// appendIndexedBootstrapDetailed also reports how much of the original minimal
// packet survived warning assembly, excluding any added truncation indicator.
func appendIndexedBootstrapDetailed(minimal, rendered string, warnings []IndexedContextWarning, budget int) (string, int) {
	minimal = strings.TrimSpace(minimal)
	var out strings.Builder
	out.WriteString(minimal)
	if rendered != "" {
		out.WriteString("\n\n")
		out.WriteString(strings.TrimSpace(rendered))
	}
	if len(warnings) > 0 {
		out.WriteString("\n\n## Indexed enrichment warnings\n\n")
		for _, warning := range warnings {
			fmt.Fprintf(&out, "- %s: %s", warning.Code, warning.Message)
			if warning.Remediation != "" {
				fmt.Fprintf(&out, " (run `%s`)", warning.Remediation)
			}
			out.WriteByte('\n')
		}
	}
	text, retained := trimIndexedBytesDetailed(strings.TrimSpace(out.String()), budget)
	return text, min(retained, len(minimal))
}

func trimIndexedBytes(text string, budget int) string {
	trimmed, _ := trimIndexedBytesDetailed(text, budget)
	return trimmed
}

func trimIndexedBytesDetailed(text string, budget int) (string, int) {
	trimmed, retained := contextpack.TrimToBudgetDetailed(text, budget)
	for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed, min(retained, len(trimmed))
}
