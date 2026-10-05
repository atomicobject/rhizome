package indexingperf

// Agent-start diagnostic labels are a machine-facing contract. Keep the lists
// additive and ordered so benchmark output remains comparable across releases.
const (
	AgentStartPhaseTotal             = "agent_start.total"
	AgentStartPhaseRuntimeSearch     = "agent_start.runtime.search_ready"
	AgentStartPhaseRuntimeSemantic   = "agent_start.runtime.semantic_ready"
	AgentStartPhaseRuntimeCode       = "agent_start.runtime.code_ready"
	AgentStartPhaseStoreWarmOpen     = "agent_start.store.warm_open"
	AgentStartPhaseNoteCrawl         = "agent_start.note.crawl"
	AgentStartPhaseCodeRefDiscovery  = "agent_start.coderef.discovery_read"
	AgentStartPhaseRepoDocInventory  = "agent_start.repo_doc.inventory"
	AgentStartPhaseVaultContext      = "agent_start.vault_context"
	AgentStartPhaseTargetFileContext = "agent_start.target_file_context"
	AgentStartPhaseOntologyFreshness = "agent_start.ontology.freshness"
	AgentStartPhaseOntologySummary   = "agent_start.ontology.summary"
	AgentStartPhaseSessionReserve    = "agent_start.session.reserve"
	AgentStartPhaseSessionCommit     = "agent_start.session.commit"
	AgentStartPhaseSessionCleanup    = "agent_start.session.cleanup"

	AgentStartOpNotePasses                = "agent_start.note.passes"
	AgentStartOpRepoWalks                 = "agent_start.repo.walks"
	AgentStartOpNoteReads                 = "agent_start.note.reads"
	AgentStartOpCodeReads                 = "agent_start.code.reads"
	AgentStartOpSchemaStatements          = "agent_start.sqlite.schema_statements"
	AgentStartOpIntegrityChecks           = "agent_start.sqlite.integrity_checks"
	AgentStartOpTransactions              = "agent_start.sqlite.transactions"
	AgentStartOpIndexWrites               = "agent_start.index.writes"
	AgentStartOpSessionReserves           = "agent_start.session.reserves"
	AgentStartOpSessionCommits            = "agent_start.session.commits"
	AgentStartOpSessionReleases           = "agent_start.session.releases"
	AgentStartOpSessionCommitFailures     = "agent_start.session.commit_failures"
	AgentStartOpSessionReleaseFailures    = "agent_start.session.release_failures"
	AgentStartOpSessionCleanups           = "agent_start.session.cleanups"
	AgentStartOpIndexedReads              = "agent_start.indexed_enrichment.reads"
	AgentStartOpIndexedResults            = "agent_start.indexed_enrichment.results"
	AgentStartOpIndexedStatusAvailable    = "agent_start.indexed_enrichment.status.available"
	AgentStartOpIndexedStatusStale        = "agent_start.indexed_enrichment.status.stale"
	AgentStartOpIndexedStatusIncompatible = "agent_start.indexed_enrichment.status.incompatible"
	AgentStartOpIndexedStatusMissing      = "agent_start.indexed_enrichment.status.missing"
)

var agentStartPhaseDescriptors = [...]string{
	AgentStartPhaseTotal,
	AgentStartPhaseRuntimeSearch,
	AgentStartPhaseRuntimeSemantic,
	AgentStartPhaseRuntimeCode,
	AgentStartPhaseStoreWarmOpen,
	AgentStartPhaseNoteCrawl,
	AgentStartPhaseCodeRefDiscovery,
	AgentStartPhaseRepoDocInventory,
	AgentStartPhaseVaultContext,
	AgentStartPhaseTargetFileContext,
	AgentStartPhaseOntologyFreshness,
	AgentStartPhaseOntologySummary,
	AgentStartPhaseSessionReserve,
	AgentStartPhaseSessionCommit,
	AgentStartPhaseSessionCleanup,
}

var agentStartOperationDescriptors = [...]string{
	AgentStartOpNotePasses,
	AgentStartOpRepoWalks,
	AgentStartOpNoteReads,
	AgentStartOpCodeReads,
	AgentStartOpSchemaStatements,
	AgentStartOpIntegrityChecks,
	AgentStartOpTransactions,
	AgentStartOpIndexWrites,
	AgentStartOpSessionReserves,
	AgentStartOpSessionCommits,
	AgentStartOpSessionReleases,
	AgentStartOpSessionCommitFailures,
	AgentStartOpSessionReleaseFailures,
	AgentStartOpSessionCleanups,
	AgentStartOpIndexedReads,
	AgentStartOpIndexedResults,
	AgentStartOpIndexedStatusAvailable,
	AgentStartOpIndexedStatusStale,
	AgentStartOpIndexedStatusIncompatible,
	AgentStartOpIndexedStatusMissing,
}

type AgentStartDiagnostics struct {
	Phases     []AgentStartPhaseDiagnostic     `json:"phases"`
	Operations []AgentStartOperationDiagnostic `json:"operations"`
}

type AgentStartPhaseDiagnostic struct {
	Label      string `json:"label"`
	DurationMs int64  `json:"durationMs"`
	Count      int64  `json:"count"`
}

type AgentStartOperationDiagnostic struct {
	Label     string `json:"label"`
	Count     int64  `json:"count"`
	Available bool   `json:"available"`
}

func (c *Collector) AgentStartDiagnostics() AgentStartDiagnostics {
	out := AgentStartDiagnostics{
		Phases:     make([]AgentStartPhaseDiagnostic, 0, len(agentStartPhaseDescriptors)),
		Operations: make([]AgentStartOperationDiagnostic, 0, len(agentStartOperationDescriptors)),
	}
	if c == nil {
		return out
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, label := range agentStartPhaseDescriptors {
		span := c.spans[label]
		out.Phases = append(out.Phases, AgentStartPhaseDiagnostic{Label: label, DurationMs: span.Duration.Milliseconds(), Count: span.Count})
	}
	for _, label := range agentStartOperationDescriptors {
		var total int64
		for metric, counter := range c.counters {
			if metric.Name == label {
				total += counter.Total
			}
		}
		// Full schema recovery intentionally remains an unbounded fallback. A
		// schema-statement count is available only when the validated warm probe
		// reports its complete, bounded statement budget.
		available := (label != AgentStartOpSchemaStatements && label != AgentStartOpIntegrityChecks) || total > 0
		out.Operations = append(out.Operations, AgentStartOperationDiagnostic{Label: label, Count: total, Available: available})
	}
	return out
}
