package actions

import (
	"context"
	"path/filepath"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search/graphalg"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// HealthParams contains parameters for comprehensive vault health analysis.
type HealthParams struct {
	StaleDays     int      // Staleness threshold in days (default 90)
	Include       []string // Metrics to include (brokenLinks, staleNotes, deadEnds, suggestedMerges)
	IncludeImages bool     // Include image links in broken link detection (default false)
	obsidian.WikilinkOptions
	GraphOptions          obsidian.GraphAnalysisOptions
	SessionStore          *semdb.Store
	NoteMetadata          notemeta.Indexer
	MetadataStoreFallback MetadataStoreFallbackPolicy
}

// DefaultHealthParams provides standard options for health analysis.
var DefaultHealthParams = HealthParams{
	StaleDays:       90,
	Include:         nil, // nil means include all
	WikilinkOptions: obsidian.DefaultWikilinkOptions,
}

// BrokenLinks returns all wikilinks pointing to non-existent notes.
// By default excludes image links unless options.IncludeImages is true.
func BrokenLinks(vault obsidian.VaultManager, note obsidian.NoteReader, options obsidian.BrokenLinksOptions) ([]obsidian.BrokenLink, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return nil, err
	}

	return obsidian.FindBrokenLinks(vaultDef, note, options)
}

// DeadEnds returns notes with inbound links but no outbound links.
func DeadEnds(vault obsidian.VaultManager, note obsidian.NoteReader, params GraphAnalysisParams) ([]obsidian.DeadEndNote, error) {
	analysis, err := GraphAnalysis(vault, note, params)
	if err != nil {
		return nil, err
	}

	return obsidian.FindDeadEnds(analysis), nil
}

// StaleNotes returns notes not modified within the threshold days.
func StaleNotes(vault obsidian.VaultManager, note obsidian.NoteReader, thresholdDays int) ([]obsidian.StaleNote, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return nil, err
	}

	return obsidian.FindStaleNotes(vaultDef, note, thresholdDays)
}

// VaultHealth returns a comprehensive health report for the vault.
func VaultHealth(vault obsidian.VaultManager, note obsidian.NoteReader, params HealthParams) (*obsidian.HealthReport, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return nil, err
	}

	vaultName, err := vault.DefaultName()
	if err != nil {
		return nil, err
	}

	// Get list of all notes for stats and merge suggestions
	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}

	report := &obsidian.HealthReport{
		Vault:      vaultName,
		VaultPath:  filepath.ToSlash(vaultDef.BasePath()),
		AnalyzedAt: time.Now(),
		Stats: obsidian.HealthStats{
			TotalNotes: len(allNotes),
		},
	}

	// Check which metrics to include
	includeSet := make(map[string]bool)
	if len(params.Include) == 0 {
		// Include all by default
		includeSet["brokenLinks"] = true
		includeSet["staleNotes"] = true
		includeSet["deadEnds"] = true
		includeSet["suggestedMerges"] = true
	} else {
		for _, inc := range params.Include {
			includeSet[inc] = true
		}
	}
	includeSurprises := includeSet["surprises"]

	// Collect broken links
	if includeSet["brokenLinks"] {
		brokenLinksOpts := obsidian.BrokenLinksOptions{
			WikilinkOptions: params.WikilinkOptions,
			IncludeImages:   params.IncludeImages,
		}
		brokenLinks, err := obsidian.FindBrokenLinks(vaultDef, note, brokenLinksOpts)
		if err != nil {
			return nil, err
		}
		report.BrokenLinks = brokenLinks
		report.Stats.BrokenLinkCount = len(brokenLinks)
	}

	// Collect stale notes
	if includeSet["staleNotes"] {
		staleNotes, err := obsidian.FindStaleNotes(vaultDef, note, params.StaleDays)
		if err != nil {
			return nil, err
		}
		report.StaleNotes = staleNotes
		report.Stats.StaleNoteCount = len(staleNotes)
	}

	// Collect dead ends
	if includeSet["deadEnds"] {
		graphParams := GraphAnalysisParams{
			Options:               params.GraphOptions,
			SessionStore:          params.SessionStore,
			NoteMetadata:          params.NoteMetadata,
			MetadataStoreFallback: params.MetadataStoreFallback,
		}
		analysis, err := GraphAnalysis(vault, note, graphParams)
		if err != nil {
			return nil, err
		}
		deadEnds := obsidian.FindDeadEnds(analysis)
		report.DeadEnds = deadEnds
		report.Stats.DeadEndCount = len(deadEnds)
	}

	// Collect merge suggestions
	if includeSet["suggestedMerges"] {
		suggestions := obsidian.FindMergeSuggestions(allNotes)
		report.SuggestedMerges = suggestions
		report.Stats.MergeSuggestionCount = len(suggestions)
	}

	// Collect surprising connections (requires intel store with graph_doc_edges + graph_doc_scores).
	if includeSurprises && params.SessionStore != nil {
		if conns, err := loadSurprisingConnections(context.Background(), params.SessionStore, 10, 2.0); err == nil {
			report.SurprisingConnections = conns
			report.Stats.SurprisingConnectionCount = len(conns)
		}
		// Ignore errors — graph index may not be populated yet.
	}

	var (
		ontologyRuntime *ontology.Runtime
		cleanup         func()
	)
	if params.SessionStore != nil && !params.MetadataStoreFallback.allowsStoreOpen() {
		if state, stateErr := params.SessionStore.GetOntologySchemaState(context.Background()); stateErr == nil {
			ontologyRuntime = &ontology.Runtime{Issues: ontology.ValidationIssuesFromJSON(state.ErrorJSON)}
		}
	} else if params.SessionStore != nil && params.NoteMetadata.Validate() == nil {
		ontologyRuntime, err = ontology.EnsureRuntimeWithStore(context.Background(), params.NoteMetadata, vaultDef, note, params.SessionStore)
	} else if params.MetadataStoreFallback.allowsStoreOpen() && params.NoteMetadata.Validate() == nil {
		ontologyRuntime, cleanup, err = ontology.EnsureRuntime(context.Background(), params.NoteMetadata, vaultDef, note)
		if cleanup != nil {
			defer cleanup()
		}
	}
	if ontologyRuntime != nil {
		report.OntologyIssues = make([]obsidian.OntologyIssue, 0, len(ontologyRuntime.Issues))
		for _, issue := range ontologyRuntime.Issues {
			report.OntologyIssues = append(report.OntologyIssues, obsidian.OntologyIssue{
				Code:       issue.Code,
				NotePath:   issue.NotePath,
				TypeName:   issue.TypeName,
				FieldName:  issue.FieldName,
				Line:       issue.Line,
				LinkKind:   issue.LinkKind,
				LinkTarget: issue.LinkTarget,
				Message:    issue.Message,
			})
		}
		report.Stats.OntologyIssueCount = len(report.OntologyIssues)
	}
	if err != nil {
		// Ontology errors should be reported through the health payload, not fail the whole report.
		return report, nil
	}

	return report, nil
}

// loadSurprisingConnections queries the intel store for graph edges and scores,
// runs the surprise scoring algorithm, and returns health-report entries.
func loadSurprisingConnections(ctx context.Context, store *semdb.Store, topN int, minScore float64) ([]obsidian.SurprisingConnectionEntry, error) {
	facts, err := noderead.NewService(obsidian.VaultDefinition{}, &obsidian.Note{}, store, nil).
		NewScope(ctx, noderead.ScopeOptions{}).
		GraphFacts(ctx, noderead.GraphFactsRequest{
			NodeLimit:       1_000_000,
			EdgeLimit:       1_000_000,
			IncludeOntology: true,
			IncludeDocLinks: true,
			IncludeCode:     true,
			IncludeEmbedded: true,
		})
	if err != nil {
		return nil, err
	}

	docScores, err := store.GraphDocScores(ctx)
	if err != nil {
		return nil, err
	}

	edges := make([]graphalg.GraphDocEdgeForSurprise, 0, len(facts.Edges))
	for _, e := range facts.Edges {
		src := e.SourcePath
		dst := e.TargetPath
		if src == "" {
			src = e.SourceRef.NotePath
		}
		if dst == "" {
			dst = e.TargetRef.NotePath
		}
		if src == "" || dst == "" || src == dst {
			continue
		}
		edges = append(edges, graphalg.GraphDocEdgeForSurprise{
			SrcPath:         src,
			DstPath:         dst,
			Kind:            healthGraphFactEdgeKind(e),
			Confidence:      healthGraphFactConfidenceLabel(e),
			ConfidenceScore: e.Confidence,
		})
	}

	scores := make(map[string]graphalg.GraphDocScoreMinimal, len(docScores))
	for _, sc := range docScores {
		scores[sc.DocPath] = graphalg.GraphDocScoreMinimal{
			DocPath:   sc.DocPath,
			DocType:   sc.DocType,
			Community: sc.Community,
			Inbound:   sc.Inbound,
			Outbound:  sc.Outbound,
			Authority: sc.Authority,
		}
	}

	conns := graphalg.FindSurprisingConnections(edges, scores, topN, minScore)

	out := make([]obsidian.SurprisingConnectionEntry, 0, len(conns))
	for _, c := range conns {
		out = append(out, obsidian.SurprisingConnectionEntry{
			SrcPath:    c.SrcPath,
			DstPath:    c.DstPath,
			EdgeKind:   c.EdgeKind,
			Score:      c.Score,
			Confidence: c.Confidence,
			Reasons:    c.Reasons,
		})
	}
	return out, nil
}

func healthGraphFactEdgeKind(edge noderead.GraphFactEdge) string {
	if edge.RelationName != "" && edge.Kind == "ontology" {
		return edge.RelationName
	}
	return edge.Kind
}

func healthGraphFactConfidenceLabel(edge noderead.GraphFactEdge) string {
	if edge.Confidence > 0 && edge.Confidence < 1 {
		return semdb.EdgeConfidenceInferred
	}
	return semdb.EdgeConfidenceExtracted
}
