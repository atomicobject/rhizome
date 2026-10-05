package indexingperf

import (
	"fmt"
	"strings"
	"time"
)

func (c *Collector) renderWindowPhaseLine(window time.Duration, phase string) string {
	providerMS := c.windowLatencyDeltaPhase(phase, "provider.latency")
	prepMS := c.windowLatencyDeltaPhase(phase, "codeembed.prepare") + c.windowLatencyDeltaPhase(phase, "noteembed.prepare")
	prepareBusyMS := c.windowLatencyDeltaPhase(phase, "node.prepare.busy")
	prepareBlockedMS := c.windowLatencyDeltaPhase(phase, "node.prepare.blocked")
	embedStarvedMS := c.windowLatencyDeltaPhase(phase, "node.embed.starved")
	embedPackWaitMS := c.windowLatencyDeltaPhase(phase, "node.embed.pack_wait")
	freeSlotsUnderMinTextsMS := c.windowLatencyDeltaPhase(phase, "node.embed.free_slots_under_mintexts")
	writerIdleMS := c.windowLatencyDeltaPhase(phase, "node.write.starved")
	writerBusyMS := c.windowLatencyDeltaPhase(phase, "node.write.busy")
	writerDeferredMS := c.windowLatencyDeltaPhase(phase, "node.write.deferred")
	prepareIn := c.windowCounterDeltaPhase(phase, "node.prepare.in")
	prepareOut := c.windowCounterDeltaPhase(phase, "node.prepare.out")
	embedIn := c.windowCounterDeltaPhase(phase, "node.embed.in")
	embedOut := c.windowCounterDeltaPhase(phase, "node.embed.out")
	writerIn := c.windowCounterDeltaPhase(phase, "node.write.in")
	writerOut := c.windowCounterDeltaPhase(phase, "node.write.out")
	providerCalls := c.windowCounterDeltaPhase(phase, "provider.calls")
	providerTexts := c.windowCounterDeltaPhase(phase, "provider.texts")
	providerLogicalTexts := c.windowCounterDeltaPhase(phase, "provider.logical_texts")
	voyageRequests := c.windowCounterDeltaPhase(phase, "provider.request.voyage")
	voyageAttempts := c.windowCounterDeltaPhase(phase, "provider.request.voyage.attempt")
	voyageNetworkFailures := c.windowCounterDeltaPhase(phase, "provider.request.voyage.network_failures")
	voyageHTTPRetries := c.windowCounterDeltaPhase(phase, "provider.request.voyage.http_retry")
	voyageSplits := c.windowCounterDeltaPhase(phase, "provider.request.voyage.split")
	voyageCompleted := c.windowCounterDeltaPhase(phase, "provider.request.voyage.completed")
	voyageBodyBytes := c.windowCounterDeltaPhase(phase, "provider.request.voyage.body_bytes")
	voyageBackoff := c.windowLatencyDeltaPhase(phase, "provider.request.voyage.backoff")
	voyageInputsP50, voyageInputsP95, voyageInputsMax := c.windowSamplePercentilesPhase(phase, "provider.request.voyage.inputs")
	voyageStatusP50, voyageStatusP95, voyageStatusMax := c.windowSamplePercentilesPhase(phase, "provider.request.voyage.status")
	voyageStatusCount := c.windowSampleCountPhase(phase, "provider.request.voyage.status")
	discovered := c.windowCounterDeltaPhase(phase, "fs.discovered")
	completed := c.windowCounterDeltaPhase(phase, "fs.completed")
	queueWaitMS := c.windowLatencyDeltaPhase(phase, "queue.wait")
	codeQueueWaitMS := c.windowLatencyDeltaPhase(phase, "queue.code_wait")
	otherQueueWaitMS := c.windowLatencyDeltaPhase(phase, "queue.other_wait")
	dbWaitMS := c.windowLatencyDeltaPhase(phase, "db.wait")
	dbHoldMS := c.windowLatencyDeltaPhase(phase, "db.hold")
	gateWaitMS := c.windowLatencyDeltaPhase(phase, "embed.gate_wait")
	futureWaitMS := c.windowLatencyDeltaPhase(phase, "embed.future_wait")
	walkMS := c.windowLatencyDeltaPhase(phase, "fs.walk")
	readLatencyMS := c.windowLatencyDeltaPhase(phase, "fs.read_latency")
	statMS := c.windowLatencyDeltaPhase(phase, "fs.stat")
	files := c.windowCounterDeltaPhase(phase, "fs.read")
	chunks := c.windowCounterDeltaPhase(phase, "semantic.chunks")
	suppressedAnchors := c.windowCounterDeltaPrefixPhase(phase, "semantic.suppressed_anchors.")
	codeChunksPruned := c.windowCounterDeltaPrefixPhase(phase, "semantic.code_chunks_pruned.")
	codePlanKind := c.windowCounterDeltaPrefixPhase(phase, "semantic.code_plan.kind.")
	codePlanGranularity := c.windowCounterDeltaPrefixPhase(phase, "semantic.code_plan.granularity.")
	codeEmbeddedKind := c.windowCounterDeltaPrefixPhase(phase, "semantic.code_chunks.kind.")
	codeEmbeddedGranularity := c.windowCounterDeltaPrefixPhase(phase, "semantic.code_chunks.granularity.")
	noteRawEligiblePaths := c.windowCounterDeltaPhase(phase, "noteplan.raw_eligible_paths")
	noteRawSkippedPaths := c.windowCounterDeltaPhase(phase, "noteplan.raw_skipped_paths")
	ontologyTypedProjections := c.windowCounterDeltaPhase(phase, "ontology_nodes.typed_projections_planned")
	ontologyFallbackProjections := c.windowCounterDeltaPhase(phase, "ontology_nodes.fallback_projections_planned")
	ontologyBodyChunksPlanned := c.windowCounterDeltaPhase(phase, "ontology_nodes.body_chunks_planned")
	ontologyChunksReused := c.windowCounterDeltaPhase(phase, "ontology_nodes.chunks_reused")
	ontologyChunksEmbedded := c.windowCounterDeltaPhase(phase, "ontology_nodes.chunks_embedded")
	ontologyFieldRowsPlanned := c.windowCounterDeltaPhase(phase, "ontology.field_rows_planned")
	ontologyFieldRowsWritten := c.windowCounterDeltaPhase(phase, "ontology.field_rows_written")
	queueDepthPeak := c.windowGaugePeakPhase(phase, "queue.depth")
	queueDepthAvg := c.windowGaugeAveragePhase(phase, "queue.depth")
	codeQueueDepthPeak := c.windowGaugePeakPhase(phase, "queue.code_depth")
	codeQueueDepthAvg := c.windowGaugeAveragePhase(phase, "queue.code_depth")
	otherQueueDepthPeak := c.windowGaugePeakPhase(phase, "queue.other_depth")
	otherQueueDepthAvg := c.windowGaugeAveragePhase(phase, "queue.other_depth")
	fileQueueDepthPeak := c.windowGaugePeakPhase(phase, "codeindex.file_queue_depth")
	fileQueueDepthAvg := c.windowGaugeAveragePhase(phase, "codeindex.file_queue_depth")
	readyDepthPeak := c.windowGaugePeakPhase(phase, "embed.ready_depth")
	readyDepthAvg := c.windowGaugeAveragePhase(phase, "embed.ready_depth")
	providerInflightAvg := c.windowGaugeAveragePhase(phase, "provider.inflight")
	providerInflightPeak := c.windowGaugePeakPhase(phase, "provider.inflight")
	providerInflightCurrent := c.currentGaugePhase(phase, "provider.inflight")
	intentPendingRows := c.currentGaugePhase(phase, "intent.writeback.pending_rows")
	intentPendingPeak := c.windowGaugePeakPhase(phase, "intent.writeback.pending_rows")
	embedQueueAgeAvg := c.windowLatencyAvgPhase(phase, "embed.queue_age")
	embedQueueAgeMax := c.phaseLatencyMax(phase, "embed.queue_age")
	batchOldestAgeMax := c.phaseLatencyMax(phase, "embed.batch_oldest_age")
	dispatchIdleMS := c.windowLatencyDeltaPhase(phase, "provider.dispatch_idle")
	batchTextsP50, batchTextsP95, batchTextsMax := c.windowSamplePercentilesPhase(phase, "provider.batch_texts")
	batchBytesP50, batchBytesP95, batchBytesMax := c.windowSamplePercentilesPhase(phase, "provider.batch_bytes")
	batchFillP50, batchFillP95, batchFillMax := c.windowSamplePercentilesPhase(phase, "provider.batch_fill_ratio")
	bytesFillP50, bytesFillP95, bytesFillMax := c.windowSamplePercentilesPhase(phase, "provider.bytes_fill_ratio")
	providerLatencyP50, providerLatencyP95, providerLatencyMax := c.windowLatencyWindowPercentilesPhase(phase, "provider.latency")
	futureWaitP50, futureWaitP95, futureWaitMax := c.windowLatencyWindowPercentilesPhase(phase, "embed.future_wait")
	_, _, providerCapTextsMax := c.windowSamplePercentilesPhase(phase, "provider.cap_texts")
	_, _, providerDispatchFloorTextsMax := c.windowSamplePercentilesPhase(phase, "provider.dispatch_floor_texts")
	callsNeededTexts := providerLogicalTexts
	if callsNeededTexts == 0 {
		callsNeededTexts = providerTexts
	}
	providerCallsNeededAtProviderCap := ceilDivInt64(callsNeededTexts, providerCapTextsMax)
	providerCallsNeededAtDispatchFloor := ceilDivInt64(callsNeededTexts, providerDispatchFloorTextsMax)
	hashPrefetchMS := c.windowLatencyDeltaPhase(phase, "codeembed.hash_prefetch")
	loadAnchorMetaMS := c.windowLatencyDeltaPhase(phase, "plan.load_anchor_meta")
	loadAnchorsMS := c.windowLatencyDeltaPhase(phase, "plan.load_anchors")
	loadAnchorsByPathMS := c.windowLatencyDeltaPhase(phase, "plan.load_anchors_by_path")
	listItemsMS := c.windowLatencyDeltaPhase(phase, "plan.list_items")
	itemEmbeddingStatesMS := c.windowLatencyDeltaPhase(phase, "plan.item_embedding_states")
	callLookupFilesMS := c.windowLatencyDeltaPhase(phase, "plan.call_lookup_files")
	callLookupOwnersMS := c.windowLatencyDeltaPhase(phase, "plan.call_lookup_owners")
	noteLoadDocSectionMetaMS := c.windowLatencyDeltaPhase(phase, "noteplan.load_doc_section_meta")
	noteLoadDocSectionsMS := c.windowLatencyDeltaPhase(phase, "noteplan.load_doc_sections")
	noteLoadDocSectionsByPathMS := c.windowLatencyDeltaPhase(phase, "noteplan.load_doc_sections_by_path")
	noteLoadTitlesMS := c.windowLatencyDeltaPhase(phase, "noteplan.load_titles")
	noteChunkHashesBatchMS := c.windowLatencyDeltaPhase(phase, "noteplan.chunk_hashes_batch")
	noteChunkHashLookupMS := c.windowLatencyDeltaPhase(phase, "noteplan.chunk_hash_lookup")
	noteBuildChunksMS := c.windowLatencyDeltaPhase(phase, "noteplan.build_chunks")
	notePlanChunkUpdatesMS := c.windowLatencyDeltaPhase(phase, "noteplan.plan_chunk_updates")
	noteLoadStoredChunksMS := c.windowLatencyDeltaPhase(phase, "noteplan.load_stored_chunks")
	noteCacheLookupMS := c.windowLatencyDeltaPhase(phase, "noteplan.cache_lookup")
	noteMetaSubmitMS := c.windowLatencyDeltaPhase(phase, "noteplan.meta_submit")
	noteMetaFlushWaitMS := c.windowLatencyDeltaPhase(phase, "noteplan.meta_flush_wait")
	noteFinalizeMS := c.windowLatencyDeltaPhase(phase, "noteembed.finalize")
	noteFinalizeP50, noteFinalizeP95, noteFinalizeMax := c.windowLatencyWindowPercentilesPhase(phase, "noteembed.finalize")
	noteFinalizeWritebackSubmitMS := c.windowLatencyDeltaPhase(phase, "noteembed.finalize.writeback_submit")
	noteFinalizeWriteChunksMS := c.windowLatencyDeltaPhase(phase, "noteembed.finalize.write_chunks")
	noteFinalizeWriteIntelMS := c.windowLatencyDeltaPhase(phase, "noteembed.finalize.write_intel")
	noteFinalizeCleanupMS := c.windowLatencyDeltaPhase(phase, "noteembed.finalize.cleanup")
	noteSyncChunkLookupMS := c.windowLatencyDeltaPhase(phase, "noteemb.sync_note_chunks.lookup_note_row")
	noteSyncChunkUpsertCacheMS := c.windowLatencyDeltaPhase(phase, "noteemb.sync_note_chunks.upsert_cache")
	noteSyncChunkUpsertRowsMS := c.windowLatencyDeltaPhase(phase, "noteemb.sync_note_chunks.upsert_rows")
	noteSyncChunkDeleteStaleMS := c.windowLatencyDeltaPhase(phase, "noteemb.sync_note_chunks.delete_stale")
	noteWritebackFlushWaitMS := c.windowLatencyDeltaPhase(phase, "noteembed.writeback.flush_wait")
	hashPrefetchCalls := c.windowCounterDeltaPhase(phase, "codeembed.hash_prefetch.calls")
	hashPrefetchHashes := c.windowCounterDeltaPhase(phase, "codeembed.hash_prefetch.hashes")
	hashPrefetchHits := c.windowCounterDeltaPhase(phase, "codeembed.hash_prefetch.hits")
	memReuseHits := c.windowCounterDeltaPhase(phase, "codeembed.reuse_mem.hit")
	memReuseMisses := c.windowCounterDeltaPhase(phase, "codeembed.reuse_mem.miss")
	hashPrefetchBatchP50, hashPrefetchBatchP95, hashPrefetchBatchMax := c.windowSamplePercentilesPhase(phase, "codeembed.hash_prefetch.batch")
	callEdgeFlushes := c.windowCounterDeltaPhase(phase, "calledge.flush.count")
	callEdgePathsP50, callEdgePathsP95, callEdgePathsMax := c.windowSamplePercentilesPhase(phase, "calledge.flush.paths")
	callEdgeWorkP50, callEdgeWorkP95, callEdgeWorkMax := c.windowSamplePercentilesPhase(phase, "calledge.flush.work")
	callEdgeRefsLoadMS := c.windowLatencyDeltaPhase(phase, "calledge.refs_load")
	callEdgeCacheBuildMS := c.windowLatencyDeltaPhase(phase, "calledge.cache_build")
	callEdgeCacheBuildFQNLookupMS := c.windowLatencyDeltaPhase(phase, "calledge.cache_build.fqn_lookup")
	callEdgeCacheBuildImportLookupMS := c.windowLatencyDeltaPhase(phase, "calledge.cache_build.import_lookup")
	callEdgeCacheBuildGoMethodLookupMS := c.windowLatencyDeltaPhase(phase, "calledge.cache_build.go_method_lookup")
	callEdgeCacheBuildPyFallbackLookupMS := c.windowLatencyDeltaPhase(phase, "calledge.cache_build.py_fallback_lookup")
	codeFileQueueWaitMS := c.windowLatencyDeltaPhase(phase, "codeindex.file_queue_wait")
	codeFileEnqueueWaitMS := c.windowLatencyDeltaPhase(phase, "codeindex.file_enqueue_wait")
	codeResultSubmitMS := c.windowLatencyDeltaPhase(phase, "codeindex.result_submit_wait")
	codeResultReduceMS := c.windowLatencyDeltaPhase(phase, "codeindex.result_reduce")
	codeResultFinalizeMS := c.windowLatencyDeltaPhase(phase, "codeindex.result_finalize")
	codeWorkerServiceMS := c.windowLatencyDeltaPhase(phase, "codeindex.worker_service")
	codeWorkerBuildMS := c.windowLatencyDeltaPhase(phase, "codeindex.worker_build")
	codeWorkerSubmitLocalMS := c.windowLatencyDeltaPhase(phase, "codeindex.worker_submit_local")
	codeWorkerSubmitSemanticMS := c.windowLatencyDeltaPhase(phase, "codeindex.worker_submit_semantic")
	codeWorkerSubmitWriteMS := c.windowLatencyDeltaPhase(phase, "codeindex.worker_submit_write")
	codeWorkerSubmitMS := c.windowLatencyDeltaPhase(phase, "codeindex.worker_submit")
	codeWorkerTotalMS := c.windowLatencyDeltaPhase(phase, "codeindex.worker_total")

	if providerMS == 0 && prepMS == 0 && prepareBusyMS == 0 && prepareBlockedMS == 0 &&
		embedStarvedMS == 0 && embedPackWaitMS == 0 && writerIdleMS == 0 && writerBusyMS == 0 &&
		providerCalls == 0 && providerTexts == 0 && providerInflightCurrent == 0 && intentPendingRows == 0 && intentPendingPeak == 0 &&
		voyageRequests == 0 && voyageAttempts == 0 && voyageNetworkFailures == 0 && voyageHTTPRetries == 0 && voyageSplits == 0 && voyageCompleted == 0 && voyageBackoff == 0 &&
		voyageStatusCount == 0 && queueWaitMS == 0 && dbWaitMS == 0 && dbHoldMS == 0 &&
		gateWaitMS == 0 && dispatchIdleMS == 0 && files == 0 && chunks == 0 && discovered == 0 && completed == 0 &&
		walkMS == 0 && readLatencyMS == 0 && statMS == 0 && hashPrefetchMS == 0 && memReuseHits == 0 &&
		memReuseMisses == 0 && freeSlotsUnderMinTextsMS == 0 && loadAnchorMetaMS == 0 &&
		loadAnchorsMS == 0 && loadAnchorsByPathMS == 0 && listItemsMS == 0 &&
		itemEmbeddingStatesMS == 0 && callLookupFilesMS == 0 && callLookupOwnersMS == 0 &&
		noteLoadDocSectionMetaMS == 0 && noteLoadDocSectionsMS == 0 && noteLoadDocSectionsByPathMS == 0 &&
		noteLoadTitlesMS == 0 && noteChunkHashesBatchMS == 0 && noteChunkHashLookupMS == 0 &&
		noteBuildChunksMS == 0 && notePlanChunkUpdatesMS == 0 && noteLoadStoredChunksMS == 0 &&
		noteCacheLookupMS == 0 && noteMetaSubmitMS == 0 && noteMetaFlushWaitMS == 0 &&
		noteFinalizeMS == 0 && noteFinalizeWritebackSubmitMS == 0 && noteFinalizeWriteChunksMS == 0 &&
		noteFinalizeWriteIntelMS == 0 && noteFinalizeCleanupMS == 0 &&
		noteSyncChunkLookupMS == 0 && noteSyncChunkUpsertCacheMS == 0 &&
		noteSyncChunkUpsertRowsMS == 0 && noteSyncChunkDeleteStaleMS == 0 &&
		noteWritebackFlushWaitMS == 0 && !c.windowWritebackHasActivity(phase) &&
		codeFileQueueWaitMS == 0 && codeFileEnqueueWaitMS == 0 && codeResultSubmitMS == 0 &&
		codeResultReduceMS == 0 && codeResultFinalizeMS == 0 && codeWorkerServiceMS == 0 &&
		codeWorkerBuildMS == 0 && codeWorkerSubmitLocalMS == 0 && codeWorkerSubmitSemanticMS == 0 &&
		codeWorkerSubmitWriteMS == 0 && codeWorkerSubmitMS == 0 && codeWorkerTotalMS == 0 &&
		callEdgeFlushes == 0 && callEdgeRefsLoadMS == 0 && callEdgeCacheBuildMS == 0 &&
		callEdgeCacheBuildFQNLookupMS == 0 && callEdgeCacheBuildImportLookupMS == 0 &&
		callEdgeCacheBuildGoMethodLookupMS == 0 && callEdgeCacheBuildPyFallbackLookupMS == 0 &&
		!c.windowCountersHaveActivity(phase, callEdgeCounterMetrics[:]) &&
		providerLogicalTexts == 0 &&
		len(suppressedAnchors) == 0 && len(codeChunksPruned) == 0 &&
		len(codePlanKind) == 0 && len(codePlanGranularity) == 0 &&
		len(codeEmbeddedKind) == 0 && len(codeEmbeddedGranularity) == 0 &&
		noteRawEligiblePaths == 0 && noteRawSkippedPaths == 0 &&
		ontologyTypedProjections == 0 && ontologyFallbackProjections == 0 &&
		ontologyBodyChunksPlanned == 0 &&
		ontologyChunksReused == 0 && ontologyChunksEmbedded == 0 &&
		providerCallsNeededAtProviderCap == 0 && providerCallsNeededAtDispatchFloor == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("[index-perf]")
	if window > 0 {
		fmt.Fprintf(&b, " window=%s", window.Truncate(time.Second))
	}
	if phase != "" {
		fmt.Fprintf(&b, " phase=%s", phase)
	}
	if files > 0 {
		fmt.Fprintf(&b, " files=%d", files)
	}
	if discovered > 0 {
		fmt.Fprintf(&b, " discovered=%d", discovered)
	}
	if completed > 0 {
		fmt.Fprintf(&b, " completed=%d", completed)
	}
	if chunks > 0 {
		fmt.Fprintf(&b, " chunks=%d", chunks)
	}
	if s := formatSuffixCounts("suppressed_anchors", suppressedAnchors); s != "" {
		fmt.Fprintf(&b, " %s", s)
	}
	if s := formatSuffixCounts("code_chunks_pruned", codeChunksPruned); s != "" {
		fmt.Fprintf(&b, " %s", s)
	}
	if s := formatSuffixCounts("code_plan_kind", codePlanKind); s != "" {
		fmt.Fprintf(&b, " %s", s)
	}
	if s := formatSuffixCounts("code_plan_granularity", codePlanGranularity); s != "" {
		fmt.Fprintf(&b, " %s", s)
	}
	if s := formatSuffixCounts("code_embedded_kind", codeEmbeddedKind); s != "" {
		fmt.Fprintf(&b, " %s", s)
	}
	if s := formatSuffixCounts("code_embedded_granularity", codeEmbeddedGranularity); s != "" {
		fmt.Fprintf(&b, " %s", s)
	}
	if noteRawEligiblePaths > 0 {
		fmt.Fprintf(&b, " raw_eligible_paths=%d", noteRawEligiblePaths)
	}
	if noteRawSkippedPaths > 0 {
		fmt.Fprintf(&b, " raw_skipped_paths=%d", noteRawSkippedPaths)
	}
	if ontologyTypedProjections > 0 {
		fmt.Fprintf(&b, " typed_projections_planned=%d", ontologyTypedProjections)
	}
	if ontologyFallbackProjections > 0 {
		fmt.Fprintf(&b, " fallback_projections_planned=%d", ontologyFallbackProjections)
	}
	if ontologyBodyChunksPlanned > 0 {
		fmt.Fprintf(&b, " ontology_body_chunks_planned=%d", ontologyBodyChunksPlanned)
	}
	if ontologyChunksReused > 0 {
		fmt.Fprintf(&b, " ontology_chunks_reused=%d", ontologyChunksReused)
	}
	if ontologyChunksEmbedded > 0 {
		fmt.Fprintf(&b, " ontology_chunks_embedded=%d", ontologyChunksEmbedded)
	}
	if ontologyFieldRowsPlanned > 0 {
		fmt.Fprintf(&b, " field_rows_planned=%d", ontologyFieldRowsPlanned)
	}
	if ontologyFieldRowsWritten > 0 {
		fmt.Fprintf(&b, " field_rows_written=%d", ontologyFieldRowsWritten)
	}
	if providerCalls > 0 {
		fmt.Fprintf(&b, " calls=%d", providerCalls)
	}
	if providerTexts > 0 {
		fmt.Fprintf(&b, " texts=%d", providerTexts)
	}
	if providerLogicalTexts > 0 && providerLogicalTexts != providerTexts {
		fmt.Fprintf(&b, " logical_texts=%d", providerLogicalTexts)
	}
	if voyageRequests > 0 {
		fmt.Fprintf(&b, " voyage_requests=%d", voyageRequests)
	}
	if voyageAttempts > 0 {
		fmt.Fprintf(&b, " voyage_attempts=%d", voyageAttempts)
	}
	if voyageCompleted > 0 {
		fmt.Fprintf(&b, " voyage_attempts_returned=%d", voyageCompleted)
	}
	if voyageNetworkFailures > 0 {
		fmt.Fprintf(&b, " voyage_network_failures=%d", voyageNetworkFailures)
	}
	if voyageHTTPRetries > 0 {
		fmt.Fprintf(&b, " voyage_http_retries=%d", voyageHTTPRetries)
	}
	if voyageSplits > 0 {
		fmt.Fprintf(&b, " voyage_splits=%d", voyageSplits)
	}
	if voyageBackoff > 0 {
		fmt.Fprintf(&b, " voyage_backoff_cum_ms=%d", voyageBackoff)
	}
	if voyageStatusCount > 0 {
		fmt.Fprintf(&b, " voyage_responses=%d voyage_status_p50=%d voyage_status_p95=%d voyage_status_max=%d", voyageStatusCount, voyageStatusP50, voyageStatusP95, voyageStatusMax)
	}
	if voyageInputsMax > 0 {
		fmt.Fprintf(&b, " voyage_inputs_p50=%d voyage_inputs_p95=%d voyage_inputs_max=%d", voyageInputsP50, voyageInputsP95, voyageInputsMax)
	}
	if voyageBodyBytes > 0 {
		fmt.Fprintf(&b, " voyage_body_bytes=%s", humanBytes(voyageBodyBytes))
	}
	if providerCallsNeededAtProviderCap > 0 {
		fmt.Fprintf(&b, " calls_needed_at_provider_cap=%d", providerCallsNeededAtProviderCap)
	}
	if providerCallsNeededAtDispatchFloor > 0 {
		fmt.Fprintf(&b, " calls_needed_at_dispatch_floor=%d", providerCallsNeededAtDispatchFloor)
	}
	if providerCalls > 0 && providerTexts > 0 {
		fmt.Fprintf(&b, " avg_texts_call=%.1f", float64(providerTexts)/float64(providerCalls))
	}
	windowSeconds := window.Seconds()
	if windowSeconds > 0 {
		if files > 0 {
			fmt.Fprintf(&b, " files_s=%.1f", float64(files)/windowSeconds)
		}
		if chunks > 0 {
			fmt.Fprintf(&b, " chunks_s=%.1f", float64(chunks)/windowSeconds)
		}
		if providerCalls > 0 {
			fmt.Fprintf(&b, " calls_s=%.1f", float64(providerCalls)/windowSeconds)
		}
		if providerTexts > 0 {
			fmt.Fprintf(&b, " texts_s=%.1f", float64(providerTexts)/windowSeconds)
		}
	}
	if prepMS > 0 {
		fmt.Fprintf(&b, " prep_cum_ms=%d", prepMS)
	}
	if walkMS > 0 {
		fmt.Fprintf(&b, " walk_cum_ms=%d", walkMS)
	}
	if statMS > 0 {
		fmt.Fprintf(&b, " stat_cum_ms=%d", statMS)
	}
	if readLatencyMS > 0 {
		fmt.Fprintf(&b, " read_cum_ms=%d", readLatencyMS)
	}
	if prepareBusyMS > 0 {
		fmt.Fprintf(&b, " prepare_busy_ms=%d", prepareBusyMS)
	}
	if prepareBlockedMS > 0 {
		fmt.Fprintf(&b, " prepare_blocked_ms=%d", prepareBlockedMS)
	}
	if embedStarvedMS > 0 {
		fmt.Fprintf(&b, " embed_starved_ms=%d", embedStarvedMS)
	}
	if embedPackWaitMS > 0 {
		fmt.Fprintf(&b, " embed_pack_wait_ms=%d", embedPackWaitMS)
	}
	if freeSlotsUnderMinTextsMS > 0 {
		fmt.Fprintf(&b, " free_slots_under_mintexts_ms=%d", freeSlotsUnderMinTextsMS)
	}
	if dispatchIdleMS > 0 {
		fmt.Fprintf(&b, " dispatch_idle_ms=%d", dispatchIdleMS)
	}
	c.appendWindowLatencyMetrics(&b, phase, planningLatencyMetrics[:])
	if providerMS > 0 {
		fmt.Fprintf(&b, " provider_cum_ms=%d", providerMS)
		if providerLatencyMax > 0 {
			fmt.Fprintf(&b, " provider_latency_p50_ms=%d provider_latency_p95_ms=%d provider_latency_max_ms=%d", providerLatencyP50.Milliseconds(), providerLatencyP95.Milliseconds(), providerLatencyMax.Milliseconds())
		}
	}
	if gateWaitMS > 0 {
		fmt.Fprintf(&b, " gate_wait_cum_ms=%d", gateWaitMS)
	}
	if futureWaitMS > 0 {
		fmt.Fprintf(&b, " future_wait_cum_ms=%d", futureWaitMS)
		if futureWaitMax > 0 {
			fmt.Fprintf(&b, " future_wait_p50_ms=%d future_wait_p95_ms=%d future_wait_max_ms=%d", futureWaitP50.Milliseconds(), futureWaitP95.Milliseconds(), futureWaitMax.Milliseconds())
		}
	}
	if queueWaitMS > 0 {
		fmt.Fprintf(&b, " queue_wait_cum_ms=%d", queueWaitMS)
	}
	if codeQueueWaitMS > 0 {
		fmt.Fprintf(&b, " code_queue_wait_cum_ms=%d", codeQueueWaitMS)
	}
	if otherQueueWaitMS > 0 {
		fmt.Fprintf(&b, " other_queue_wait_cum_ms=%d", otherQueueWaitMS)
	}
	if dbWaitMS > 0 {
		fmt.Fprintf(&b, " db_wait_cum_ms=%d", dbWaitMS)
	}
	if dbHoldMS > 0 {
		fmt.Fprintf(&b, " db_hold_cum_ms=%d", dbHoldMS)
	}
	if writerIdleMS > 0 {
		fmt.Fprintf(&b, " writer_idle_ms=%d", writerIdleMS)
	}
	if writerBusyMS > 0 {
		fmt.Fprintf(&b, " writer_busy_ms=%d", writerBusyMS)
	}
	if writerDeferredMS > 0 {
		fmt.Fprintf(&b, " writer_deferred_ms=%d", writerDeferredMS)
	}
	c.appendWindowLatencyMetrics(&b, phase, codeIndexLatencyMetrics[:])
	if noteFinalizeMS > 0 {
		fmt.Fprintf(&b, " finalize_cum_ms=%d", noteFinalizeMS)
		if noteFinalizeMax > 0 {
			fmt.Fprintf(&b, " finalize_p50_ms=%d", noteFinalizeP50.Milliseconds())
			fmt.Fprintf(&b, " finalize_p95_ms=%d", noteFinalizeP95.Milliseconds())
			fmt.Fprintf(&b, " finalize_max_ms=%d", noteFinalizeMax.Milliseconds())
		}
	}
	c.appendWindowLatencyMetrics(&b, phase, noteFinalizeLatencyMetrics[:])
	c.appendWindowWritebackMetrics(&b, phase)
	if queueDepthPeak > 0 {
		fmt.Fprintf(&b, " queue_depth_peak=%d", queueDepthPeak)
	}
	if queueDepthAvg > 0 {
		fmt.Fprintf(&b, " queue_depth_avg=%.1f", queueDepthAvg)
	}
	if codeQueueDepthPeak > 0 {
		fmt.Fprintf(&b, " code_queue_depth_peak=%d", codeQueueDepthPeak)
	}
	if codeQueueDepthAvg > 0 {
		fmt.Fprintf(&b, " code_queue_depth_avg=%.1f", codeQueueDepthAvg)
	}
	if otherQueueDepthPeak > 0 {
		fmt.Fprintf(&b, " other_queue_depth_peak=%d", otherQueueDepthPeak)
	}
	if otherQueueDepthAvg > 0 {
		fmt.Fprintf(&b, " other_queue_depth_avg=%.1f", otherQueueDepthAvg)
	}
	if fileQueueDepthPeak > 0 {
		fmt.Fprintf(&b, " file_queue_depth_peak=%d", fileQueueDepthPeak)
	}
	if fileQueueDepthAvg > 0 {
		fmt.Fprintf(&b, " file_queue_depth_avg=%.1f", fileQueueDepthAvg)
	}
	if activeWorkersPeak := c.windowGaugePeakPhase(phase, "codeindex.active_workers"); activeWorkersPeak > 0 {
		fmt.Fprintf(&b, " active_workers_peak=%d", activeWorkersPeak)
	}
	if activeWorkersAvg := c.windowGaugeAveragePhase(phase, "codeindex.active_workers"); activeWorkersAvg > 0 {
		fmt.Fprintf(&b, " active_workers_avg=%.1f", activeWorkersAvg)
	}
	if semanticQueueDepthPeak := c.windowGaugePeakPhase(phase, "codeindex.semantic_queue_depth"); semanticQueueDepthPeak > 0 {
		fmt.Fprintf(&b, " semantic_queue_depth_peak=%d", semanticQueueDepthPeak)
	}
	if semanticQueueDepthAvg := c.windowGaugeAveragePhase(phase, "codeindex.semantic_queue_depth"); semanticQueueDepthAvg > 0 {
		fmt.Fprintf(&b, " semantic_queue_depth_avg=%.1f", semanticQueueDepthAvg)
	}
	if resultQueueDepthPeak := c.windowGaugePeakPhase(phase, "codeindex.result_queue_depth"); resultQueueDepthPeak > 0 {
		fmt.Fprintf(&b, " result_queue_depth_peak=%d", resultQueueDepthPeak)
	}
	if resultQueueDepthAvg := c.windowGaugeAveragePhase(phase, "codeindex.result_queue_depth"); resultQueueDepthAvg > 0 {
		fmt.Fprintf(&b, " result_queue_depth_avg=%.1f", resultQueueDepthAvg)
	}
	c.appendWindowWritebackQueueMetrics(&b, phase)
	if readyDepthPeak > 0 {
		fmt.Fprintf(&b, " ready_depth_peak=%d", readyDepthPeak)
	}
	if readyDepthAvg > 0 {
		fmt.Fprintf(&b, " ready_depth_avg=%.1f", readyDepthAvg)
	}
	if providerInflightPeak > 0 {
		fmt.Fprintf(&b, " provider_inflight_peak=%d", providerInflightPeak)
	}
	if providerInflightAvg > 0 {
		fmt.Fprintf(&b, " provider_inflight_avg=%.1f", providerInflightAvg)
	}
	if intentPendingRows > 0 {
		fmt.Fprintf(&b, " intent_pending_rows=%d", intentPendingRows)
	}
	if intentPendingPeak > 0 {
		fmt.Fprintf(&b, " intent_pending_rows_peak=%d", intentPendingPeak)
	}
	if providerInflightCurrent > 0 {
		fmt.Fprintf(&b, " provider_inflight=%d", providerInflightCurrent)
	}
	if batchTextsMax > 0 {
		fmt.Fprintf(&b, " batch_texts_p50=%d", batchTextsP50)
		fmt.Fprintf(&b, " batch_texts_p95=%d", batchTextsP95)
		fmt.Fprintf(&b, " batch_texts_max=%d", batchTextsMax)
	}
	if batchBytesMax > 0 {
		fmt.Fprintf(&b, " batch_bytes_p50=%s", humanBytes(batchBytesP50))
		fmt.Fprintf(&b, " batch_bytes_p95=%s", humanBytes(batchBytesP95))
		fmt.Fprintf(&b, " batch_bytes_max=%s", humanBytes(batchBytesMax))
	}
	if batchFillMax > 0 {
		fmt.Fprintf(&b, " batch_fill_ratio_p50=%d%% batch_fill_ratio_p95=%d%% batch_fill_ratio_max=%d%%", batchFillP50, batchFillP95, batchFillMax)
	}
	if bytesFillMax > 0 {
		fmt.Fprintf(&b, " bytes_fill_ratio_p50=%d%% bytes_fill_ratio_p95=%d%% bytes_fill_ratio_max=%d%%", bytesFillP50, bytesFillP95, bytesFillMax)
	}
	if hashPrefetchCalls > 0 {
		fmt.Fprintf(&b, " hash_prefetch_calls=%d", hashPrefetchCalls)
	}
	if hashPrefetchHashes > 0 {
		fmt.Fprintf(&b, " hash_prefetch_hashes=%d", hashPrefetchHashes)
	}
	if hashPrefetchHits > 0 {
		fmt.Fprintf(&b, " hash_prefetch_hits=%d", hashPrefetchHits)
	}
	if hashPrefetchBatchMax > 0 {
		fmt.Fprintf(&b, " hash_prefetch_batch_p50=%d", hashPrefetchBatchP50)
		fmt.Fprintf(&b, " hash_prefetch_batch_p95=%d", hashPrefetchBatchP95)
		fmt.Fprintf(&b, " hash_prefetch_batch_max=%d", hashPrefetchBatchMax)
	}
	if memReuseHits > 0 {
		fmt.Fprintf(&b, " reuse_mem_hits=%d", memReuseHits)
	}
	if memReuseMisses > 0 {
		fmt.Fprintf(&b, " reuse_mem_misses=%d", memReuseMisses)
	}
	if callEdgeFlushes > 0 {
		fmt.Fprintf(&b, " calledge_flushes=%d", callEdgeFlushes)
		if callEdgePathsMax > 0 {
			fmt.Fprintf(&b, " calledge_paths_p50=%d", callEdgePathsP50)
			fmt.Fprintf(&b, " calledge_paths_p95=%d", callEdgePathsP95)
			fmt.Fprintf(&b, " calledge_paths_max=%d", callEdgePathsMax)
		}
		if callEdgeWorkMax > 0 {
			fmt.Fprintf(&b, " calledge_work_p50=%d", callEdgeWorkP50)
			fmt.Fprintf(&b, " calledge_work_p95=%d", callEdgeWorkP95)
			fmt.Fprintf(&b, " calledge_work_max=%d", callEdgeWorkMax)
		}
	}
	c.appendWindowLatencyMetrics(&b, phase, callEdgeLatencyMetrics[:])
	c.appendWindowCounterMetrics(&b, phase, callEdgeCounterMetrics[:])
	c.appendWindowCounterMetrics(&b, phase, callEdgeLookupCounterMetrics[:])
	if embedQueueAgeAvg > 0 {
		fmt.Fprintf(&b, " embed_queue_age_avg_ms=%d", embedQueueAgeAvg.Milliseconds())
	}
	if embedQueueAgeMax > 0 {
		fmt.Fprintf(&b, " embed_queue_age_max_ms=%d", embedQueueAgeMax.Milliseconds())
	}
	if batchOldestAgeMax > 0 {
		fmt.Fprintf(&b, " embed_batch_oldest_max_ms=%d", batchOldestAgeMax.Milliseconds())
	}
	if prepareIn > 0 || prepareOut > 0 {
		fmt.Fprintf(&b, " prepare_tasks_in=%d", prepareIn)
		fmt.Fprintf(&b, " prepare_tasks_out=%d", prepareOut)
	}
	if embedIn > 0 || embedOut > 0 {
		fmt.Fprintf(&b, " embed_texts_in=%d", embedIn)
		fmt.Fprintf(&b, " embed_texts_out=%d", embedOut)
	}
	if writerIn > 0 || writerOut > 0 {
		fmt.Fprintf(&b, " writer_cmds_in=%d", writerIn)
		fmt.Fprintf(&b, " writer_rows_out=%d", writerOut)
	}
	return b.String()
}
