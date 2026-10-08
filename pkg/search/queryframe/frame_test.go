package queryframe

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestExtractExpandsMorphologicalVariantsWithoutDomainHints(t *testing.T) {
	frame := Extract("how are notes embedded?")

	require.Equal(t, ActionExplain, frame.Action)
	require.Contains(t, frame.Terms, "note")
	require.Contains(t, frame.Terms, "notes")
	require.Contains(t, frame.Terms, "embed")
	require.Contains(t, frame.Terms, "embedded")
	require.Contains(t, frame.Terms, "embedding")
	require.Contains(t, frame.Terms, "embeddings")
	require.NotContains(t, frame.Terms, "noteembed")
	require.NotContains(t, frame.Terms, "provider")
	require.NotContains(t, frame.Terms, "vector")
	require.Equal(t, [][]string{{"note", "notes"}, {"embed", "embedded", "embedding", "embeddings"}}, frame.SupportTermGroups)
}

func TestSupportTermGroupsDeduplicateVariantsAndRetainTopicalRequestWords(t *testing.T) {
	frame := Extract("embedding embed tests documentation implementation callers definition")

	require.Equal(t, [][]string{
		{"embed", "embedded", "embedding", "embeddings"},
		{"test", "tests"},
		{"documentation"},
		{"implementation"},
		{"caller", "callers"},
		{"definition"},
	}, frame.SupportTermGroups)
}

func TestSupportTermGroupsMergePartiallyOverlappingMorphology(t *testing.T) {
	frame := Extract("retry retries policy")

	require.Equal(t, [][]string{{"retries", "retry"}, {"policy"}}, frame.SupportTermGroups)
	retryOnly := EnrichCandidate(frame, search.Candidate{Path: "src/retry.go"})
	require.Less(t, SupportSpecificityScore(retryOnly.Evidence), .7, "one retry variant cannot satisfy two obligations")

	embedding := Extract("embedding embedder policy")
	require.Equal(t, [][]string{{"embed", "embedded", "embedder", "embedding", "embeddings"}, {"policy"}}, embedding.SupportTermGroups)
	embeddingOnly := EnrichCandidate(embedding, search.Candidate{Path: "src/embedding.go"})
	require.Less(t, SupportSpecificityScore(embeddingOnly.Evidence), .7, "one embedding variant cannot satisfy two obligations")
}

func TestExtractSplitsCodeIdentifiers(t *testing.T) {
	frame := Extract("PlanNoteEmbeddings note_syncer.go")

	require.Contains(t, frame.Terms, "plan")
	require.Contains(t, frame.Terms, "note")
	require.Contains(t, frame.Terms, "embeddings")
	require.Contains(t, frame.Terms, "syncer")
}

func TestExtractPreservesLiteralAndUnicodeInformation(t *testing.T) {
	query := `"Café Δelta" HTTPServer note_syncer pkg/search.go`
	frame := Extract(query)

	require.Equal(t, query, frame.Query)
	require.Contains(t, frame.Terms, "café")
	require.Contains(t, frame.Terms, "δelta")
	require.Contains(t, frame.Terms, "httpserver")
	require.Contains(t, frame.Terms, "note")
	require.Contains(t, frame.Terms, "syncer")
	require.Contains(t, frame.Terms, "pkg")
	require.Contains(t, frame.Terms, "search")
}

func TestExtractDoesNotTreatSpecAsTestRequest(t *testing.T) {
	frame := Extract("spec-driven ontology")

	require.False(t, frame.MentionsTests)
}

func TestExtractDetectsTestRequests(t *testing.T) {
	for _, query := range []string{
		"tests for ontology",
		"ontology coverage",
		"ontology regression",
	} {
		t.Run(query, func(t *testing.T) {
			frame := Extract(query)
			require.True(t, frame.MentionsTests)
		})
	}
}

func TestScoreCandidatePrefersSpecificCodeSurface(t *testing.T) {
	frame := Extract("how are notes embedded?")
	score := ScoreCandidate(frame, search.Candidate{
		Path:   "pkg/search/semantic/note_syncer.go",
		Symbol: "PlanNoteEmbeddings",
	})

	require.Greater(t, score.Value, 0.5)
	require.Contains(t, score.Matched, "note")
	require.Contains(t, score.Matched, "embed")
}

func TestEnrichCandidateReplacesQuerySpecificityIdempotently(t *testing.T) {
	candidate := search.Candidate{Path: "pkg/search/service.go", Evidence: []search.Evidence{{Type: "query_specificity", RawScore: 0.1, Source: "retriever"}}}
	first := EnrichCandidate(Extract("search service"), candidate)
	second := EnrichCandidate(Extract("search service"), first)
	require.Equal(t, first.Evidence, second.Evidence)
	count := 0
	for _, evidence := range second.Evidence {
		if evidence.Type == "query_specificity" {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestEnrichCandidateRecordsIdentityAndSourceExcerptMatchesSeparately(t *testing.T) {
	candidate := search.Candidate{
		Path: "specs/route-assignment.md", Title: "Conflict review",
		Evidence: []search.Evidence{{Type: "intel_doc_match", RawScore: .9, Source: "pkg/anchors/sqlite", Details: map[string]string{
			"snippet": "A stale assignment version creates a review item.",
		}}},
	}

	enriched := EnrichCandidate(Extract("what action did the review assign to Maya"), candidate)

	require.Len(t, enriched.Evidence, 2)
	details := enriched.Evidence[1].Details
	require.Equal(t, "true", details["content_available"])
	require.Contains(t, details["identity_matched"], "review")
	require.Contains(t, details["content_matched"], "review")
	require.NotContains(t, details["content_matched"], "maya")
}

func TestEnrichCandidateDoesNotTreatPathOnlySnippetAsSourceContent(t *testing.T) {
	candidate := search.Candidate{Path: "src/SyncClient.php", Evidence: []search.Evidence{{
		Type: "intel_fts_match", RawScore: .8, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "src/SyncClient.php", "pathOnly": "true"},
	}}}

	enriched := EnrichCandidate(Extract("where is SyncClient defined"), candidate)

	require.Equal(t, "false", enriched.Evidence[1].Details["content_available"])
	require.Empty(t, enriched.Evidence[1].Details["content_matched"])
}

func TestEnrichCandidateRequiresExactTermsFromSourceOwnedEvidence(t *testing.T) {
	candidate := search.Candidate{Path: "src/worker.go", Evidence: []search.Evidence{
		{Type: "intel_fts_match", RawScore: .8, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "Mayan queue policy"}},
		{Type: "code_ref", RawScore: 1, Details: map[string]string{"snippet": "Maya is linked from another source"}},
	}}

	enriched := EnrichCandidate(Extract("assigned to Maya"), candidate)

	details := enriched.Evidence[len(enriched.Evidence)-1].Details
	require.Equal(t, "true", details["content_available"])
	require.NotContains(t, details["content_matched"], "maya")
}

func TestEnrichCandidateSeparatesRankingSnippetFromAnswerSupport(t *testing.T) {
	query := Extract("retry queue policy")
	linkOnly := EnrichCandidate(query, search.Candidate{Path: "src/unrelated.go", Evidence: []search.Evidence{{
		Type: "code_ref", RawScore: 1, Source: "doc_links", Details: map[string]string{"snippet": "retry queue policy"},
	}}})
	owned := EnrichCandidate(query, search.Candidate{Path: "src/unrelated.go", Evidence: []search.Evidence{{
		Type: "intel_fts_match", RawScore: 1, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "retry queue policy"},
	}}})

	require.Greater(t, SpecificityScore(linkOnly.Evidence), 0.7, "link text may remain useful to retrieval ranking")
	require.Zero(t, SupportSpecificityScore(linkOnly.Evidence))
	require.Greater(t, SupportSpecificityScore(owned.Evidence), 0.7)
}

func TestSupportSpecificityRequiresDistinctExactTerms(t *testing.T) {
	substrings := EnrichCandidate(Extract("cat dog fox"), search.Candidate{Path: "notes/unrelated.md", Evidence: []search.Evidence{{
		Type: "intel_doc_match", RawScore: 1, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "concatenate dogmatic firefox"},
	}}})
	repeated := EnrichCandidate(Extract("retry queue policy"), search.Candidate{Path: "retry.go", Title: "Retry", Symbol: "Retry"})

	require.Zero(t, SupportSpecificityScore(substrings.Evidence))
	require.Less(t, SupportSpecificityScore(repeated.Evidence), 0.7)
}

func TestSupportSpecificityNormalizesByOriginalMeaningfulGroups(t *testing.T) {
	shortIdentity := EnrichCandidate(Extract("retry"), search.Candidate{Path: "src/retry.go"})
	shortContent := EnrichCandidate(Extract("retry"), search.Candidate{Path: "src/worker.go", Evidence: []search.Evidence{{
		Type: "intel_fts_match", RawScore: 1, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "retry"},
	}}})
	longDistractor := EnrichCandidate(Extract("retry queue policy mobile owner escalation"), search.Candidate{Path: "src/worker.go", Evidence: []search.Evidence{{
		Type: "intel_fts_match", RawScore: 1, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "retry queue policy"},
	}}})

	require.GreaterOrEqual(t, SupportSpecificityScore(shortIdentity.Evidence), .7)
	require.Less(t, SupportSpecificityScore(shortContent.Evidence), .7, "one generic body term remains useful")
	require.Less(t, SupportSpecificityScore(longDistractor.Evidence), .7, "three terms cannot saturate an arbitrarily longer request")
}

// One query concept repeated across path, title, breadcrumb, and heading
// must not reach the score of a source that matches every concept. Before
// each concept was capped at its share, "Drivers" saturated like the note
// titled with the whole phrase (dogfood query "Innovation teams").
func TestScoreFieldsCapsEachConceptAtItsShare(t *testing.T) {
	frame := Extract("volunteer drivers")
	oneConcept := ScoreFields(frame, Fields{
		Path:       "Notes/Drivers.md",
		Title:      "Drivers",
		Breadcrumb: "Notes/Drivers.md > Drivers",
		Heading:    "Drivers",
	})
	everyConcept := ScoreFields(frame, Fields{
		Path:  "Projects/Opportunity - Volunteer drivers and rural routes.md",
		Title: "Opportunity - Volunteer drivers and rural routes",
	})
	require.Equal(t, 1.0, everyConcept.RankValue)
	require.Equal(t, 0.5, oneConcept.RankValue, "one of two concepts earns half; no multi-concept bonus")
	require.Equal(t, 1.0, oneConcept.Value, "identity strength for answer assembly is unchanged")

	single := ScoreFields(Extract("chunker"), Fields{Path: "pkg/search/chunker.go", Symbol: "Chunker"})
	require.Equal(t, single.Value, single.RankValue, "a one-concept query without variants keeps its scale")
}

// A word's variants ("driver", "drivers") are one concept, so a field that
// contains the word counts once. Otherwise a note that mentions the second
// query word only in body text reaches full specificity.
func TestScoreFieldsCountsAConceptOncePerField(t *testing.T) {
	frame := Extract("volunteer drivers")
	titleAndBody := ScoreFields(frame, Fields{
		Path:    "Notes/Volunteer pressure group pilots new routes.md",
		Title:   "Volunteer pressure group pilots new routes",
		Snippet: "Several of the drivers in the pilot later trained new recruits.",
	})
	require.Less(t, titleAndBody.RankValue, 0.9)
}

// A long question rarely names every concept in one title, and a strong
// identity match on its key word (decorators.py for "decorator") is the
// signal, so the concept cap applies only to short, title-like queries.
func TestScoreFieldsKeepsLongQuestionScale(t *testing.T) {
	frame := Extract("how does the Python instrumentation decorator preserve calls and attach metadata")
	score := ScoreFields(frame, Fields{
		Path:   "packages/observability/decorators.py",
		Symbol: "instrumented",
		FQN:    "observability.decorators.instrumented",
	})
	require.Equal(t, score.Value, score.RankValue)
}

// A short question is not a title: "work" in "how does Container work?" is
// filler, so capping "container" at half its weight let any source that
// mentions both words outrank the Container class.
func TestScoreFieldsKeepsShortQuestionScale(t *testing.T) {
	score := ScoreFields(Extract("how does Container work?"), Fields{
		Path:   "src/container.py",
		Symbol: "Container",
	})
	require.Equal(t, score.Value, score.RankValue)
}

func TestScoreCandidateReadsAgreedLinkLabelsWithTheTitle(t *testing.T) {
	frame := Extract("catalog migration")
	plain := search.Candidate{Path: "Projects/Project Kestrel.md", Title: "Project Kestrel"}
	require.Zero(t, ScoreCandidate(frame, plain).RankValue)

	aliased := plain
	aliased.Evidence = []search.Evidence{{Type: "link_text_match", RawScore: 0.9, Details: map[string]string{search.LinkTextAliasDetail: "catalog migration"}}}
	require.Equal(t, ScoreCandidate(frame, search.Candidate{Path: plain.Path, Title: "catalog migration"}).RankValue, ScoreCandidate(frame, aliased).RankValue)
}
