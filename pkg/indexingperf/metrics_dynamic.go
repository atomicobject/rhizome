package indexingperf

import (
	"fmt"
	"slices"
)

// MetricFamily is a finite set of identities. Select rejects misspelled internal
// reasons; open label families map unknown labels to their explicit other bucket.
type MetricFamily struct {
	pattern  string
	choices  [][]string
	classify bool
}

func (f MetricFamily) Select(values ...string) MetricName {
	if len(values) != len(f.choices) {
		panic("incorrect metric selector arity: " + f.pattern)
	}
	args := make([]any, len(values))
	for i, value := range values {
		if !slices.Contains(f.choices[i], value) {
			if !f.classify || !slices.Contains(f.choices[i], "other") {
				panic("unknown metric selector for " + f.pattern + ": " + value)
			}
			value = "other"
		}
		args[i] = value
	}
	return MetricName(fmt.Sprintf(f.pattern, args...))
}

var (
	providerMetricPrefixes  = []string{"provider.request.openai", "provider.request.voyage", "provider.request.ollama"}
	providerFailureLabels   = []string{"canceled", "timeout", "dns", "transient", "other"}
	codeMetricKinds         = []string{"function", "method", "class", "interface", "struct", "type", "enum", "variable", "constant", "field", "property", "constructor", "module", "namespace", "file", "note", "doc_section", "ontology_node", "unknown", "other"}
	codeMetricGranularities = []string{"symbol", "module", "file", "section", "node_body", "node_body_visible", "node_body_supplemental", "unknown", "other"}
	searchOutcomeLabels     = []string{"ok", "error", "canceled", "timeout", "partial", "success", "failed", "timed_out", "unknown", "other"}
	searchRetrieverLabels   = []string{"anchor_graph", "auto_expand", "auto_expand.diffusion", "auto_expand.graph", "auto_expand.ontology", "auto_expand.refs", "call_edges", "code_anchor_notes", "code_anchor_refs", "composite", "definition", "diffusion", "doc_links", "explicit_seeds", "graph", "graph_ppr", "implementers", "intel_lexical", "link_text", "local_docs", "note_lexical", "ontology", "other", "outgoing_links", "rationale_fts", "refs", "seed_vector", "symbol_probe", "tests_for_code", "vector"}

	WatcherDuration = MetricFamily{pattern: "%s", choices: [][]string{{"derived_retry", "derived_prepare", "derived_publication"}}}

	CallEdgeCacheLookup        = MetricFamily{pattern: "%s.%s", choices: [][]string{{"calledge.cache.fqn", "calledge.cache.import", "calledge.cache.go_method", "calledge.cache.py_fallback"}, {"hit", "miss"}}, classify: false}
	CallEdgeSourceFallback     = MetricFamily{pattern: "calledge.source_fallback.%s.count", choices: [][]string{{"runtime_missing_refs", "runtime_parse_untrusted", "no_authoritative_inputs"}}, classify: false}
	CallEdgeSuffixSkip         = MetricFamily{pattern: "calledge.cache_build_suffix_skipped.%s.count", choices: [][]string{{"go_method_only", "py_fallback_only", "bare_name", "unqualified_pkg"}}, classify: false}
	CallEdgeSuffixLanguage     = MetricFamily{pattern: "calledge.cache_build_suffix.candidate.%s.count", choices: [][]string{{"go", "py", "cs", "ts", "js", "java", "rust", "ruby", "cpp", "c", "other"}}, classify: true}
	CallEdgeSuffixKind         = MetricFamily{pattern: "calledge.cache_build_suffix.%s_candidate.count", choices: [][]string{{"call", "typeref", "memberref"}}, classify: false}
	ProviderRequestMetric      = MetricFamily{pattern: "%s.%s", choices: [][]string{providerMetricPrefixes, {"attempt", "sent_bytes", "attempt_latency", "body_read", "received_bytes", "backoff_scheduled", "backoff"}}, classify: false}
	ProviderHTTPStatus         = MetricFamily{pattern: "%s.status.%s", choices: [][]string{providerMetricPrefixes, httpMetricStatuses()}, classify: false}
	ProviderTransport          = MetricFamily{pattern: "%s.transport.%s", choices: [][]string{providerMetricPrefixes, providerFailureLabels}, classify: false}
	ProviderBodyReadError      = MetricFamily{pattern: "%s.body_read.error.%s", choices: [][]string{providerMetricPrefixes, providerFailureLabels}, classify: false}
	ProviderLogicalOutcome     = MetricFamily{pattern: "provider.logical.%s.outcome.%s", choices: [][]string{{"openai", "voyage", "ollama"}, {"ok", "error", "canceled", "timeout", "dns", "transient", "other"}}, classify: false}
	ProviderBackoffReason      = MetricFamily{pattern: "%s.backoff.reason.%s", choices: [][]string{providerMetricPrefixes, {"http", "network"}}, classify: false}
	ProviderBackoffInterrupted = MetricFamily{pattern: "%s.backoff.interrupted.%s", choices: [][]string{providerMetricPrefixes, providerFailureLabels}, classify: false}
	ProviderRetry              = MetricFamily{pattern: "provider.request.%s.retry.%s", choices: [][]string{{"openai", "voyage", "ollama"}, {"http", "network"}}, classify: false}
	VoyageSplitReason          = MetricFamily{pattern: "provider.request.voyage.split.reason.%s", choices: [][]string{providerFailureLabels}, classify: false}
	ProviderCacheMissReason    = MetricFamily{pattern: "provider.cache.miss.reason.%s", choices: [][]string{{"read_error", "absent", "dimensions", "invalid_vector"}}, classify: false}
	NoteEmbeddingResetReason   = MetricFamily{pattern: "noteemb.reset.reason.%s", choices: [][]string{{"dimensions_mismatch", "provider_mismatch", "model_mismatch"}}, classify: false}
	QueueFlushOutcome          = MetricFamily{pattern: "queue.flush.outcome.%s", choices: [][]string{{"success", "canceled", "error"}}, classify: false}
	DerivedRecovery            = MetricFamily{pattern: "derived.recovery.%s", choices: [][]string{{"graph", "code", "notes", "ontology"}}, classify: false}
	EmbeddingReadyState        = MetricFamily{pattern: "embed.ready_state.%s", choices: [][]string{{"empty", "nonzero"}}, classify: false}
	CodeEmbeddingNeed          = MetricFamily{pattern: "codeembed.need.%s", choices: [][]string{{"pruned_all_chunks", "no_chunks", "unchanged", "chunk_hash_mismatch", "legacy_missing_item_hash", "first_build_missing_state", "missing_item_hash", "item_hash_mismatch", "unchanged_hash_fallback"}}, classify: false}
	SuppressedAnchor           = MetricFamily{pattern: "semantic.suppressed_anchors.%s", choices: [][]string{codeMetricKinds}, classify: true}
	PrunedStaleChunks          = MetricFamily{pattern: "semantic.pruned_stale_chunks.%s", choices: [][]string{codeMetricKinds}, classify: true}
	CodeChunksPruned           = MetricFamily{pattern: "semantic.code_chunks_pruned.%s", choices: [][]string{codeMetricKinds}, classify: true}
	CodeChunkKind              = MetricFamily{pattern: "%s.kind.%s", choices: [][]string{{"semantic.code_chunks", "semantic.code_plan"}, codeMetricKinds}, classify: true}
	CodeChunkGranularity       = MetricFamily{pattern: "%s.granularity.%s", choices: [][]string{{"semantic.code_chunks", "semantic.code_plan"}, codeMetricGranularities}, classify: true}
	CodeChunkKindGranularity   = MetricFamily{pattern: "%s.kind_granularity.%s.%s", choices: [][]string{{"semantic.code_chunks", "semantic.code_plan"}, codeMetricKinds, codeMetricGranularities}, classify: true}
	WritebackMetric            = MetricFamily{pattern: "%s.%s", choices: [][]string{{"noteplan.writeback.meta", "validation.writeback.metadata", "noteembed.writeback.chunk", "noteembed.writeback.intel", "codeembed.writeback.item", "codeembed.writeback.chunk", "codeembed.writeback.intel"}, {"submit_wait", "queue_depth", "flush.count", "batch_rows", "batch_bytes", "flush"}}, classify: false}
	SearchOutcome              = MetricFamily{pattern: "search.outcome.%s", choices: [][]string{searchOutcomeLabels}, classify: false}
	SearchFallback             = MetricFamily{pattern: "search.fallback.%s.%s", choices: [][]string{{"plan", "retrieve", "repair", "rerank", "rank", "rollup", "shaping", "adjust", "pack"}, searchOutcomeLabels}, classify: false}
	SearchStageOutcome         = MetricFamily{pattern: "search.stage.%s.outcome.%s", choices: [][]string{{"adjust", "anchor_graph", "auto_expand", "auto_expand.base", "auto_expand.diffusion", "auto_expand.graph", "auto_expand.ontology", "auto_expand.refs", "call_edges", "code_anchor_notes", "code_anchor_refs", "composite", "definition", "diffusion", "doc_links", "explicit_seeds", "graph", "graph.filesystem", "graph.snapshot.analyze", "graph.snapshot.load", "graph.store.communities", "graph.store.edges", "graph.store.freshness", "graph.store.metadata", "graph.store.seed_scores", "graph_ppr", "graph_ppr.diffuse", "graph_ppr.induced_graph", "implementers", "intel_lexical", "link_text", "local_docs", "note_lexical", "ontology", "other", "outgoing_links", "pack", "pack_fallback", "plan", "rank", "rank_fallback", "rationale_fts", "refs", "repair", "rerank", "resolve", "retrieval", "retrieve", "rollup", "search", "seed", "seed_vector", "semantic.embed.code", "semantic.embed.note", "semantic.embed.shared", "shape", "shaping", "symbol_probe", "tests_for_code", "vector"}, searchOutcomeLabels}, classify: true}
	SearchWarning              = MetricFamily{pattern: "search.warning.%s", choices: [][]string{{"index_unavailable", "indexed_graph_unavailable", "inferred_seed_paths", "missing_call_edges", "missing_definition", "missing_tests", "other", "overview_representative_code", "precision_fallback_blocked", "ranking_degraded", "rerank_unavailable", "retriever_degraded", "subsystem_overview_downgraded", "subsystem_overview_fallback", "target_ambiguous", "target_speculative", "target_unresolved", "typo_fallback"}}, classify: true}
	SearchRetrieverOutcome     = MetricFamily{pattern: "search.retriever.%s.outcome.%s", choices: [][]string{searchRetrieverLabels, {"ran", "empty", "skipped", "timed_out", "degraded", "canceled", "unknown", "other"}}, classify: true}
	SearchRetrieverResults     = MetricFamily{pattern: "search.retriever.%s.results", choices: [][]string{searchRetrieverLabels}, classify: true}
	WatcherTrigger             = MetricFamily{pattern: "watcher.trigger.%s", choices: [][]string{{"cache_recrawl_completed", "unready_derived_work", "directory_event", "directory_rename", "directory_create", "file_rename", "file_deleted", "file_changed", "file_created", "file_modified", "file_removed", "file_renamed", "file_recreated", "file_unknown", "stale_watcher_error", "stale_overflow", "stale_ignore_changed", "stale_dir_renamed", "stale_dir_created", "stale_hint_resync", "stale_unknown", "source_fsnotify", "source_hint", "source_unknown", "unspecified_resync", "subscription_resync", "backend_resync", "overflow", "watcher_error", "watcher_closed", "startup", "other"}}, classify: true}
	WatcherFullDiscovery       = MetricFamily{pattern: "watcher.full_discovery.%s", choices: [][]string{{"resync", "pending_reconciliation", "selection_config", "metadata_change", "ontology_change"}}, classify: false}
)

func httpMetricStatuses() []string {
	values := []string{"other"}
	for code := 100; code <= 599; code++ {
		values = append(values, fmt.Sprint(code))
	}
	return values
}
