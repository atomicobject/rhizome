package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	indexedBootstrapItemLimit  = 40
	indexedBootstrapByteBudget = 12_000
)

// buildIndexedBootstrapContext deliberately composes the bounded filesystem
// bootstrap with an already-open store. WHY: SPEC-0082.US10-US11 assign all
// discovery and repair to `rzm index`; this consumer must never widen a failed
// durable read into a live crawl.
func buildIndexedBootstrapContext(vault obsidian.VaultManager, params VaultContextTextParams, budget int) (VaultContextTextResult, error) {
	ctx := params.Context
	if ctx == nil {
		ctx = context.Background()
	}
	enrichmentBudget := min(indexedBootstrapByteBudget, max(1200, budget/3))
	minimalBudget := max(1200, budget-enrichmentBudget)
	minimal, err := buildMinimalBootstrapContext(vault, params, minimalBudget)
	if err != nil {
		return VaultContextTextResult{}, err
	}
	minimal = strings.Replace(minimal, fmt.Sprintf("- budgetChars: %d", minimalBudget), fmt.Sprintf("- budgetChars: %d", budget), 1)
	result := VaultContextTextResult{
		Text:          minimal,
		IndexedStatus: IndexedContextMissing,
	}
	if params.SessionStore == nil {
		unavailable := IndexedContextFreshness{
			State: IndexedContextMissing, WarningCode: "indexed-context-missing", Remediation: "rzm index",
		}
		if params.IndexedUnavailable != nil {
			unavailable = *params.IndexedUnavailable
		}
		result.Warnings = append(result.Warnings, indexedContextWarning(
			unavailable,
			"indexed enrichment is unavailable because the unified index could not be opened",
		))
		result.IndexedStatus = unavailable.State
		result.Text = appendIndexedBootstrap(minimal, "", result.Warnings, budget)
		return result, nil
	}

	inputs, err := resolveContextInputs(vault, params.Profile)
	if err != nil {
		return VaultContextTextResult{}, err
	}
	expectedScopeHash := ""
	if inputs.LocalCfg != nil && inputs.LocalCfg.cfg != nil {
		expectedScopeHash = inputs.LocalCfg.cfg.ScopeConfigHash()
	}
	snapshot, err := params.SessionStore.CodeIndexPrerequisiteSnapshot(ctx)
	result.IndexedReads++
	indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
	if err != nil {
		result.Warnings = append(result.Warnings, indexedContextWarning(
			IndexedContextFreshness{State: IndexedContextMissing, WarningCode: "indexed-context-missing", Remediation: "rzm index"},
			fmt.Sprintf("indexed enrichment metadata is unavailable: %v", err),
		))
		result.Text = appendIndexedBootstrap(minimal, "", result.Warnings, budget)
		return result, nil
	}

	evidence := IndexedContextFreshnessEvidence{
		SchemaCompatible:  true,
		HasIndexerVersion: snapshot.IndexerVersionPresent,
		IndexerVersion:    snapshot.IndexerVersion,
		ExpectedVersion:   codeanchor.IndexerVersion,
		HasScopeHash:      snapshot.ScopeConfigHashPresent,
		ScopeHash:         snapshot.ScopeConfigHash,
		ExpectedScopeHash: expectedScopeHash,
		RequireOntology:   params.RequireOntology,
	}
	var ontologySummary *codeanchor.IndexedOntologySummary
	if params.RequireOntology {
		summary, summaryErr := params.SessionStore.IndexedOntologySummary(ctx, 30, 3)
		result.IndexedReads++
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
		if summaryErr == nil {
			ontologySummary = &summary
			evidence.HasOntologyState = summary.Available
			evidence.OntologyReady = summary.Ready
			evidence.OntologyVersion = summary.MaterializationVersion
			evidence.ExpectedOntologyVersion = ontology.OntologyMaterializationVersion
		}
	}
	freshness := EvaluateIndexedContextFreshness(evidence)
	result.IndexedStatus = freshness.State
	if freshness.State != IndexedContextAvailable {
		result.Warnings = append(result.Warnings, indexedContextWarning(freshness, "indexed enrichment is unavailable or does not match the current index contract"))
		result.Text = appendIndexedBootstrap(minimal, "", result.Warnings, budget)
		return result, nil
	}

	links := make([]codeanchor.IndexedCodeNoteLink, 0)
	rationales := make([]codeanchor.IndexedRationale, 0)
	edges := make([]codeanchor.IndexedCodeEdge, 0)
	notes := make([]codeanchor.IndexedNoteMetadata, 0)
	var graphSummary *codeanchor.IndexedGraphSummary
	if params.GraphSummary {
		summary, summaryErr := params.SessionStore.IndexedGraphSummary(ctx, 4)
		result.IndexedReads++
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
		if summaryErr != nil {
			result.IndexedStatus = IndexedContextStale
			warningCode := "indexed-context-graph-summary-read-failed"
			message := summaryErr.Error()
			if errors.Is(summaryErr, codeanchor.ErrIndexedGraphSummaryMissing) {
				warningCode = "indexed-context-graph-summary-missing"
				message = "indexed graph summary is unavailable because the index contains no graph score evidence"
			}
			result.Warnings = append(result.Warnings, IndexedContextWarning{
				Code: warningCode, Message: message, Remediation: "rzm index",
			})
		} else {
			graphSummary = &summary
		}
	}
	targets := append(append([]string(nil), params.Files...), params.ContextFiles...)
	if len(targets) > 8 {
		targets = targets[:8]
		result.Warnings = append(result.Warnings, IndexedContextWarning{
			Code:    "indexed-context-target-limit",
			Message: "indexed enrichment is limited to the first 8 explicit targets",
		})
	}
	for _, rawTarget := range targets {
		target, kind, resolveErr := indexedBootstrapTarget(inputs, rawTarget)
		if resolveErr != nil {
			result.Warnings = append(result.Warnings, IndexedContextWarning{
				Code: "indexed-context-target-invalid", Message: resolveErr.Error(),
			})
			continue
		}
		classification := configuredPathKind{Owner: notediscovery.Unowned}
		if kind != IndexedContextTargetDirectory {
			definition := inputs.VaultDef
			if definition.BasePath() == "" {
				definition.Path = inputs.VaultPath
			}
			absTarget, absErr := paths.NewVaultPaths(inputs.VaultPath)
			if absErr != nil {
				result.Warnings = append(result.Warnings, IndexedContextWarning{Code: "indexed-context-target-invalid", Message: absErr.Error()})
				continue
			}
			absolute, absErr := absTarget.Abs(paths.RelPath(target))
			if absErr != nil {
				result.Warnings = append(result.Warnings, IndexedContextWarning{Code: "indexed-context-target-invalid", Message: absErr.Error()})
				continue
			}
			classification, absErr = classifyConfiguredPath(definition, absolute.String(), params.NoteMetadata)
			if absErr != nil {
				result.Warnings = append(result.Warnings, IndexedContextWarning{Code: "indexed-context-target-invalid", Message: absErr.Error()})
				continue
			}
			if classification.Owner == notediscovery.Note && !classification.Projectable {
				result.Warnings = append(result.Warnings, IndexedContextWarning{
					Code: "indexed-context-note-projection-unsupported", Message: unsupportedProjectionMessage(target, classification.Provider, "indexed context"),
				})
				continue
			}
			if classification.Owner != notediscovery.Note && classification.Owner != notediscovery.Code {
				result.Warnings = append(result.Warnings, IndexedContextWarning{
					Code: "indexed-context-target-unowned", Message: fmt.Sprintf("selected target %q is not a configured note or code file", target),
				})
				continue
			}
		}
		var found []codeanchor.IndexedCodeNoteLink
		var foundRationale []codeanchor.IndexedRationale
		var foundEdges []codeanchor.IndexedCodeEdge
		if classification.Owner == notediscovery.Note {
			note, noteErr := params.SessionStore.IndexedNoteMetadataForPath(ctx, target)
			result.IndexedReads++
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
			if noteErr != nil {
				result.IndexedStatus = IndexedContextStale
				result.Warnings = append(result.Warnings, IndexedContextWarning{
					Code: "indexed-context-note-read-failed", Message: noteErr.Error(), Remediation: "rzm index",
				})
			} else if note != nil {
				notes = append(notes, *note)
			} else {
				result.Warnings = append(result.Warnings, IndexedContextWarning{
					Code:        "indexed-context-note-missing",
					Message:     fmt.Sprintf("selected note %q is not present in the durable note index", target),
					Remediation: "rzm index",
				})
			}
		}
		if kind == IndexedContextTargetDirectory || classification.Owner == notediscovery.Note {
			found, err = params.SessionStore.IndexedCodeNoteLinksForSubtree(ctx, target, indexedBootstrapItemLimit)
			result.IndexedReads++
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
			if err == nil {
				foundRationale, err = params.SessionStore.IndexedRationaleForSubtree(ctx, target, indexedBootstrapItemLimit)
				result.IndexedReads++
				indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
			}
			if err == nil {
				foundEdges, err = params.SessionStore.IndexedCodeEdgesForSubtree(ctx, target, indexedBootstrapItemLimit)
				result.IndexedReads++
				indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
			}
		} else {
			found, err = params.SessionStore.IndexedCodeNoteLinksForFile(ctx, target, indexedBootstrapItemLimit)
			result.IndexedReads++
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
			if err == nil {
				foundRationale, err = params.SessionStore.IndexedRationaleForFile(ctx, target, indexedBootstrapItemLimit)
				result.IndexedReads++
				indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
			}
			if err == nil {
				foundEdges, err = params.SessionStore.IndexedCodeEdgesForFile(ctx, target, indexedBootstrapItemLimit)
				result.IndexedReads++
				indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedReads, 1)
			}
		}
		if err != nil {
			result.IndexedStatus = IndexedContextStale
			result.Warnings = append(result.Warnings, IndexedContextWarning{
				Code: "indexed-context-read-failed", Message: err.Error(), Remediation: "rzm index",
			})
			continue
		}
		links = append(links, found...)
		rationales = append(rationales, foundRationale...)
		edges = append(edges, foundEdges...)
	}
	links = rankIndexedContextLinks(links, params.Intent, 12)
	rationales = rankIndexedRationale(rationales, params.Intent, 8)
	edges = rankIndexedEdges(edges, params.Intent, 8)
	notes = dedupeIndexedNotes(notes, 8)
	graphResults := 0
	if graphSummary != nil {
		graphResults = len(graphSummary.Communities)
	}
	result.IndexedResults = len(links) + len(rationales) + len(edges) + len(notes) + graphResults
	indexingperf.AddCount(ctx, indexingperf.AgentStartOpIndexedResults, int64(result.IndexedResults))
	rendered := renderIndexedBootstrap(links, rationales, edges, notes, graphSummary, ontologySummary, enrichmentBudget)
	result.Text = appendIndexedBootstrap(minimal, rendered, result.Warnings, budget)
	return result, nil
}

func indexedBootstrapTarget(inputs resolvedContextInputs, raw string) (string, IndexedContextTargetKind, error) {
	display := strings.TrimSpace(raw)
	if display == "" {
		return "", "", fmt.Errorf("indexed bootstrap target is empty")
	}
	abs := display
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(inputs.ProjectRoot, abs)
	}
	abs = paths.ResolveSymlinks(abs).String()
	if abs == "" {
		abs = filepath.Clean(filepath.Join(inputs.ProjectRoot, display))
	}
	if _, err := inputs.ProjectRootPaths.RelStrict(abs); err != nil {
		return "", "", fmt.Errorf("indexed bootstrap target %q: %w", display, err)
	}
	vaultRootPaths, err := paths.NewVaultPaths(inputs.VaultPath)
	if err != nil {
		return "", "", fmt.Errorf("indexed bootstrap vault root: %w", err)
	}
	indexedRel, err := vaultRootPaths.RelStrict(abs)
	if err != nil {
		return "", "", fmt.Errorf("indexed bootstrap target %q is outside the indexed vault: %w", display, err)
	}
	kind := IndexedContextTargetFile
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		kind = IndexedContextTargetDirectory
	}
	return indexedRel.String(), kind, nil
}

type scoredIndexedLink struct {
	link  codeanchor.IndexedCodeNoteLink
	score int
}

func rankIndexedContextLinks(links []codeanchor.IndexedCodeNoteLink, intent string, limit int) []codeanchor.IndexedCodeNoteLink {
	terms := indexedIntentTerms(intent)
	seen := make(map[string]struct{}, len(links))
	scored := make([]scoredIndexedLink, 0, len(links))
	for _, link := range links {
		key := link.CodePath + "\x00" + link.NotePath + "\x00" + link.Label + "\x00" + link.Snippet
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		haystack := strings.ToLower(strings.Join([]string{link.CodePath, link.NotePath, link.Label, link.Snippet}, " "))
		score := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				score++
			}
		}
		scored = append(scored, scoredIndexedLink{link: link, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		a, b := scored[i].link, scored[j].link
		if a.NotePath != b.NotePath {
			return a.NotePath < b.NotePath
		}
		if a.CodePath != b.CodePath {
			return a.CodePath < b.CodePath
		}
		if a.Label != b.Label {
			return a.Label < b.Label
		}
		return a.Snippet < b.Snippet
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]codeanchor.IndexedCodeNoteLink, len(scored))
	for i := range scored {
		out[i] = scored[i].link
	}
	return out
}

func indexedIntentTerms(intent string) []string {
	fields := strings.FieldsFunc(strings.ToLower(intent), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

func rankIndexedRationale(items []codeanchor.IndexedRationale, intent string, limit int) []codeanchor.IndexedRationale {
	terms := indexedIntentTerms(intent)
	sort.SliceStable(items, func(i, j int) bool {
		aScore := indexedTextScore(strings.Join([]string{items[i].CodePath, items[i].SymbolFQN, string(items[i].Kind), items[i].Content}, " "), terms)
		bScore := indexedTextScore(strings.Join([]string{items[j].CodePath, items[j].SymbolFQN, string(items[j].Kind), items[j].Content}, " "), terms)
		if aScore != bScore {
			return aScore > bScore
		}
		if items[i].CodePath != items[j].CodePath {
			return items[i].CodePath < items[j].CodePath
		}
		if items[i].StartLine != items[j].StartLine {
			return items[i].StartLine < items[j].StartLine
		}
		return items[i].ID < items[j].ID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func rankIndexedEdges(items []codeanchor.IndexedCodeEdge, intent string, limit int) []codeanchor.IndexedCodeEdge {
	terms := indexedIntentTerms(intent)
	sort.SliceStable(items, func(i, j int) bool {
		aScore := indexedTextScore(strings.Join([]string{items[i].SourcePath, items[i].TargetPath, items[i].Kind}, " "), terms)
		bScore := indexedTextScore(strings.Join([]string{items[j].SourcePath, items[j].TargetPath, items[j].Kind}, " "), terms)
		if aScore != bScore {
			return aScore > bScore
		}
		if items[i].SourcePath != items[j].SourcePath {
			return items[i].SourcePath < items[j].SourcePath
		}
		if items[i].TargetPath != items[j].TargetPath {
			return items[i].TargetPath < items[j].TargetPath
		}
		return items[i].Kind < items[j].Kind
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func indexedTextScore(text string, terms []string) int {
	text = strings.ToLower(text)
	score := 0
	for _, term := range terms {
		if strings.Contains(text, term) {
			score++
		}
	}
	return score
}

func dedupeIndexedNotes(notes []codeanchor.IndexedNoteMetadata, limit int) []codeanchor.IndexedNoteMetadata {
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	out := notes[:0]
	for _, note := range notes {
		if len(out) > 0 && out[len(out)-1].Path == note.Path {
			continue
		}
		out = append(out, note)
		if len(out) == limit {
			break
		}
	}
	return out
}

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
