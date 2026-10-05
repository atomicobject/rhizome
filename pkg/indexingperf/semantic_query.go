package indexingperf

import "time"

// Semantic-query diagnostic labels are a machine-facing, additive contract.
// Keep the descriptor order stable so benchmark reports remain comparable.
const (
	SemanticQueryPhaseTotal           = "semantic_query.total"
	SemanticQueryPhaseBootstrap       = "semantic_query.bootstrap"
	SemanticQueryPhaseRuntimeSearch   = "semantic_query.runtime.search_ready"
	SemanticQueryPhaseRuntimeSemantic = "semantic_query.runtime.semantic_ready"
	SemanticQueryPhaseRuntimeCode     = "semantic_query.runtime.code_ready"
	SemanticQueryPhaseNoteCache       = "semantic_query.note_cache"
	SemanticQueryPhaseHandler         = "semantic_query.handler"
	SemanticQueryPhaseSeedResolution  = "semantic_query.seed_resolution"
	SemanticQueryPhaseStoreOpen       = "semantic_query.store_open"
	SemanticQueryPhaseSearch          = "semantic_query.search"
	SemanticQueryPhaseShaping         = "semantic_query.shaping"
	SemanticQueryPhaseMarshal         = "semantic_query.marshal"
	SemanticQueryPhaseSession         = "semantic_query.session"

	SemanticQueryOpNotePasses            = "semantic_query.note.passes"
	SemanticQueryOpRepoWalks             = "semantic_query.repo.walks"
	SemanticQueryOpNoteReads             = "semantic_query.note.reads"
	SemanticQueryOpCodeReads             = "semantic_query.code.reads"
	SemanticQueryOpBodyReads             = "semantic_query.body.reads"
	SemanticQueryOpBodyReadBytes         = "semantic_query.body.read_bytes"
	SemanticQueryOpFallbackStoreOpens    = "semantic_query.sqlite.fallback_store_opens"
	SemanticQueryOpSchemaStatements      = "semantic_query.sqlite.schema_statements"
	SemanticQueryOpIntegrityChecks       = "semantic_query.sqlite.integrity_checks"
	SemanticQueryOpIndexWrites           = "semantic_query.index.writes"
	SemanticQueryOpProviderCalls         = "semantic_query.provider.calls"
	SemanticQueryOpVectorKNNQueries      = "semantic_query.vector.knn_queries"
	SemanticQueryOpVectorTieRetries      = "semantic_query.vector.tie_retries"
	SemanticQueryOpVectorScalarFallbacks = "semantic_query.vector.scalar_fallbacks"
)

var semanticQueryPhaseDescriptors = [...]string{
	SemanticQueryPhaseTotal,
	SemanticQueryPhaseBootstrap,
	SemanticQueryPhaseRuntimeSearch,
	SemanticQueryPhaseRuntimeSemantic,
	SemanticQueryPhaseRuntimeCode,
	SemanticQueryPhaseNoteCache,
	SemanticQueryPhaseHandler,
	SemanticQueryPhaseSeedResolution,
	SemanticQueryPhaseStoreOpen,
	SemanticQueryPhaseSearch,
	SemanticQueryPhaseShaping,
	SemanticQueryPhaseMarshal,
	SemanticQueryPhaseSession,
}

type semanticQueryOperationDescriptor struct {
	label           string
	source          string
	alwaysAvailable bool
}

var semanticQueryOperationDescriptors = [...]semanticQueryOperationDescriptor{
	{SemanticQueryOpNotePasses, AgentStartOpNotePasses, true},
	{SemanticQueryOpRepoWalks, AgentStartOpRepoWalks, true},
	{SemanticQueryOpNoteReads, AgentStartOpNoteReads, true},
	{SemanticQueryOpCodeReads, AgentStartOpCodeReads, true},
	{SemanticQueryOpBodyReads, SemanticQueryOpBodyReads, true},
	{SemanticQueryOpBodyReadBytes, SemanticQueryOpBodyReadBytes, true},
	{SemanticQueryOpFallbackStoreOpens, SemanticQueryOpFallbackStoreOpens, true},
	{SemanticQueryOpSchemaStatements, AgentStartOpSchemaStatements, false},
	{SemanticQueryOpIntegrityChecks, AgentStartOpIntegrityChecks, false},
	{SemanticQueryOpIndexWrites, AgentStartOpIndexWrites, false},
	{SemanticQueryOpProviderCalls, "provider.calls", false},
	{SemanticQueryOpVectorKNNQueries, SemanticQueryOpVectorKNNQueries, true},
	{SemanticQueryOpVectorTieRetries, SemanticQueryOpVectorTieRetries, true},
	{SemanticQueryOpVectorScalarFallbacks, SemanticQueryOpVectorScalarFallbacks, true},
}

type SemanticQueryDiagnostics struct {
	Phases     []SemanticQueryPhaseDiagnostic     `json:"phases"`
	Operations []SemanticQueryOperationDiagnostic `json:"operations"`
}

type SemanticQueryPhaseDiagnostic struct {
	Label      string `json:"label"`
	DurationMs int64  `json:"durationMs"`
	Count      int64  `json:"count"`
	Available  bool   `json:"available"`
}

type SemanticQueryOperationDiagnostic struct {
	Label     string `json:"label"`
	Count     int64  `json:"count"`
	Available bool   `json:"available"`
}

func NewSemanticQueryCollector() *Collector {
	collector := New()
	allowedMetrics := map[string]bool{}
	for _, descriptor := range semanticQueryOperationDescriptors {
		allowedMetrics[descriptor.source] = true
	}
	collector.metricAllowed = func(metric Metric) bool {
		return allowedMetrics[metric.Name]
	}
	collector.spanAllowed = func(name string) bool {
		for _, label := range semanticQueryPhaseDescriptors {
			if name == label {
				return true
			}
		}
		return false
	}
	collector.collectDBWrites = false
	return collector
}

func (c *Collector) SemanticQueryDiagnostics() SemanticQueryDiagnostics {
	out := SemanticQueryDiagnostics{
		Phases:     make([]SemanticQueryPhaseDiagnostic, 0, len(semanticQueryPhaseDescriptors)),
		Operations: make([]SemanticQueryOperationDiagnostic, 0, len(semanticQueryOperationDescriptors)),
	}
	if c == nil {
		return out
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for _, label := range semanticQueryPhaseDescriptors {
		if label == SemanticQueryPhaseTotal {
			out.Phases = append(out.Phases, SemanticQueryPhaseDiagnostic{
				Label: label, DurationMs: now.Sub(c.startedAt).Milliseconds(), Count: 1, Available: true,
			})
			continue
		}
		span, available := c.spans[label]
		out.Phases = append(out.Phases, SemanticQueryPhaseDiagnostic{
			Label: label, DurationMs: span.Duration.Milliseconds(), Count: span.Count, Available: available,
		})
	}
	for _, descriptor := range semanticQueryOperationDescriptors {
		var total int64
		available := descriptor.alwaysAvailable
		for metric, counter := range c.counters {
			if metric.Name == descriptor.source {
				total += counter.Total
				available = true
			}
		}
		out.Operations = append(out.Operations, SemanticQueryOperationDiagnostic{
			Label: descriptor.label, Count: total, Available: available,
		})
	}
	return out
}
