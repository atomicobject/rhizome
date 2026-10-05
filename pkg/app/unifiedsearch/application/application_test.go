package application

import (
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
	"github.com/stretchr/testify/require"
)

func TestAssessRetainsParaphraseOnlySemanticEvidence(t *testing.T) {
	assessed := Assess(search.IntentSearch, []search.RankedResult{{
		Candidate: search.Candidate{
			Handle: knowledge.NoteHandle("docs/pressure.md"),
			Path:   "docs/pressure.md",
			Type:   "note",
			Evidence: []search.Evidence{{
				Type: "note_vector_similarity", RawScore: 0.81, Source: "semantic",
			}},
		},
		FinalScore: 0.95,
	}})

	require.Len(t, assessed, 1)
	require.Equal(t, RelevanceUseful, assessed[0].Relevance)
	packet := BuildAssessedAnswer(search.IntentSearch, "how does excess load get released?", TargetResolution{}, nil, AvailabilityComplete, assessed)
	require.NotEmpty(t, packet.MustRead)
	require.Equal(t, "low", packet.Confidence.Level)
}

func strongQuerySpecificityEvidence() []search.Evidence {
	return []search.Evidence{{Type: "query_specificity", RawScore: 1, Source: "queryframe", Details: map[string]string{"support_score": "1"}}}
}

func TestBuildAssessedAnswerKeepsEmbeddedNodeStrengthByCanonicalIdentity(t *testing.T) {
	nodeA := &ontology.NodeRef{NotePath: "docs/spec.md", NodeID: "story-a", Fragment: "story-a", Kind: ontology.NodeKindEmbedded, Structural: "a"}
	nodeB := &ontology.NodeRef{NotePath: "docs/spec.md", NodeID: "story-b", Fragment: "story-b", Kind: ontology.NodeKindEmbedded, Structural: "b"}
	sources := []SourceAssessment{
		{Result: search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NodeChunkHandle("story-a", "docs/spec.md", "node_body", 0), Path: "docs/spec.md", Type: "note", NodeRef: nodeA}}, Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"}},
		{Result: search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NodeChunkHandle("story-b", "docs/spec.md", "node_body", 0), Path: "docs/spec.md", Type: "note", NodeRef: nodeB}}, Relevance: RelevanceWeak, SupportedRoles: []string{"documentation"}},
	}

	packet := BuildAssessedAnswer(search.IntentSearch, "story a", TargetResolution{}, nil, AvailabilityComplete, sources)
	require.Len(t, packet.MustRead, 1)
	require.Equal(t, "story-a", packet.MustRead[0].NodeRef.NodeID)
	require.Equal(t, resultIdentity(sources[0].Result), itemIdentity(packet.MustRead[0]))
	require.NotEqual(t, resultIdentity(sources[0].Result), resultIdentity(sources[1].Result))
}

func TestBuildAssessedAnswerMatchesOrdinaryNotesByCanonicalPath(t *testing.T) {
	result := search.RankedResult{Candidate: search.Candidate{
		Handle: knowledge.NoteHandle("docs/test.md"), Path: "docs/test.md", Type: "note",
		Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}},
	}, FinalScore: 1}
	assessed := Assess(search.IntentSearch, []search.RankedResult{result})

	packet := BuildAssessedAnswer(search.IntentSearch, "test", TargetResolution{}, nil, AvailabilityComplete, assessed)
	require.Len(t, packet.MustRead, 1)
	require.Equal(t, resultIdentity(result), itemIdentity(packet.MustRead[0]))
}

func TestAssessDoesNotPromoteNearZeroLaneArtifacts(t *testing.T) {
	assessed := Assess(search.IntentSearch, []search.RankedResult{{
		Candidate: search.Candidate{Path: "docs/noise.md", Type: "note", Evidence: []search.Evidence{
			{Type: "note_vector_similarity", RawScore: 0.0001},
			{Type: "intel_fts_match", RawScore: 0.0001},
		}},
		FinalScore: 0.0002,
	}})

	require.Len(t, assessed, 1)
	require.Equal(t, RelevanceWeak, assessed[0].Relevance)
	packet := BuildAssessedAnswer(search.IntentSearch, "absent topic", TargetResolution{}, nil, AvailabilityComplete, assessed)
	require.Empty(t, packet.MustRead)
	require.Equal(t, "low", packet.Confidence.Level)
}

func TestAssessPromotesMeaningfulIndependentCorroboration(t *testing.T) {
	assessed := Assess(search.IntentSearch, []search.RankedResult{{
		Candidate: search.Candidate{Path: "pkg/search/merge.go", Type: "code", Evidence: []search.Evidence{
			{Type: "code_vector_similarity", RawScore: 0.72},
			{Type: "intel_fts_match", RawScore: 0.51, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "merge ranked search evidence"}},
		}},
		FinalScore: 0.76,
	}})

	require.Len(t, assessed, 1)
	require.Equal(t, RelevanceStrong, assessed[0].Relevance)
}

func TestAssessQueryDoesNotPromoteCodeFromTheWrongExplicitLanguage(t *testing.T) {
	results := []search.RankedResult{
		{Candidate: search.Candidate{Handle: knowledge.FileHandle("src/tasks.py"), Path: "src/tasks.py", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: 1},
		{Candidate: search.Candidate{Handle: knowledge.FileHandle("csharp/TaskService.cs"), Path: "csharp/TaskService.cs", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: .9},
	}

	assessed := AssessQuery("what happens when the C# service adds a task", search.IntentSearch, results)

	require.Equal(t, RelevanceUseful, assessed[0].Relevance)
	require.Equal(t, "code language does not match the explicit query language", assessed[0].RelevanceWhy)
	require.Equal(t, RelevanceStrong, assessed[1].Relevance)
	packet := BuildAssessedAnswer(search.IntentSearch, "what happens when the C# service adds a task", TargetResolution{}, nil, AvailabilityComplete, assessed)
	require.Len(t, packet.MustRead, 1)
	require.Equal(t, "csharp/TaskService.cs", packet.MustRead[0].Path)
}

func TestAssessQueryAllowsEveryExplicitlyRequestedLanguageAndCrossLanguageDocs(t *testing.T) {
	results := []search.RankedResult{
		{Candidate: search.Candidate{Path: "src/tasks.py", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: 1},
		{Candidate: search.Candidate{Path: "csharp/TaskService.cs", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: 1},
		{Candidate: search.Candidate{Path: "docs/python-interop.md", Type: "doc_section", Evidence: strongQuerySpecificityEvidence()}, FinalScore: 1},
	}

	assessed := AssessQuery("compare the C# and Python task services", search.IntentSearch, results)

	require.Equal(t, RelevanceStrong, assessed[0].Relevance)
	require.Equal(t, RelevanceStrong, assessed[1].Relevance)
	require.Equal(t, RelevanceStrong, assessed[2].Relevance)
}

func TestAssessQueryDoesNotTreatEnglishGoAsALanguageOrPromoteWeakMismatch(t *testing.T) {
	weakPython := search.RankedResult{Candidate: search.Candidate{Path: "src/queue.py", Type: "code"}}
	strongPython := search.RankedResult{Candidate: search.Candidate{Path: "src/tasks.py", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: 1}

	english := AssessQuery("where do queued tasks go after retry", search.IntentSearch, []search.RankedResult{strongPython})
	require.Equal(t, RelevanceStrong, english[0].Relevance)
	for _, imperative := range []string{
		"Go through the retry handlers", "Go over the retry handlers", "Go Check the retry handlers",
		"Go Back through the retry handlers", "Go. Retry the request",
	} {
		require.Equal(t, RelevanceStrong, AssessQuery(imperative, search.IntentSearch, []search.RankedResult{strongPython})[0].Relevance)
	}
	require.Equal(t, RelevanceUseful, AssessQuery("Go code retry handler", search.IntentSearch, []search.RankedResult{strongPython})[0].Relevance)
	require.Equal(t, RelevanceUseful, AssessQuery("retry handler in go", search.IntentSearch, []search.RankedResult{strongPython})[0].Relevance)
	require.Equal(t, RelevanceUseful, AssessQuery("Go RetryHandler", search.IntentSearch, []search.RankedResult{strongPython})[0].Relevance)
	require.Equal(t, RelevanceUseful, AssessQuery("Go retryHandler", search.IntentSearch, []search.RankedResult{strongPython})[0].Relevance)

	mismatch := AssessQuery("where is the C# queue implementation", search.IntentSearch, []search.RankedResult{weakPython})
	require.Equal(t, RelevanceWeak, mismatch[0].Relevance)
}

func TestAggregateFacetsPreservesEachFacetLanguageConstraint(t *testing.T) {
	csharp := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("csharp/TaskService.cs"), Path: "csharp/TaskService.cs", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: 1}
	python := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("src/retry.py"), Path: "src/retry.py", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: .9}
	aggregated := AggregateFacets([]search.Response{
		{Query: search.QuerySpec{Text: "C# task service", Intent: search.IntentSearch}, Results: []search.RankedResult{csharp}},
		{Query: search.QuerySpec{Text: "retry queue policy", Intent: search.IntentSearch}, Results: []search.RankedResult{python}},
	}, 10)

	assessed := AssessQuery("C# task service\n\nretry queue policy", search.IntentSearch, aggregated)

	require.Len(t, assessed, 2)
	require.Equal(t, RelevanceStrong, assessed[0].Relevance)
	require.Equal(t, RelevanceStrong, assessed[1].Relevance)
}

func TestAssessQueryIgnoresUnownedQueryMatchEvidence(t *testing.T) {
	result := search.RankedResult{Candidate: search.Candidate{Path: "src/noise.py", Type: "code", Evidence: []search.Evidence{{
		Type: "query_match", Source: "external", Details: map[string]string{"query": "policy", "mode": "search", "relevance": "strong"},
	}}}}

	assessed := AssessQuery("policy", search.IntentSearch, []search.RankedResult{result})

	require.Equal(t, RelevanceWeak, assessed[0].Relevance)
	require.Empty(t, assessed[0].FacetSupport)
}

func TestAssessQuerySelectsOneCoherentFacetObservationRegardlessOfEvidenceOrder(t *testing.T) {
	strongSupporting := search.Evidence{Type: "query_match", Source: "multi_query", Details: map[string]string{
		"query": "who calls Target", "mode": "callers", "relevance": "strong", "eligibility": "supporting",
	}}
	strongPrimary := search.Evidence{Type: "query_match", Source: "multi_query", Details: map[string]string{
		"query": "who calls Target", "mode": "callers", "relevance": "strong", "eligibility": "primary", "relationship": "caller", "target_established": "true",
	}}
	useful := search.Evidence{Type: "query_match", Source: "multi_query", Details: map[string]string{
		"query": "who calls Target", "mode": "callers", "relevance": "useful", "eligibility": "primary", "relationship": "caller", "target_established": "true",
	}}
	want := FacetSupport{Text: "who calls Target", Mode: search.IntentCallers, Relevance: RelevanceStrong, Eligibility: EvidencePrimary, Relationship: "caller", TargetEstablished: true, SupportedRoles: []string{"implementation"}}

	for _, evidence := range [][]search.Evidence{{strongSupporting, useful, strongPrimary}, {strongPrimary, useful, strongSupporting}} {
		result := search.RankedResult{Candidate: search.Candidate{Path: "src/caller.go", Type: "code", Evidence: evidence}}
		assessment := Assess(search.IntentSearch, []search.RankedResult{result})[0]
		require.Equal(t, RelevanceStrong, assessment.Relevance)
		require.Equal(t, []FacetSupport{want}, assessment.FacetSupport)
	}
}

func TestAssessQueryRequiresEvidenceForExplicitNamedEntity(t *testing.T) {
	query := "what action did the review assign to Maya"
	wrong := queryframe.EnrichCandidate(queryframe.Extract(query), search.Candidate{
		Path: "specs/route-assignment.md", Title: "Assignment review", Type: "note",
		Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}, {Type: "intel_doc_match", RawScore: .9, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "A stale assignment creates a review item."}}},
	})
	right := queryframe.EnrichCandidate(queryframe.Extract(query), search.Candidate{
		Path: "meetings/reliability-review.md", Title: "Reliability review", Type: "note",
		Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}, {Type: "intel_doc_match", RawScore: .9, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "Add quarantine visibility; assignee Maya."}}},
	})
	missingExcerpt := queryframe.EnrichCandidate(queryframe.Extract(query), search.Candidate{
		Path: "specs/assignment-review.md", Title: "Assignment review", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}},
	})

	assessed := AssessQuery(query, search.IntentSearch, []search.RankedResult{
		{Candidate: wrong, FinalScore: 1}, {Candidate: right, FinalScore: .9}, {Candidate: missingExcerpt, FinalScore: .8},
	})

	require.Equal(t, RelevanceUseful, assessed[0].Relevance)
	require.Equal(t, "retrieved evidence does not establish an explicit query entity", assessed[0].RelevanceWhy)
	require.Equal(t, RelevanceStrong, assessed[1].Relevance)
	require.Equal(t, RelevanceUseful, assessed[2].Relevance, "missing source text remains useful rather than proving irrelevance")
}

func TestAssessQueryKeepsEstablishedCallerProofForNamedTarget(t *testing.T) {
	result := search.RankedResult{Candidate: search.Candidate{
		Path: "src/main.ts", FQN: "app.main.runWorkspace", Type: "anchor",
		Evidence: []search.Evidence{{Type: "call_edge", Details: map[string]string{"callee": "app.target.targetCall"}}},
	}}

	assessment := AssessQuery("callers of TypeScript barrelCall", search.IntentCallers, []search.RankedResult{result})[0]

	require.Equal(t, EvidencePrimary, assessment.Eligibility)
	require.Equal(t, RelevanceStrong, assessment.Relevance)
	require.Equal(t, "caller", assessment.Relationship)
}

func TestExplicitQueryEntitiesRequireReliableSyntaxOrOwnershipCue(t *testing.T) {
	require.Empty(t, explicitQueryEntities("Explain Retry policy"))
	require.Equal(t, [][]string{{"maya"}}, explicitQueryEntities("what was assigned to Maya"))
	require.Equal(t, [][]string{{"sync", "client"}}, explicitQueryEntities("where is SyncClient defined"))
	require.Equal(t, [][]string{{"queue", "policy"}}, explicitQueryEntities(`explain "queue policy"`))
}

func TestAssessQueryRequiresOneSourceOwnedContiguousEntitySpan(t *testing.T) {
	forcedStrong := func(source, snippet string) search.RankedResult {
		candidate := queryframe.EnrichCandidate(queryframe.Extract(`what was assigned to "Maya Chen"`), search.Candidate{
			Path: "notes/assignment.md", Type: "note", Evidence: []search.Evidence{
				{Type: "path_exact", RawScore: 1},
				{Type: "intel_doc_match", RawScore: 1, Source: source, Details: map[string]string{"snippet": snippet}},
			},
		})
		return search.RankedResult{Candidate: candidate, FinalScore: 1}
	}

	split := AssessQuery(`what was assigned to "Maya Chen"`, search.IntentSearch, []search.RankedResult{forcedStrong("pkg/anchors/sqlite", "Maya owns queue. Chen owns storage.")})
	require.Equal(t, RelevanceUseful, split[0].Relevance)

	unowned := AssessQuery(`what was assigned to "Maya Chen"`, search.IntentSearch, []search.RankedResult{forcedStrong("external", "Maya Chen owns queue.")})
	require.Equal(t, RelevanceUseful, unowned[0].Relevance)

	owned := AssessQuery(`what was assigned to "Maya Chen"`, search.IntentSearch, []search.RankedResult{forcedStrong("pkg/anchors/sqlite", "Maya Chen owns queue.")})
	require.Equal(t, RelevanceStrong, owned[0].Relevance)
}

func TestAssessQueryPreservesStopWordsInQuotedEntityAndIgnoresApostrophes(t *testing.T) {
	bankQuery := `explain "Bank of America" policy`
	bank := queryframe.EnrichCandidate(queryframe.Extract(bankQuery), search.Candidate{Path: "notes/bank.md", Type: "note", Evidence: []search.Evidence{{
		Type: "intel_doc_match", RawScore: 1, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "Bank of America policy"},
	}}})
	require.Equal(t, RelevanceStrong, AssessQuery(bankQuery, search.IntentSearch, []search.RankedResult{{Candidate: bank, FinalScore: 1}})[0].Relevance)

	possessiveQuery := "what's the user's retry policy"
	possessive := queryframe.EnrichCandidate(queryframe.Extract(possessiveQuery), search.Candidate{Path: "notes/retry.md", Type: "note", Evidence: []search.Evidence{{
		Type: "intel_doc_match", RawScore: 1, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "The user's retry policy uses backoff."},
	}}})
	require.Empty(t, explicitQueryEntities(possessiveQuery))
	require.Equal(t, RelevanceStrong, AssessQuery(possessiveQuery, search.IntentSearch, []search.RankedResult{{Candidate: possessive, FinalScore: 1}})[0].Relevance)
}

func TestExplicitQueryEntityDelimitersMustPairAndApostrophesDoNotSuppressCodeNames(t *testing.T) {
	malformed := `what was assigned to ` + "`Maya Chen\""
	require.Empty(t, explicitQueryEntities(malformed))

	query := "where is 'SyncClient' defined"
	entities := explicitQueryEntities(query)
	require.Equal(t, [][]string{{"sync", "client"}}, entities)
	result := search.RankedResult{Candidate: search.Candidate{Path: "src/OtherClient.php", Type: "code", Evidence: []search.Evidence{{Type: "path_exact", RawScore: 1}}}, FinalScore: 1}
	require.Equal(t, RelevanceUseful, AssessQuery(query, search.IntentSearch, []search.RankedResult{result})[0].Relevance)
}

func TestAssessQueryDoesNotTreatLinkSnippetAsStrongSourceSupport(t *testing.T) {
	query := "retry queue policy behavior"
	candidate := queryframe.EnrichCandidate(queryframe.Extract(query), search.Candidate{Path: "src/unrelated.go", Type: "code", Evidence: []search.Evidence{{
		Type: "code_ref", RawScore: 1, Source: "doc_links", Details: map[string]string{"snippet": query},
	}, {Type: "code_vector_similarity", RawScore: .5, Source: "pkg/semantic"}}})
	owned := queryframe.EnrichCandidate(queryframe.Extract(query), search.Candidate{Path: "src/retry.go", Type: "code", Evidence: []search.Evidence{{
		Type: "intel_fts_match", RawScore: .5, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "retry queue"},
	}, {Type: "code_vector_similarity", RawScore: .5, Source: "pkg/semantic"}}})
	external := queryframe.EnrichCandidate(queryframe.Extract(query), search.Candidate{Path: "src/external.go", Type: "code", Evidence: []search.Evidence{{
		Type: "intel_doc_match", RawScore: .5, Source: "external", Details: map[string]string{"snippet": "retry queue"},
	}, {Type: "code_vector_similarity", RawScore: .5, Source: "pkg/semantic"}}})
	pathOnly := queryframe.EnrichCandidate(queryframe.Extract(query), search.Candidate{Path: "src/path-only.go", Type: "code", Evidence: []search.Evidence{{
		Type: "intel_fts_match", RawScore: .5, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "retry queue", "pathOnly": "true"},
	}, {Type: "code_vector_similarity", RawScore: .5, Source: "pkg/semantic"}}})

	assessment := AssessQuery(query, search.IntentSearch, []search.RankedResult{{Candidate: candidate, FinalScore: 1}})[0]
	ownedAssessment := AssessQuery(query, search.IntentSearch, []search.RankedResult{{Candidate: owned, FinalScore: 1}})[0]
	externalAssessment := AssessQuery(query, search.IntentSearch, []search.RankedResult{{Candidate: external, FinalScore: 1}})[0]
	pathOnlyAssessment := AssessQuery(query, search.IntentSearch, []search.RankedResult{{Candidate: pathOnly, FinalScore: 1}})[0]

	require.NotEqual(t, RelevanceStrong, assessment.Relevance)
	require.Equal(t, RelevanceUseful, externalAssessment.Relevance)
	require.Equal(t, RelevanceUseful, pathOnlyAssessment.Relevance)
	require.Equal(t, RelevanceStrong, ownedAssessment.Relevance)
	require.Equal(t, "semantic relevance is corroborated by an independent evidence lane", ownedAssessment.RelevanceWhy)
}

func TestAggregateFacetsBindsLanguageConstraintToTheExactFacetQuery(t *testing.T) {
	python := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("src/tasks.py"), Path: "src/tasks.py", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: 1}
	csharp := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("csharp/TaskService.cs"), Path: "csharp/TaskService.cs", Type: "code", Evidence: strongQuerySpecificityEvidence()}, FinalScore: .9}

	aggregated := AggregateFacets([]search.Response{{
		Query:   search.QuerySpec{Text: "what happens when the C# service adds a task", Intent: search.IntentSearch},
		Results: []search.RankedResult{python, csharp},
	}}, 10)
	assessed := Assess(search.IntentSearch, aggregated)

	require.Len(t, assessed, 2)
	require.Equal(t, RelevanceUseful, assessed[0].FacetSupport[0].Relevance)
	require.Equal(t, RelevanceStrong, assessed[1].FacetSupport[0].Relevance)
}

func TestAssessAssignsCodeAnchorsImplementationRoleWithoutChangingNoteAnchors(t *testing.T) {
	code := search.RankedResult{Candidate: search.Candidate{
		Handle: knowledge.AnchorHandle("code-symbol"), Type: "anchor", Path: "pkg/service.go", FQN: "example.Service",
		Evidence: []search.Evidence{{Type: "definition_anchor", RawScore: 1}},
	}}
	note := search.RankedResult{Candidate: search.Candidate{
		Handle: knowledge.NodeChunkHandle("decision", "docs/design.md", "node_body", 0), Type: "anchor", Path: "docs/design.md",
		NodeRef:  &ontology.NodeRef{NotePath: "docs/design.md", NodeID: "decision", Kind: ontology.NodeKindEmbedded},
		Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}},
	}}

	assessed := Assess(search.IntentGoToDef, []search.RankedResult{code, note})
	require.Equal(t, []string{"implementation"}, assessed[0].SupportedRoles)
	require.Equal(t, []string{"documentation"}, assessed[1].SupportedRoles)
}

func TestBuildAssessedAnswerKeepsPrecisionContextOutOfMustRead(t *testing.T) {
	primary := search.RankedResult{Candidate: search.Candidate{
		Handle: knowledge.AnchorHandle("selected"), Type: "anchor", Path: "pkg/merge.go", FQN: "pkg.Merge",
		Evidence: []search.Evidence{{Type: "definition_anchor", RawScore: 1}},
	}, FinalScore: 0.9}
	context := search.RankedResult{Candidate: search.Candidate{
		Handle: knowledge.FileHandle("pkg/merge.go"), Type: "code", Path: "pkg/merge.go",
		Evidence: []search.Evidence{{Type: "code_anchor", RawScore: 1}, {Type: "path_exact", RawScore: 1}},
	}, FinalScore: 0.8}
	assessed := Assess(search.IntentGoToDef, []search.RankedResult{primary, context})
	require.Equal(t, EvidencePrimary, assessed[0].Eligibility)
	require.Equal(t, EvidenceSupporting, assessed[1].Eligibility)

	packet := BuildAssessedAnswer(search.IntentGoToDef, "definition of Merge", TargetResolution{Status: search.TargetStatusInferredSymbol, Confidence: .95, Selected: &search.TargetCandidate{FQN: "pkg.Merge", Path: "pkg/merge.go"}}, nil, AvailabilityComplete, assessed)
	require.Len(t, packet.MustRead, 1)
	require.Equal(t, "pkg.Merge", packet.MustRead[0].FQN)
	require.Len(t, packet.Supporting, 1)
	require.Equal(t, "pkg/merge.go", packet.Supporting[0].Path)
}

func TestAggregateFacetsIsOrderIndependentAndRetainsEachFacet(t *testing.T) {
	first := search.Response{Query: search.QuerySpec{Text: "implementation", Intent: search.IntentSearch}, Results: []search.RankedResult{
		{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/shared.go"), Path: "pkg/shared.go", Type: "code"}, FinalScore: 0.9},
		{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/implementation.go"), Path: "pkg/implementation.go", Type: "code"}, FinalScore: 0.8},
	}}
	second := search.Response{Query: search.QuerySpec{Text: "proof", Intent: search.IntentTestsForCode}, Results: []search.RankedResult{
		{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/shared.go"), Path: "pkg/shared.go", Type: "code"}, FinalScore: 0.85},
		{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/implementation_test.go"), Path: "pkg/implementation_test.go", Type: "code"}, FinalScore: 0.79},
	}}

	forward := AggregateFacets([]search.Response{first, second}, 3)
	reverse := AggregateFacets([]search.Response{second, first}, 3)
	require.Equal(t, canonicalPaths(forward), canonicalPaths(reverse))
	require.Equal(t, []string{"pkg/shared.go", "pkg/implementation.go", "pkg/implementation_test.go"}, canonicalPaths(forward))
	cappedForward := AggregateFacets([]search.Response{first, second}, 2)
	cappedReverse := AggregateFacets([]search.Response{second, first}, 2)
	require.Equal(t, canonicalPaths(cappedForward), canonicalPaths(cappedReverse))
	require.Equal(t, []string{"pkg/implementation.go", "pkg/implementation_test.go"}, canonicalPaths(cappedForward))
}

func TestAggregateFacetsBindsRelevanceToTheFacetThatHasSupport(t *testing.T) {
	sharedStrong := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/shared.go"), Path: "pkg/shared.go", Type: "code", Evidence: []search.Evidence{{Type: "path_exact", RawScore: 1}}}, FinalScore: .9}
	sharedNoise := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/shared.go"), Path: "pkg/shared.go", Type: "code", Evidence: []search.Evidence{{Type: "code_vector_similarity", RawScore: .001}}}, FinalScore: .001}
	proof := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/shared_test.go"), Path: "pkg/shared_test.go", Type: "code", Evidence: []search.Evidence{{Type: "path_exact", RawScore: 1}}}, FinalScore: .8}

	aggregated := AggregateFacets([]search.Response{
		{Query: search.QuerySpec{Text: "implementation", Intent: search.IntentSearch}, Results: []search.RankedResult{sharedStrong}},
		{Query: search.QuerySpec{Text: "proof", Intent: search.IntentSearch}, Results: []search.RankedResult{sharedNoise, proof}},
	}, 3)
	assessed := Assess(search.IntentSearch, aggregated)
	require.Len(t, assessed, 2)
	require.Equal(t, []FacetSupport{{Text: "implementation", Mode: search.IntentSearch, Relevance: RelevanceStrong, Eligibility: EvidenceSupporting, LeadsFacet: true, SupportedRoles: []string{"implementation"}}}, assessed[0].FacetSupport)
	require.Equal(t, "proof", assessed[1].FacetSupport[0].Text)
}

func TestAggregateFacetsPreservesPrecisionFacetProof(t *testing.T) {
	target := search.TargetCandidate{FQN: "pkg.Target"}
	result := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.AnchorHandle("caller"), Path: "caller.go", FQN: "pkg.Caller", Type: "anchor", Evidence: []search.Evidence{{Type: "call_edge", RawScore: 1}}}, FinalScore: 1}
	aggregated := AggregateFacets([]search.Response{{
		Query:   search.QuerySpec{Text: "who calls Target", Intent: search.IntentCallers, TargetStatus: search.TargetStatusInferredSymbol, ResolutionConfidence: .95, ResolvedTarget: &target},
		Results: []search.RankedResult{result},
	}}, 10)
	assessed := Assess(search.IntentSearch, aggregated)
	require.Len(t, assessed, 1)
	require.Equal(t, []FacetSupport{{Text: "who calls Target", Mode: search.IntentCallers, Relevance: RelevanceStrong, Eligibility: EvidencePrimary, Relationship: "caller", TargetEstablished: true, LeadsFacet: true, SupportedRoles: []string{"implementation"}}}, assessed[0].FacetSupport)
}

func TestBuildAssessedAnswerForFacetsDoesNotClaimHighConfidenceWhenOneFacetIsUnsupported(t *testing.T) {
	doc := BindFacetSupport(SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("docs/design.md"), Path: "docs/design.md", Title: "Design", Type: "note"}, FinalScore: .9},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "how is it designed?", search.IntentSearch)
	code := BindFacetSupport(SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/service.go"), Path: "pkg/service.go", Symbol: "Service", Type: "code"}, FinalScore: .85},
		Relevance: RelevanceStrong, SupportedRoles: []string{"implementation"},
	}, "how is it implemented?", search.IntentSearch)

	packet := BuildAssessedAnswerForFacets(search.IntentSearch, "design service", []RequiredFacet{
		{Text: "how is it designed?", Mode: search.IntentSearch},
		{Text: "how is it implemented?", Mode: search.IntentSearch},
		{Text: "what tests prove it?", Mode: search.IntentSearch},
	}, TargetResolution{}, nil, AvailabilityComplete, []SourceAssessment{doc, code})

	require.True(t, packet.Coverage.Docs)
	require.True(t, packet.Coverage.Code)
	require.Contains(t, packet.Coverage.Missing, "facet:what tests prove it?")
	require.NotEqual(t, "high", packet.Confidence.Level)
}

func TestBuildAssessedAnswerForFacetsTreatsSingleQueryAsOrdinaryAnswer(t *testing.T) {
	source := SourceAssessment{
		Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("policy.md"), Path: "policy.md", Title: "Policy", Type: "note"}, FinalScore: .9},
		Eligibility: EvidenceSupporting, Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}
	packet := BuildAssessedAnswerForFacets(search.IntentSearch, "policy", []RequiredFacet{{Text: "policy", Mode: search.IntentSearch}}, TargetResolution{}, nil, AvailabilityComplete, []SourceAssessment{source})
	require.Len(t, packet.MustRead, 1)
	require.Equal(t, "policy.md", packet.MustRead[0].Path)
}

func TestBuildAssessedAnswerForFacetsSelectsEachSourceBoundProof(t *testing.T) {
	owner := BindFacetSupport(SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("people/maya.md"), Path: "people/maya.md", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: .7},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "who owns the mobile client?", search.IntentSearch)
	outage := BindFacetSupport(SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("projects/mobile.md"), Path: "projects/mobile.md", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: .9},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "what outage duration must it tolerate?", search.IntentSearch)
	distractor := SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("internal/queue.go"), Path: "internal/queue.go", Type: "code"}, FinalScore: 10},
		Relevance: RelevanceUseful, SupportedRoles: []string{"implementation"},
	}

	packet := BuildAssessedAnswerForFacets(search.IntentSearch, "mobile ownership and outage", []RequiredFacet{
		{Text: "who owns the mobile client?", Mode: search.IntentSearch},
		{Text: "what outage duration must it tolerate?", Mode: search.IntentSearch},
	}, TargetResolution{}, nil, AvailabilityComplete, []SourceAssessment{distractor, outage, owner})

	require.ElementsMatch(t, []string{"people/maya.md", "projects/mobile.md"}, []string{packet.MustRead[0].Path, packet.MustRead[1].Path})
	require.Empty(t, packet.Coverage.Missing)
	require.Equal(t, "high", packet.Confidence.Level)
}

func TestBuildAssessedAnswerForFacetsPrefersSeparateStrongProofsOverUsefulBroadMatch(t *testing.T) {
	first := BindFacetSupport(SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("first.md"), Path: "first.md", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: .7},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "first facet", search.IntentSearch)
	second := BindFacetSupport(SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("second.md"), Path: "second.md", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: .6},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "second facet", search.IntentSearch)
	broad := BindFacetSupport(SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("broad.md"), Path: "broad.md", Type: "note"}, FinalScore: 10},
		Relevance: RelevanceUseful, SupportedRoles: []string{"documentation"},
	}, "first facet", search.IntentSearch)
	broad = BindFacetSupport(broad, "second facet", search.IntentSearch)

	packet := BuildAssessedAnswerForFacets(search.IntentSearch, "both facets", []RequiredFacet{
		{Text: "first facet", Mode: search.IntentSearch}, {Text: "second facet", Mode: search.IntentSearch},
	}, TargetResolution{}, nil, AvailabilityComplete, []SourceAssessment{broad, second, first})
	require.Equal(t, []string{"first.md", "second.md"}, []string{packet.MustRead[0].Path, packet.MustRead[1].Path})
	require.Equal(t, "high", packet.Confidence.Level)
}

func TestBuildAssessedAnswerForFacetsPrefersDirectStrongProofForEachFacetOverBroadStrongMatch(t *testing.T) {
	first := BindFacetSupport(SourceAssessment{
		Result: search.RankedResult{Candidate: search.Candidate{
			Handle: knowledge.NoteHandle("owners/mobile-owner.md"), Path: "owners/mobile-owner.md", Title: "Mobile owner", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}},
		}, FinalScore: .7},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "mobile owner", search.IntentSearch)
	second := BindFacetSupport(SourceAssessment{
		Result: search.RankedResult{Candidate: search.Candidate{
			Handle: knowledge.NoteHandle("recovery/card-return.md"), Path: "recovery/card-return.md", Title: "Card return", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}},
		}, FinalScore: .6},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "card return", search.IntentSearch)
	broad := BindFacetSupport(SourceAssessment{
		Result: search.RankedResult{Candidate: search.Candidate{
			Handle: knowledge.NoteHandle("overview.md"), Path: "overview.md", Title: "Overview", Type: "note",
		}, FinalScore: 10},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "mobile owner", search.IntentSearch)
	broad = BindFacetSupport(broad, "card return", search.IntentSearch)

	packet := BuildAssessedAnswerForFacets(search.IntentSearch, "mobile owner and card return", []RequiredFacet{
		{Text: "mobile owner", Mode: search.IntentSearch}, {Text: "card return", Mode: search.IntentSearch},
	}, TargetResolution{}, nil, AvailabilityComplete, []SourceAssessment{broad, second, first})
	require.Equal(t, []string{"owners/mobile-owner.md", "recovery/card-return.md"}, []string{packet.MustRead[0].Path, packet.MustRead[1].Path})
	require.Empty(t, packet.Coverage.Missing)
	require.Equal(t, "high", packet.Confidence.Level)
}

func TestBuildAssessedAnswerForFacetsPrefersFacetLocalStrengthBeforeIdentitySpecificity(t *testing.T) {
	const owner = "mobile owner"
	broad := SourceAssessment{
		Result:         search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("overview.md"), Path: "overview.md", Type: "note"}, FinalScore: .5},
		Relevance:      RelevanceUseful,
		FacetSupport:   []FacetSupport{{Text: owner, Mode: search.IntentSearch, Relevance: RelevanceStrong}},
		SupportedRoles: []string{"documentation"},
	}
	namedButWeak := BindFacetSupport(SourceAssessment{
		Result:         search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("mobile-owner.md"), Path: "mobile-owner.md", Type: "note"}, FinalScore: 10},
		Relevance:      RelevanceUseful,
		SupportedRoles: []string{"documentation"},
	}, owner, search.IntentSearch)
	card := BindFacetSupport(SourceAssessment{
		Result:         search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("card-return.md"), Path: "card-return.md", Type: "note"}, FinalScore: .8},
		Relevance:      RelevanceStrong,
		SupportedRoles: []string{"documentation"},
	}, "card return", search.IntentSearch)

	packet := BuildAssessedAnswerForFacets(search.IntentSearch, "mobile owner and card return", []RequiredFacet{
		{Text: owner, Mode: search.IntentSearch}, {Text: "card return", Mode: search.IntentSearch},
	}, TargetResolution{}, nil, AvailabilityComplete, []SourceAssessment{namedButWeak, card, broad})
	require.Equal(t, []string{"overview.md", "card-return.md"}, []string{packet.MustRead[0].Path, packet.MustRead[1].Path})
	require.Empty(t, packet.Coverage.Missing)
}

func TestBuildAssessedAnswerForPrecisionFacetRequiresLocalRelationshipAndTargetProof(t *testing.T) {
	context := BindFacetSupport(SourceAssessment{
		Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("context.md"), Path: "context.md", Type: "note"}, FinalScore: .9},
		Eligibility: EvidenceSupporting, Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "who calls Target", search.IntentCallers)
	packet := BuildAssessedAnswerForFacets(search.IntentSearch, "call context", []RequiredFacet{{Text: "who calls Target", Mode: search.IntentCallers}}, TargetResolution{}, nil, AvailabilityComplete, []SourceAssessment{context})
	require.Contains(t, packet.Coverage.Missing, "facet:who calls Target")
	require.NotEqual(t, "high", packet.Confidence.Level)

	caller := SourceAssessment{
		Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.AnchorHandle("caller"), Path: "caller.go", FQN: "pkg.Caller", Type: "anchor"}, FinalScore: .8},
		Eligibility: EvidencePrimary, Relationship: "caller", Relevance: RelevanceStrong, SupportedRoles: []string{"implementation"},
		FacetSupport: []FacetSupport{{Text: "who calls Target", Mode: search.IntentCallers, Relevance: RelevanceStrong, Eligibility: EvidencePrimary, Relationship: "caller", TargetEstablished: true}},
	}
	packet = BuildAssessedAnswerForFacets(search.IntentSearch, "call context", []RequiredFacet{{Text: "who calls Target", Mode: search.IntentCallers}}, TargetResolution{}, nil, AvailabilityComplete, []SourceAssessment{caller})
	require.NotContains(t, packet.Coverage.Missing, "facet:who calls Target")
}

func TestBuildAssessedAnswerStopsAfterStrongQueryProof(t *testing.T) {
	strong := SourceAssessment{
		Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("policy.md"), Path: "policy.md", Type: "note"}, FinalScore: .8},
		Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}
	sources := []SourceAssessment{strong}
	for index := 0; index < 5; index++ {
		sources = append(sources, SourceAssessment{
			Result:    search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle(fmt.Sprintf("noise-%d.md", index)), Path: fmt.Sprintf("noise-%d.md", index), Type: "note"}, FinalScore: 1 + float64(index)},
			Relevance: RelevanceUseful, SupportedRoles: []string{"documentation"},
		})
	}

	packet := BuildAssessedAnswer(search.IntentSearch, "replacement package size", TargetResolution{}, nil, AvailabilityComplete, sources)
	require.Len(t, packet.MustRead, 1)
	require.Equal(t, "policy.md", packet.MustRead[0].Path)
}

func TestBuildAssessedAnswerRequiresSourceBoundPrecisionRelationship(t *testing.T) {
	context := SourceAssessment{
		Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.AnchorHandle("context"), Path: "pkg/context.go", FQN: "pkg.Context", Type: "anchor"}, FinalScore: 10},
		Eligibility: EvidenceSupporting, Relevance: RelevanceStrong, SupportedRoles: []string{"implementation"},
	}
	caller := SourceAssessment{
		Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.AnchorHandle("caller"), Path: "pkg/caller.go", FQN: "pkg.Caller", Type: "anchor", Evidence: []search.Evidence{{Type: "symbol_exact", RawScore: 1}}}, FinalScore: .8},
		Eligibility: EvidencePrimary, Relationship: "caller", Relevance: RelevanceStrong, SupportedRoles: []string{"implementation"},
	}

	packet := BuildAssessedAnswer(search.IntentCallers, "callers of Target", TargetResolution{
		Status: search.TargetStatusInferredSymbol, Confidence: .95, Selected: &search.TargetCandidate{FQN: "pkg.Target"},
	}, nil, AvailabilityComplete, []SourceAssessment{context, caller})

	require.Len(t, packet.MustRead, 1)
	require.Equal(t, "pkg.Caller", packet.MustRead[0].FQN)
	require.NotContains(t, packet.Coverage.Missing, "relationship:caller")
	require.Equal(t, "high", packet.Confidence.Level)
}

func TestResolvePrecisionEvidenceTargetRequiresOneProvedTarget(t *testing.T) {
	proof := func(target string) SourceAssessment {
		return SourceAssessment{
			Result: search.RankedResult{Candidate: search.Candidate{Evidence: []search.Evidence{{
				Type: "tests_path", Details: map[string]string{"target_fqn": target},
			}}}},
			Eligibility: EvidencePrimary, Relationship: "tests",
		}
	}
	current := TargetResolution{Status: search.TargetStatusExplicitPath, Confidence: 1}

	resolved := ResolvePrecisionEvidenceTarget(search.IntentTestsForCode, current, []SourceAssessment{proof("pkg.MergeCandidate"), proof("pkg.MergeCandidate")})
	require.NotNil(t, resolved.Selected)
	require.Equal(t, "pkg.MergeCandidate", resolved.Selected.FQN)

	ambiguous := ResolvePrecisionEvidenceTarget(search.IntentTestsForCode, current, []SourceAssessment{proof("pkg.MergeCandidate"), proof("pkg.MergeEvidence")})
	require.Nil(t, ambiguous.Selected)
	oneCallerTwoTargets := proof("pkg.MergeCandidate")
	oneCallerTwoTargets.Result.Evidence = append(oneCallerTwoTargets.Result.Evidence, search.Evidence{
		Type: "tests_path", Details: map[string]string{"target_fqn": "pkg.MergeEvidence"},
	})
	require.Nil(t, ResolvePrecisionEvidenceTarget(search.IntentTestsForCode, current, []SourceAssessment{oneCallerTwoTargets}).Selected)

	unsupported := proof("pkg.MergeCandidate")
	unsupported.Eligibility = EvidenceSupporting
	require.Nil(t, ResolvePrecisionEvidenceTarget(search.IntentTestsForCode, current, []SourceAssessment{unsupported}).Selected)
}

func TestBuildAssessedAnswerRetainsAllExactPrimaryCallers(t *testing.T) {
	callers := []SourceAssessment{
		{Result: search.RankedResult{Candidate: search.Candidate{Handle: knowledge.AnchorHandle("a"), Path: "a.go", FQN: "pkg.A", Type: "anchor", Evidence: []search.Evidence{{Type: "symbol_exact", RawScore: 1}}}, FinalScore: .9}, Eligibility: EvidencePrimary, Relationship: "caller", Relevance: RelevanceStrong, SupportedRoles: []string{"implementation"}},
		{Result: search.RankedResult{Candidate: search.Candidate{Handle: knowledge.AnchorHandle("b"), Path: "b_test.go", FQN: "pkg.TestB", Type: "anchor", Evidence: []search.Evidence{{Type: "symbol_exact", RawScore: 1}}}, FinalScore: .8}, Eligibility: EvidencePrimary, Relationship: "caller", Relevance: RelevanceStrong, SupportedRoles: []string{"test"}},
	}
	packet := BuildAssessedAnswer(search.IntentCallers, "callers of Target", TargetResolution{Status: search.TargetStatusInferredSymbol, Confidence: .95, Selected: &search.TargetCandidate{FQN: "pkg.Target"}}, nil, AvailabilityComplete, callers)
	require.Equal(t, []string{"pkg.A", "pkg.TestB"}, []string{packet.MustRead[0].FQN, packet.MustRead[1].FQN})
	require.Equal(t, "high", packet.Confidence.Level)
}

func TestBuildAssessedAnswerDisclosesTruncatedExactRelationshipEvidence(t *testing.T) {
	callers := make([]SourceAssessment, 0, 7)
	for index := 0; index < 7; index++ {
		callers = append(callers, SourceAssessment{
			Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.AnchorHandle(fmt.Sprintf("caller-%d", index)), Path: fmt.Sprintf("caller-%d.go", index), FQN: fmt.Sprintf("pkg.Caller%d", index), Type: "anchor"}, FinalScore: 1 - float64(index)/10},
			Eligibility: EvidencePrimary, Relationship: "caller", Relevance: RelevanceStrong, SupportedRoles: []string{"implementation"},
		})
	}
	packet := BuildAssessedAnswer(search.IntentCallers, "callers of Target", TargetResolution{Status: search.TargetStatusInferredSymbol, Confidence: .95, Selected: &search.TargetCandidate{FQN: "pkg.Target"}}, nil, AvailabilityComplete, callers)
	require.Len(t, packet.MustRead, 6)
	require.Contains(t, packet.Coverage.Missing, "relationship:caller:truncated")
	require.NotEqual(t, "high", packet.Confidence.Level)
}

func TestBuildAssessedAnswerRetainsEveryExactPrecisionFacetRelationship(t *testing.T) {
	const facetText = "who calls Target"
	sources := make([]SourceAssessment, 0, 4)
	for index := 0; index < 3; index++ {
		sources = append(sources, SourceAssessment{
			Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.AnchorHandle(fmt.Sprintf("caller-%d", index)), Path: fmt.Sprintf("caller-%d.go", index), FQN: fmt.Sprintf("pkg.Caller%d", index), Type: "anchor"}, FinalScore: .9 - float64(index)/10},
			Eligibility: EvidencePrimary, Relationship: "caller", Relevance: RelevanceStrong, SupportedRoles: []string{"implementation"},
			FacetSupport: []FacetSupport{{Text: facetText, Mode: search.IntentCallers, Relevance: RelevanceStrong, Eligibility: EvidencePrimary, Relationship: "caller", TargetEstablished: true}},
		})
	}
	sources = append(sources, BindFacetSupport(SourceAssessment{
		Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("concept.md"), Path: "concept.md", Type: "note"}, FinalScore: .95},
		Eligibility: EvidenceSupporting, Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}, "why it matters", search.IntentSearch))
	packet := BuildAssessedAnswerForFacets(search.IntentSearch, "callers and context", []RequiredFacet{{Text: facetText, Mode: search.IntentCallers}, {Text: "why it matters", Mode: search.IntentSearch}}, TargetResolution{}, nil, AvailabilityComplete, sources)
	require.Len(t, packet.MustRead, 4)
	require.NotContains(t, packet.Coverage.Missing, "facet:"+facetText+":truncated")
}

func TestBuildAssessedAnswerRefreshesSummaryAfterAvailabilityOverride(t *testing.T) {
	source := SourceAssessment{
		Result:      search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("policy.md"), Path: "policy.md", Title: "Policy", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: 1},
		Eligibility: EvidenceSupporting, Relevance: RelevanceStrong, SupportedRoles: []string{"documentation"},
	}
	packet := BuildAssessedAnswer(search.IntentSearch, "Policy", TargetResolution{}, nil, AvailabilityPartial, []SourceAssessment{source})
	require.Equal(t, "medium", packet.Confidence.Level)
	require.Contains(t, packet.Summary, "Confidence: medium")
}

func TestAggregateFacetsPreservesDistinctEmbeddedNodes(t *testing.T) {
	response := search.Response{Query: search.QuerySpec{Text: "stories", Intent: search.IntentSearch}, Results: []search.RankedResult{
		{Candidate: search.Candidate{Handle: knowledge.NodeChunkHandle("story-a", "docs/spec.md", "node_body", 0), Path: "docs/spec.md", Type: "note", NodeRef: &ontology.NodeRef{NotePath: "docs/spec.md", NodeID: "story-a", Kind: ontology.NodeKindEmbedded}}},
		{Candidate: search.Candidate{Handle: knowledge.NodeChunkHandle("story-b", "docs/spec.md", "node_body", 0), Path: "docs/spec.md", Type: "note", NodeRef: &ontology.NodeRef{NotePath: "docs/spec.md", NodeID: "story-b", Kind: ontology.NodeKindEmbedded}}},
	}}

	require.Len(t, AggregateFacets([]search.Response{response}, 10), 2)
}

func TestDedupeCanonicalSourcesMergesEvidenceWithoutChangingFirstRank(t *testing.T) {
	first := search.RankedResult{Candidate: search.Candidate{Type: "code", Path: "pkg/service.go", FQN: "example.Service", Evidence: []search.Evidence{{Type: "lexical", Source: "code"}}}, FinalScore: .8}
	duplicate := search.RankedResult{Candidate: search.Candidate{Type: "code", Path: "pkg/service.go", FQN: "example.Service", Evidence: []search.Evidence{{Type: "vector", Source: "code"}}}, FinalScore: .7}
	other := search.RankedResult{Candidate: search.Candidate{Type: "code", Path: "pkg/other.go", FQN: "example.Other"}, FinalScore: .6}

	got := DedupeCanonicalSources([]search.RankedResult{first, duplicate, other})
	require.Len(t, got, 2)
	require.Equal(t, CanonicalSourceIdentity(first), CanonicalSourceIdentity(got[0]))
	require.Equal(t, CanonicalSourceIdentity(other), CanonicalSourceIdentity(got[1]))
	require.Len(t, got[0].Evidence, 2)
	require.Equal(t, .8, got[0].FinalScore)
}

func TestDedupeCanonicalSourcesPreservesDistinctSymbolsInOneFile(t *testing.T) {
	first := search.RankedResult{Candidate: search.Candidate{Type: "code", Path: "pkg/service.go", FQN: "example.First"}, FinalScore: .8}
	second := search.RankedResult{Candidate: search.Candidate{Type: "code", Path: "pkg/service.go", FQN: "example.Second"}, FinalScore: .7}
	require.Len(t, DedupeCanonicalSources([]search.RankedResult{first, second}), 2)
}

func TestRoleForResultTreatsCodeModuleWithoutFQNAsImplementation(t *testing.T) {
	require.Equal(t, "implementation", roleForResult(search.RankedResult{Candidate: search.Candidate{
		Type: "anchor", Kind: "module", Path: "src/barrel.ts",
	}}))
	require.Equal(t, "documentation", roleForResult(search.RankedResult{Candidate: search.Candidate{
		Type: "anchor", Kind: "module", Path: "docs/spec.md", NodeRef: &ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded},
	}}))
}

func canonicalPaths(results []search.RankedResult) []string {
	out := make([]string, len(results))
	for i, result := range results {
		out[i] = result.Path
	}
	return out
}

func TestFacetSupportCoversFacetLeaderWhenOnlyUseful(t *testing.T) {
	useful := FacetSupport{Text: "why can a barcode repeat", Mode: search.IntentSearch, Relevance: RelevanceUseful}
	require.False(t, useful.Covers())
	useful.LeadsFacet = true
	require.True(t, useful.Covers())
	require.True(t, FacetSupport{Text: "x", Mode: search.IntentSearch, Relevance: RelevanceStrong}.Covers())
	precision := FacetSupport{Text: "Run", Mode: search.IntentGoToDef, Relevance: RelevanceUseful, LeadsFacet: true}
	require.False(t, precision.Covers(), "precision facets still need the resolved relationship")
}

func TestFacetSupportFromEvidenceMarksFacetLeader(t *testing.T) {
	result := search.RankedResult{Candidate: search.Candidate{Evidence: []search.Evidence{
		{Type: "query_match", Source: "multi_query", Details: map[string]string{"query": "a", "mode": "search", "rank": "0", "leads_facet": "true", "relevance": "useful"}},
		{Type: "query_match", Source: "multi_query", Details: map[string]string{"query": "b", "mode": "search", "rank": "2", "leads_facet": "false", "relevance": "useful"}},
	}}}
	support := facetSupportFromEvidence(result, nil)
	require.Len(t, support, 2)
	require.True(t, support[0].LeadsFacet)
	require.False(t, support[1].LeadsFacet)
}

func specificityEvidence(score float64) search.Evidence {
	return search.Evidence{Type: "query_specificity", Channel: search.EvidenceChannelSpecificity, RawScore: score, Source: "queryframe", Details: map[string]string{"support_score": fmt.Sprintf("%g", score)}}
}

func TestAssessBareTokenQueryIsNotStrongWithoutIdentityEvidence(t *testing.T) {
	evidence := []search.Evidence{
		{Type: "intel_fts_match", RawScore: 0.95, Source: "pkg/app/cli/current_user.go"},
		specificityEvidence(1),
	}
	result := search.RankedResult{Candidate: search.Candidate{Path: "pkg/app/cli/current_user.go", Type: "code", Evidence: evidence}, FinalScore: 1}

	assessed := AssessQuery("Current", search.IntentSearch, []search.RankedResult{result})
	require.Len(t, assessed, 1)
	require.Equal(t, RelevanceUseful, assessed[0].Relevance)

	withIdentity := result
	withIdentity.Evidence = append(append([]search.Evidence(nil), evidence...), search.Evidence{Type: "symbol_exact", RawScore: 1})
	identified := AssessQuery("Current", search.IntentSearch, []search.RankedResult{withIdentity})
	require.Len(t, identified, 1)
	require.Equal(t, RelevanceStrong, identified[0].Relevance)
}

func TestAssessSemanticNeedsARealSecondLane(t *testing.T) {
	const query = "who approves a medical claim denial"
	evidence := []search.Evidence{
		{Type: "code_vector_similarity", RawScore: 0.42},
		specificityEvidenceMatching(0.92, "claim"),
		{Type: "anchor_graph_edge", RawScore: 0.8},
	}
	result := search.RankedResult{Candidate: search.Candidate{Path: "pkg/validate/identifierreconcile/provenance_claim.go", Type: "code", Evidence: evidence}, FinalScore: 1.24}

	assessed := AssessQuery(query, search.IntentSearch, []search.RankedResult{result})
	require.Len(t, assessed, 1)
	require.Equal(t, RelevanceUseful, assessed[0].Relevance)

	withLane := result
	withLane.Evidence = append([]search.Evidence{{Type: "code_vector_similarity", RawScore: 0.42}, specificityEvidenceMatching(0.92, "medical,claim,denial"), {Type: "anchor_graph_edge", RawScore: 0.8}}, search.Evidence{Type: "intel_fts_match", RawScore: 0.6, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "medical claim denial provenance"}})
	corroborated := AssessQuery(query, search.IntentSearch, []search.RankedResult{withLane})
	require.Len(t, corroborated, 1)
	require.Equal(t, RelevanceStrong, corroborated[0].Relevance)
}

func TestFinalizeConfidenceRequiresIdentityOrTwoLanes(t *testing.T) {
	const query = "how does the merge stage rank search evidence"
	build := func(t *testing.T, evidence []search.Evidence, score float64) answer.Response {
		t.Helper()
		assessed := AssessQuery(query, search.IntentSearch, []search.RankedResult{{
			Candidate: search.Candidate{Path: "pkg/search/merge.go", Type: "code", Evidence: evidence}, FinalScore: score,
		}})
		return BuildAssessedAnswer(search.IntentSearch, query, TargetResolution{Status: search.TargetStatusNone}, nil, AvailabilityComplete, assessed)
	}

	identity := build(t, []search.Evidence{{Type: "note_title_exact", RawScore: 1}}, 1)
	require.Equal(t, "high", identity.Confidence.Level)

	twoLanes := build(t, []search.Evidence{
		{Type: "code_vector_similarity", RawScore: 0.6},
		{Type: "intel_fts_match", RawScore: 0.6, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "merge ranked search evidence"}},
	}, 1)
	require.Equal(t, "high", twoLanes.Confidence.Level)

	oneLane := build(t, []search.Evidence{
		{Type: "note_vector_similarity", RawScore: 0.6},
		{Type: "intel_fts_match", RawScore: 0.2, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "merge ranked search evidence"}},
		specificityEvidence(0.8),
	}, 1)
	require.Equal(t, "medium", oneLane.Confidence.Level)
	require.Equal(t, "strong evidence rests on one lane without an identity match", oneLane.Confidence.Reason)

	// A useful-only source with no aboutness evidence is not must-read material on a low page.
	usefulOnly := build(t, []search.Evidence{specificityEvidence(0.3)}, 0.4)
	require.Equal(t, "low", usefulOnly.Confidence.Level)
	require.Equal(t, "no source is about the query", usefulOnly.Confidence.Reason)
	require.Empty(t, usefulOnly.MustRead)
}

func specificityEvidenceMatching(score float64, matched string) search.Evidence {
	evidence := specificityEvidence(score)
	evidence.Details["matched"] = matched
	return evidence
}

func TestDemoteNonTargetDefinitionsKeepsOnlyTheResolvedDeclaration(t *testing.T) {
	definition := func(fqn, kind string) SourceAssessment {
		return SourceAssessment{Result: search.RankedResult{Candidate: search.Candidate{Type: "anchor", Path: "pkg/app/unifiedsearch/contract.go", FQN: fqn, Kind: kind, Evidence: []search.Evidence{{Type: "definition_anchor", RawScore: 1}}}}, Eligibility: EvidencePrimary, Relationship: "definition", Relevance: RelevanceStrong}
	}
	sources := []SourceAssessment{
		definition("example.com/pkg.ApplicationResult.RequestIdentity", "field"),
		definition("example.com/pkg.RequestIdentity", "function"),
	}
	selected := search.TargetCandidate{FQN: "example.com/pkg.RequestIdentity", Kind: "function"}
	out := DemoteNonTargetDefinitions(search.IntentGoToDef, TargetResolution{Status: search.TargetStatusInferredSymbol, Selected: &selected}, sources)
	require.Equal(t, EvidenceSupporting, out[0].Eligibility)
	require.Equal(t, RelevanceUseful, out[0].Relevance)
	require.Equal(t, EvidencePrimary, out[1].Eligibility)
	require.Equal(t, "definition", out[1].Relationship)
	require.Equal(t, sources, DemoteNonTargetDefinitions(search.IntentCallers, TargetResolution{Selected: &selected}, sources), "only go_to_def binds primaries to one declaration")
}

func TestAssessCoverageCountsMeaningfulTermsOnly(t *testing.T) {
	// Question and stop words can never appear in the matched inventory, so they
	// must not dilute the denominator: every meaningful term here matched.
	const query = "how is the value of the claim computed"
	result := search.RankedResult{Candidate: search.Candidate{Path: "pkg/claims/value.go", Type: "code", Evidence: []search.Evidence{
		{Type: "intel_fts_match", RawScore: 0.9, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "the claim value is computed here"}},
		specificityEvidenceMatching(0.92, "value,claim,computed"),
	}}, FinalScore: 1}

	assessed := AssessQuery(query, search.IntentSearch, []search.RankedResult{result})
	require.Len(t, assessed, 1)
	require.Equal(t, RelevanceStrong, assessed[0].Relevance)
}

func TestAssessTreatsSingleMeaningfulTermQueryAsBare(t *testing.T) {
	// "which" is a question word, so this query carries one concept and a
	// non-identity content hit must not become strong evidence.
	const query = "which policy"
	result := search.RankedResult{Candidate: search.Candidate{Path: "pkg/app/unifiedsearch/policy.go", Type: "code", Evidence: []search.Evidence{
		{Type: "intel_fts_match", RawScore: 0.9, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "policy resolution"}},
		specificityEvidenceMatching(0.92, "policy"),
	}}, FinalScore: 1}

	assessed := AssessQuery(query, search.IntentSearch, []search.RankedResult{result})
	require.Len(t, assessed, 1)
	require.Equal(t, RelevanceUseful, assessed[0].Relevance)
}

func TestAssessPartialConceptCoverageIsNotStrong(t *testing.T) {
	// "mobile" and "client" match, but the note says nothing about ownership,
	// so a content hit on two of three concepts is useful, not strong.
	const query = "who owns the mobile client"
	result := search.RankedResult{Candidate: search.Candidate{Path: "projects/mobile-dispatch.md", Title: "Mobile Dispatch", Type: "note", Evidence: []search.Evidence{
		{Type: "intel_doc_match", RawScore: 0.96, Source: "pkg/anchors/sqlite", Details: map[string]string{"snippet": "the mobile client keeps a local operation log"}},
		specificityEvidenceMatching(0.975, "client,mobile"),
	}}, FinalScore: 1.2}

	assessed := AssessQuery(query, search.IntentSearch, []search.RankedResult{result})
	require.Len(t, assessed, 1)
	require.Equal(t, RelevanceUseful, assessed[0].Relevance)
}

func TestAssessDirectiveOnlyQueryStaysBare(t *testing.T) {
	// "explain" is the whole query, so it is the only concept there is; dropping
	// it would leave nothing to cover and let identity metadata read as strong.
	result := search.RankedResult{Candidate: search.Candidate{Path: "docs/explain.md", Title: "Explain", Type: "note", Evidence: []search.Evidence{
		specificityEvidenceMatching(0.9, "explain"),
	}}, FinalScore: 1}

	assessed := AssessQuery("explain", search.IntentSearch, []search.RankedResult{result})
	require.Len(t, assessed, 1)
	require.NotEqual(t, RelevanceStrong, assessed[0].Relevance)
}

func facetLeaders(t *testing.T, text string, results []search.RankedResult) map[string]bool {
	t.Helper()
	aggregated := AggregateFacets([]search.Response{{Query: search.QuerySpec{Text: text, Intent: search.IntentSearch}, Results: results}}, 10)
	leads := map[string]bool{}
	for _, result := range aggregated {
		for _, support := range facetSupportFromEvidence(result, nil) {
			if support.Text == text {
				leads[result.Path] = support.LeadsFacet
			}
		}
	}
	return leads
}

func TestAggregateFacetsGivesDocumentationFacetsANoteLeader(t *testing.T) {
	code := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/search/service.go"), Path: "pkg/search/service.go", Type: "code", Evidence: []search.Evidence{{Type: "path_exact", RawScore: 1}}}, FinalScore: .9}
	note := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("docs/reference/subsystems/search.md"), Path: "docs/reference/subsystems/search.md", Title: "Search", Type: "note", Evidence: []search.Evidence{{Type: "path_exact", RawScore: 1}}}, FinalScore: .8}
	results := []search.RankedResult{code, note}

	documented := facetLeaders(t, "where is the search subsystem documented", results)
	require.True(t, documented["docs/reference/subsystems/search.md"])
	require.False(t, documented["pkg/search/service.go"])

	implemented := facetLeaders(t, "where is the pipeline implemented", results)
	require.True(t, implemented["pkg/search/service.go"])
	require.False(t, implemented["docs/reference/subsystems/search.md"])
}

func TestAggregateFacetsGivesTestFacetsATestLeader(t *testing.T) {
	impl := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/vault/ignore/impl.go"), Path: "pkg/vault/ignore/impl.go", Type: "code", Evidence: []search.Evidence{{Type: "path_exact", RawScore: 1}}}, FinalScore: .9}
	test := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.FileHandle("pkg/vault/ignore/x_test.go"), Path: "pkg/vault/ignore/x_test.go", Type: "code", Evidence: []search.Evidence{{Type: "path_exact", RawScore: 1}}}, FinalScore: .8}

	leads := facetLeaders(t, "which test proves nested gitignore", []search.RankedResult{impl, test})
	require.True(t, leads["pkg/vault/ignore/x_test.go"])
	require.False(t, leads["pkg/vault/ignore/impl.go"])
}

func TestFacetRoleCueMatchesWholeWordsOnly(t *testing.T) {
	require.Equal(t, "", facetRoleCue("what is the latest lane order"))
	require.Equal(t, "test", facetRoleCue("which test proves nested gitignore"))
	require.Equal(t, "documentation", facetRoleCue("which spec test covers this"))
	require.Equal(t, "", facetRoleCue("where is the pipeline implemented"))
}

func TestBuildAssessedAnswerDropsLowConfidenceMustReadThatIsNotAboutTheQuery(t *testing.T) {
	const query = "how does the scheduler decide retry backoff"
	graphEvidence := []search.Evidence{
		{Type: "intel_fts_match", RawScore: 0.6, Source: "pkg/anchors/sqlite"},
		{Type: "graph_hits_authority", RawScore: 0.5},
	}
	cases := []struct {
		name         string
		intent       search.Intent
		target       TargetResolution
		availability Availability
		relevance    RelevanceLevel
		eligibility  EvidenceEligibility
		relationship string
		evidence     []search.Evidence
		score        float64
		wantMustRead bool
		wantReason   string
	}{
		{
			name: "graph popularity and body terms are not aboutness", intent: search.IntentSearch,
			availability: AvailabilityComplete, relevance: RelevanceUseful, eligibility: EvidenceSupporting,
			evidence: graphEvidence, score: 0.84, wantMustRead: false, wantReason: "no source is about the query",
		},
		{
			name: "title match above the score floor stays must read", intent: search.IntentSearch,
			availability: AvailabilityComplete, relevance: RelevanceUseful, eligibility: EvidenceSupporting,
			evidence: append([]search.Evidence{{Type: "note_title_match", RawScore: 0.8}}, graphEvidence...),
			score:    1.2, wantMustRead: true, wantReason: "no source has identity-level or corroborated support",
		},
		{
			name: "aboutness below the score floor is demoted", intent: search.IntentSearch,
			availability: AvailabilityComplete, relevance: RelevanceUseful, eligibility: EvidenceSupporting,
			evidence: []search.Evidence{{Type: "note_vector_similarity", RawScore: 0.5}, {Type: "graph_hits_hub", RawScore: 0.4}},
			score:    0.64, wantMustRead: false, wantReason: "no source is about the query",
		},
		{
			name: "precision intents keep their resolved must read", intent: search.IntentGoToDef,
			target:       TargetResolution{Status: search.TargetStatusInferredSymbol, Confidence: 0.95, Selected: &search.TargetCandidate{FQN: "pkg.Retry", Path: "docs/popular.md"}},
			availability: AvailabilityComplete, relevance: RelevanceUseful, eligibility: EvidencePrimary, relationship: "definition",
			evidence: graphEvidence, score: 0.5, wantMustRead: true, wantReason: "no source has identity-level or corroborated support",
		},
		{
			name: "medium confidence is untouched", intent: search.IntentSearch,
			availability: AvailabilityPartial, relevance: RelevanceStrong, eligibility: EvidenceSupporting,
			evidence: graphEvidence, score: 0.84, wantMustRead: true, wantReason: "relevant evidence is useful but incomplete",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			source := SourceAssessment{
				Result: search.RankedResult{Candidate: search.Candidate{
					Handle: knowledge.NoteHandle("docs/popular.md"), Path: "docs/popular.md", Title: "Popular", Type: "note",
					Evidence: testCase.evidence,
				}, FinalScore: testCase.score},
				Relevance: testCase.relevance, Eligibility: testCase.eligibility, Relationship: testCase.relationship,
				SupportedRoles: []string{"documentation"},
			}
			packet := BuildAssessedAnswer(testCase.intent, query, testCase.target, nil, testCase.availability, []SourceAssessment{source})

			require.Equal(t, testCase.wantReason, packet.Confidence.Reason)
			if testCase.wantMustRead {
				require.Len(t, packet.MustRead, 1)
				require.Equal(t, "docs/popular.md", packet.MustRead[0].Path)
				return
			}
			require.Empty(t, packet.MustRead)
			require.Len(t, packet.Supporting, 1)
			require.Equal(t, "docs/popular.md", packet.Supporting[0].Path)
		})
	}
}
