package indexingperf

import (
	"fmt"
)

func (c *Collector) phaseSummaryParts(phase string) []string {
	parts := make([]string, 0, 20)
	files := c.phaseCounterTotal(phase, "fs.read")
	discovered := c.phaseCounterTotal(phase, "fs.discovered")
	completed := c.phaseCounterTotal(phase, "fs.completed")
	chunks := c.phaseCounterTotal(phase, "semantic.chunks")
	suppressedAnchors := c.phaseCounterTotalPrefix(phase, "semantic.suppressed_anchors.")
	codeChunksPruned := c.phaseCounterTotalPrefix(phase, "semantic.code_chunks_pruned.")
	codePlanKind := c.phaseCounterTotalPrefix(phase, "semantic.code_plan.kind.")
	codePlanGranularity := c.phaseCounterTotalPrefix(phase, "semantic.code_plan.granularity.")
	codeEmbeddedKind := c.phaseCounterTotalPrefix(phase, "semantic.code_chunks.kind.")
	codeEmbeddedGranularity := c.phaseCounterTotalPrefix(phase, "semantic.code_chunks.granularity.")
	prepareIn := c.phaseCounterTotal(phase, "node.prepare.in")
	prepareOut := c.phaseCounterTotal(phase, "node.prepare.out")
	embedIn := c.phaseCounterTotal(phase, "node.embed.in")
	embedOut := c.phaseCounterTotal(phase, "node.embed.out")
	writerIn := c.phaseCounterTotal(phase, "node.write.in")
	writerOut := c.phaseCounterTotal(phase, "node.write.out")
	providerCalls := c.phaseCounterTotal(phase, "provider.calls")
	providerTexts := c.phaseCounterTotal(phase, "provider.texts")
	providerLogicalTexts := c.phaseCounterTotal(phase, "provider.logical_texts")
	providerTextBytes := c.phaseCounterTotal(phase, "provider.text_bytes")
	voyageInputsP50, voyageInputsP95, voyageInputsMax := c.phaseSamplePercentiles(phase, "provider.request.voyage.inputs")
	voyageStatusP50, voyageStatusP95, voyageStatusMax := c.phaseSamplePercentiles(phase, "provider.request.voyage.status")
	prepLatency := c.phaseLatencyTotal(phase, "codeembed.prepare") + c.phaseLatencyTotal(phase, "noteembed.prepare")
	embedFutureWaitP50, embedFutureWaitP95, embedFutureWaitMax := c.phaseLatencyPercentiles(phase, "embed.future_wait")
	providerLatencyP50, providerLatencyP95, providerLatencyMax := c.phaseLatencyPercentiles(phase, "provider.latency")
	noteFinalizeP50, noteFinalizeP95, noteFinalizeMax := c.phaseLatencyPercentiles(phase, "noteembed.finalize")
	batchTextsP50, batchTextsP95, batchTextsMax := c.phaseSamplePercentiles(phase, "provider.batch_texts")
	batchBytesP50, batchBytesP95, batchBytesMax := c.phaseSamplePercentiles(phase, "provider.batch_bytes")
	inputTextsP50, inputTextsP95, inputTextsMax := c.phaseSamplePercentiles(phase, "provider.input_texts")
	inputBytesP50, inputBytesP95, inputBytesMax := c.phaseSamplePercentiles(phase, "provider.input_bytes")
	providerCapacityP50, providerCapacityP95, providerCapacityMax := c.phaseSamplePercentiles(phase, "provider.capacity")
	_, _, providerCapTextsMax := c.phaseSamplePercentiles(phase, "provider.cap_texts")
	_, _, providerDispatchFloorTextsMax := c.phaseSamplePercentiles(phase, "provider.dispatch_floor_texts")
	batchFillP50, batchFillP95, batchFillMax := c.phaseSamplePercentiles(phase, "provider.batch_fill_ratio")
	bytesFillP50, bytesFillP95, bytesFillMax := c.phaseSamplePercentiles(phase, "provider.bytes_fill_ratio")
	hashPrefetchBatchP50, hashPrefetchBatchP95, hashPrefetchBatchMax := c.phaseSamplePercentiles(phase, "codeembed.hash_prefetch.batch")
	callEdgePathsP50, callEdgePathsP95, callEdgePathsMax := c.phaseSamplePercentiles(phase, "calledge.flush.paths")
	callEdgeWorkP50, callEdgeWorkP95, callEdgeWorkMax := c.phaseSamplePercentiles(phase, "calledge.flush.work")
	callsNeededTexts := providerLogicalTexts
	if callsNeededTexts == 0 {
		callsNeededTexts = providerTexts
	}
	providerCallsNeededAtProviderCap := ceilDivInt64(callsNeededTexts, providerCapTextsMax)
	providerCallsNeededAtDispatchFloor := ceilDivInt64(callsNeededTexts, providerDispatchFloorTextsMax)

	if files > 0 {
		parts = append(parts, fmt.Sprintf("files=%d", files))
	}
	if discovered > 0 || completed > 0 {
		parts = append(parts, fmt.Sprintf("discovery=%d/%d", completed, discovered))
	}
	if readBytes := c.phaseCounterTotal(phase, "fs.read_bytes"); readBytes > 0 {
		parts = append(parts, fmt.Sprintf("read=%s", humanBytes(readBytes)))
	}
	if chunks > 0 {
		parts = append(parts, fmt.Sprintf("chunks=%d", chunks))
	}
	if s := formatSuffixCounts("suppressed_anchors", suppressedAnchors); s != "" {
		parts = append(parts, s)
	}
	if s := formatSuffixCounts("code_chunks_pruned", codeChunksPruned); s != "" {
		parts = append(parts, s)
	}
	if s := formatSuffixCounts("code_plan_kind", codePlanKind); s != "" {
		parts = append(parts, s)
	}
	if s := formatSuffixCounts("code_plan_granularity", codePlanGranularity); s != "" {
		parts = append(parts, s)
	}
	if s := formatSuffixCounts("code_embedded_kind", codeEmbeddedKind); s != "" {
		parts = append(parts, s)
	}
	if s := formatSuffixCounts("code_embedded_granularity", codeEmbeddedGranularity); s != "" {
		parts = append(parts, s)
	}
	if noteRawEligiblePaths := c.phaseCounterTotal(phase, "noteplan.raw_eligible_paths"); noteRawEligiblePaths > 0 {
		parts = append(parts, fmt.Sprintf("raw_eligible_paths=%d", noteRawEligiblePaths))
	}
	if noteRawSkippedPaths := c.phaseCounterTotal(phase, "noteplan.raw_skipped_paths"); noteRawSkippedPaths > 0 {
		parts = append(parts, fmt.Sprintf("raw_skipped_paths=%d", noteRawSkippedPaths))
	}
	if ontologyTypedProjections := c.phaseCounterTotal(phase, "ontology_nodes.typed_projections_planned"); ontologyTypedProjections > 0 {
		parts = append(parts, fmt.Sprintf("typed_projections_planned=%d", ontologyTypedProjections))
	}
	if ontologyFallbackProjections := c.phaseCounterTotal(phase, "ontology_nodes.fallback_projections_planned"); ontologyFallbackProjections > 0 {
		parts = append(parts, fmt.Sprintf("fallback_projections_planned=%d", ontologyFallbackProjections))
	}
	if ontologyBodyChunksPlanned := c.phaseCounterTotal(phase, "ontology_nodes.body_chunks_planned"); ontologyBodyChunksPlanned > 0 {
		parts = append(parts, fmt.Sprintf("ontology_body_chunks_planned=%d", ontologyBodyChunksPlanned))
	}
	if ontologyChunksReused := c.phaseCounterTotal(phase, "ontology_nodes.chunks_reused"); ontologyChunksReused > 0 {
		parts = append(parts, fmt.Sprintf("ontology_chunks_reused=%d", ontologyChunksReused))
	}
	if ontologyChunksEmbedded := c.phaseCounterTotal(phase, "ontology_nodes.chunks_embedded"); ontologyChunksEmbedded > 0 {
		parts = append(parts, fmt.Sprintf("ontology_chunks_embedded=%d", ontologyChunksEmbedded))
	}
	if ontologyConsidered := c.phaseCounterTotal(phase, "ontology.notes_considered"); ontologyConsidered > 0 {
		parts = append(parts, fmt.Sprintf("notes=%d", ontologyConsidered))
	}
	if ontologySkipped := c.phaseCounterTotal(phase, "ontology.notes_skipped"); ontologySkipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped=%d", ontologySkipped))
	}
	if ontologyAssessed := c.phaseCounterTotal(phase, "ontology.notes_assessed"); ontologyAssessed > 0 {
		parts = append(parts, fmt.Sprintf("assessed=%d", ontologyAssessed))
	}
	if ontologyRelations := c.phaseCounterTotal(phase, "ontology.relations"); ontologyRelations > 0 {
		parts = append(parts, fmt.Sprintf("relations=%d", ontologyRelations))
	}
	if ontologyDeltaRows := c.phaseCounterTotal(phase, "ontology.delta_rows"); ontologyDeltaRows > 0 {
		parts = append(parts, fmt.Sprintf("delta_rows=%d", ontologyDeltaRows))
	}
	if ontologyFieldRowsPlanned := c.phaseCounterTotal(phase, "ontology.field_rows_planned"); ontologyFieldRowsPlanned > 0 {
		parts = append(parts, fmt.Sprintf("field_rows_planned=%d", ontologyFieldRowsPlanned))
	}
	if ontologyFieldRowsWritten := c.phaseCounterTotal(phase, "ontology.field_rows_written"); ontologyFieldRowsWritten > 0 {
		parts = append(parts, fmt.Sprintf("field_rows_written=%d", ontologyFieldRowsWritten))
	}
	if providerCalls > 0 {
		parts = append(parts, fmt.Sprintf("calls=%d", providerCalls))
	}
	if providerTexts > 0 {
		parts = append(parts, fmt.Sprintf("texts=%d", providerTexts))
	}
	if providerLogicalTexts > 0 && providerLogicalTexts != providerTexts {
		parts = append(parts, fmt.Sprintf("logical_texts=%d", providerLogicalTexts))
	}
	if providerCallsNeededAtProviderCap > 0 {
		parts = append(parts, fmt.Sprintf("calls_needed_at_provider_cap=%d", providerCallsNeededAtProviderCap))
	}
	if providerCallsNeededAtDispatchFloor > 0 {
		parts = append(parts, fmt.Sprintf("calls_needed_at_dispatch_floor=%d", providerCallsNeededAtDispatchFloor))
	}
	if providerCalls > 0 && providerTexts > 0 {
		parts = append(parts, fmt.Sprintf("avg_texts/call=%.1f", float64(providerTexts)/float64(providerCalls)))
	}
	if providerCalls > 0 && providerTextBytes > 0 {
		parts = append(parts, fmt.Sprintf("avg_bytes/call=%s", humanBytes(providerTextBytes/providerCalls)))
	}
	if pending := c.currentGaugePhase(phase, "intent.writeback.pending_rows"); pending > 0 {
		parts = append(parts, fmt.Sprintf("intent_pending_rows=%d", pending))
	}
	if peak := c.phaseGaugePeak(phase, "intent.writeback.pending_rows"); peak > 0 {
		parts = append(parts, fmt.Sprintf("intent_pending_rows_peak=%d", peak))
	}
	if providerMixedBatches := c.phaseCounterTotal(phase, "provider.mixed_batches"); providerMixedBatches > 0 {
		parts = append(parts, fmt.Sprintf("mixed_batches=%d", providerMixedBatches))
	}
	if voyageRequests := c.phaseCounterTotal(phase, "provider.request.voyage"); voyageRequests > 0 {
		parts = append(parts, fmt.Sprintf("voyage_requests=%d", voyageRequests))
	}
	if voyageAttempts := c.phaseCounterTotal(phase, "provider.request.voyage.attempt"); voyageAttempts > 0 {
		parts = append(parts, fmt.Sprintf("voyage_attempts=%d", voyageAttempts))
	}
	if voyageCompleted := c.phaseCounterTotal(phase, "provider.request.voyage.completed"); voyageCompleted > 0 {
		parts = append(parts, fmt.Sprintf("voyage_attempts_returned=%d", voyageCompleted))
	}
	if voyageNetworkFailures := c.phaseCounterTotal(phase, "provider.request.voyage.network_failures"); voyageNetworkFailures > 0 {
		parts = append(parts, fmt.Sprintf("voyage_network_failures=%d", voyageNetworkFailures))
	}
	if voyageHTTPRetries := c.phaseCounterTotal(phase, "provider.request.voyage.http_retry"); voyageHTTPRetries > 0 {
		parts = append(parts, fmt.Sprintf("voyage_http_retries=%d", voyageHTTPRetries))
	}
	if voyageSplits := c.phaseCounterTotal(phase, "provider.request.voyage.split"); voyageSplits > 0 {
		parts = append(parts, fmt.Sprintf("voyage_splits=%d", voyageSplits))
	}
	if voyageBackoff := c.phaseLatencyTotal(phase, "provider.request.voyage.backoff"); voyageBackoff > 0 {
		parts = append(parts, fmt.Sprintf("voyage_backoff_cum=%s", shortDur(voyageBackoff)))
	}
	if voyageStatusCount := c.phaseSampleCount(phase, "provider.request.voyage.status"); voyageStatusCount > 0 {
		parts = append(parts,
			fmt.Sprintf("voyage_responses=%d", voyageStatusCount),
			fmt.Sprintf("voyage_status_p50=%d", voyageStatusP50),
			fmt.Sprintf("voyage_status_p95=%d", voyageStatusP95),
			fmt.Sprintf("voyage_status_max=%d", voyageStatusMax),
		)
	}
	if voyageInputsMax > 0 {
		parts = append(parts,
			fmt.Sprintf("voyage_inputs_p50=%d", voyageInputsP50),
			fmt.Sprintf("voyage_inputs_p95=%d", voyageInputsP95),
			fmt.Sprintf("voyage_inputs_max=%d", voyageInputsMax),
		)
	}
	if voyageBodyBytes := c.phaseCounterTotal(phase, "provider.request.voyage.body_bytes"); voyageBodyBytes > 0 {
		parts = append(parts, fmt.Sprintf("voyage_body_bytes=%s", humanBytes(voyageBodyBytes)))
	}
	if embedDedupeSaved := c.phaseCounterTotal(phase, "embed.dedupe.saved"); embedDedupeSaved > 0 {
		parts = append(parts, fmt.Sprintf("dedupe_saved=%d", embedDedupeSaved))
		if embedDedupeSavedBytes := c.phaseCounterTotal(phase, "embed.dedupe.saved_bytes"); embedDedupeSavedBytes > 0 {
			parts = append(parts, fmt.Sprintf("dedupe_saved_bytes=%s", humanBytes(embedDedupeSavedBytes)))
		}
	}
	spanDuration := c.phaseDuration(phase)
	if spanDuration > 0 {
		seconds := spanDuration.Seconds()
		if files > 0 {
			parts = append(parts, fmt.Sprintf("files/s=%.1f", float64(files)/seconds))
		}
		if chunks > 0 {
			parts = append(parts, fmt.Sprintf("chunks/s=%.1f", float64(chunks)/seconds))
		}
		if providerCalls > 0 {
			parts = append(parts, fmt.Sprintf("calls/s=%.1f", float64(providerCalls)/seconds))
		}
		if providerTexts > 0 {
			parts = append(parts, fmt.Sprintf("texts/s=%.1f", float64(providerTexts)/seconds))
		}
	}
	if prepLatency > 0 {
		parts = append(parts, fmt.Sprintf("prep_cum=%s", shortDur(prepLatency)))
	}
	if walkLatency := c.phaseLatencyTotal(phase, "fs.walk"); walkLatency > 0 {
		parts = append(parts, fmt.Sprintf("walk_cum=%s", shortDur(walkLatency)))
	}
	if statLatency := c.phaseLatencyTotal(phase, "fs.stat"); statLatency > 0 {
		parts = append(parts, fmt.Sprintf("stat_cum=%s", shortDur(statLatency)))
	}
	if readLatency := c.phaseLatencyTotal(phase, "fs.read_latency"); readLatency > 0 {
		parts = append(parts, fmt.Sprintf("read_cum=%s", shortDur(readLatency)))
	}
	if prepareBusy := c.phaseLatencyTotal(phase, "node.prepare.busy"); prepareBusy > 0 {
		parts = append(parts, fmt.Sprintf("prepare_busy=%s", shortDur(prepareBusy)))
	}
	if prepareBlocked := c.phaseLatencyTotal(phase, "node.prepare.blocked"); prepareBlocked > 0 {
		parts = append(parts, fmt.Sprintf("prepare_blocked=%s", shortDur(prepareBlocked)))
	}
	if embedStarved := c.phaseLatencyTotal(phase, "node.embed.starved"); embedStarved > 0 {
		parts = append(parts, fmt.Sprintf("embed_starved=%s", shortDur(embedStarved)))
	}
	if embedPackWait := c.phaseLatencyTotal(phase, "node.embed.pack_wait"); embedPackWait > 0 {
		parts = append(parts, fmt.Sprintf("embed_pack_wait=%s", shortDur(embedPackWait)))
	}
	if embedSubmitWait := c.phaseLatencyTotal(phase, "embed.submit_wait"); embedSubmitWait > 0 {
		parts = append(parts, fmt.Sprintf("embed_submit_wait=%s", shortDur(embedSubmitWait)))
	}
	if embedFutureWait := c.phaseLatencyTotal(phase, "embed.future_wait"); embedFutureWait > 0 {
		parts = append(parts, fmt.Sprintf("future_wait=%s", shortDur(embedFutureWait)))
		if embedFutureWaitMax > 0 {
			parts = append(parts,
				fmt.Sprintf("future_wait_p50=%s", shortDur(embedFutureWaitP50)),
				fmt.Sprintf("future_wait_p95=%s", shortDur(embedFutureWaitP95)),
				fmt.Sprintf("future_wait_max=%s", shortDur(embedFutureWaitMax)),
			)
		}
	}
	if nodePackWait := c.phaseLatencyTotal(phase, "embed.node_pack_wait"); nodePackWait > 0 {
		parts = append(parts, fmt.Sprintf("node_pack_wait=%s", shortDur(nodePackWait)))
	}
	if nodeSlotsWait := c.phaseLatencyTotal(phase, "embed.node_slots_wait"); nodeSlotsWait > 0 {
		parts = append(parts, fmt.Sprintf("node_slots_wait=%s", shortDur(nodeSlotsWait)))
	}
	if freeSlotsUnderMinTexts := c.phaseLatencyTotal(phase, "node.embed.free_slots_under_mintexts"); freeSlotsUnderMinTexts > 0 {
		parts = append(parts, fmt.Sprintf("free_slots_under_mintexts=%s", shortDur(freeSlotsUnderMinTexts)))
	}
	if dispatchIdle := c.phaseLatencyTotal(phase, "provider.dispatch_idle"); dispatchIdle > 0 {
		parts = append(parts, fmt.Sprintf("dispatch_idle=%s", shortDur(dispatchIdle)))
	}
	parts = c.appendSummaryLatencyMetrics(parts, phase, planningLatencyMetrics[:])
	if providerLatency := c.phaseLatencyTotal(phase, "provider.latency"); providerLatency > 0 {
		parts = append(parts, fmt.Sprintf("provider_cum=%s", shortDur(providerLatency)))
		if providerLatencyMax > 0 {
			parts = append(parts,
				fmt.Sprintf("provider_latency_p50=%s", shortDur(providerLatencyP50)),
				fmt.Sprintf("provider_latency_p95=%s", shortDur(providerLatencyP95)),
				fmt.Sprintf("provider_latency_max=%s", shortDur(providerLatencyMax)),
			)
		}
	}
	if gateWait := c.phaseLatencyTotal(phase, "embed.gate_wait"); gateWait > 0 {
		parts = append(parts, fmt.Sprintf("gate_wait_cum=%s", shortDur(gateWait)))
	}
	if queueWait := c.phaseLatencyTotal(phase, "queue.wait"); queueWait > 0 {
		parts = append(parts, fmt.Sprintf("queue_wait_cum=%s", shortDur(queueWait)))
	}
	if codeQueueWait := c.phaseLatencyTotal(phase, "queue.code_wait"); codeQueueWait > 0 {
		parts = append(parts, fmt.Sprintf("code_queue_wait_cum=%s", shortDur(codeQueueWait)))
	}
	if otherQueueWait := c.phaseLatencyTotal(phase, "queue.other_wait"); otherQueueWait > 0 {
		parts = append(parts, fmt.Sprintf("other_queue_wait_cum=%s", shortDur(otherQueueWait)))
	}
	if dbWait := c.phaseLatencyTotal(phase, "db.wait"); dbWait > 0 {
		parts = append(parts, fmt.Sprintf("db_wait_cum=%s", shortDur(dbWait)))
	}
	if dbHold := c.phaseLatencyTotal(phase, "db.hold"); dbHold > 0 {
		parts = append(parts, fmt.Sprintf("db_hold_cum=%s", shortDur(dbHold)))
	}
	if writerIdle := c.phaseLatencyTotal(phase, "node.write.starved"); writerIdle > 0 {
		parts = append(parts, fmt.Sprintf("writer_idle=%s", shortDur(writerIdle)))
	}
	if writerBusy := c.phaseLatencyTotal(phase, "node.write.busy"); writerBusy > 0 {
		parts = append(parts, fmt.Sprintf("writer_busy=%s", shortDur(writerBusy)))
	}
	if writerDeferred := c.phaseLatencyTotal(phase, "node.write.deferred"); writerDeferred > 0 {
		parts = append(parts, fmt.Sprintf("writer_deferred=%s", shortDur(writerDeferred)))
	}
	if providerInflightPeak := c.phaseGaugePeak(phase, "provider.inflight"); providerInflightPeak > 0 {
		parts = append(parts, fmt.Sprintf("provider_inflight_peak=%d", providerInflightPeak))
	}
	if readyDepthPeak := c.phaseGaugePeak(phase, "embed.ready_depth"); readyDepthPeak > 0 {
		parts = append(parts, fmt.Sprintf("ready_depth_peak=%d", readyDepthPeak))
	}
	if readyDepthAvg := c.phaseGaugeAverage(phase, "embed.ready_depth"); readyDepthAvg > 0 {
		parts = append(parts, fmt.Sprintf("ready_depth_avg=%.1f", readyDepthAvg))
	}
	if providerInflightAvg := c.phaseGaugeAverage(phase, "provider.inflight"); providerInflightAvg > 0 {
		parts = append(parts, fmt.Sprintf("provider_inflight_avg=%.1f", providerInflightAvg))
		if providerCapacityMax > 0 {
			parts = append(parts, fmt.Sprintf("provider_inflight_utilization=%.1f%%", providerInflightAvg*100/float64(providerCapacityMax)))
		}
	}
	if providerCapacityMax > 0 {
		parts = append(parts,
			fmt.Sprintf("provider_capacity_p50=%d", providerCapacityP50),
			fmt.Sprintf("provider_capacity_p95=%d", providerCapacityP95),
			fmt.Sprintf("provider_capacity_max=%d", providerCapacityMax),
		)
	}
	if batchTextsMax > 0 {
		parts = append(parts,
			fmt.Sprintf("batch_texts_p50=%d", batchTextsP50),
			fmt.Sprintf("batch_texts_p95=%d", batchTextsP95),
			fmt.Sprintf("batch_texts_max=%d", batchTextsMax),
		)
	}
	if batchBytesMax > 0 {
		parts = append(parts,
			fmt.Sprintf("batch_bytes_p50=%s", humanBytes(batchBytesP50)),
			fmt.Sprintf("batch_bytes_p95=%s", humanBytes(batchBytesP95)),
			fmt.Sprintf("batch_bytes_max=%s", humanBytes(batchBytesMax)),
		)
	}
	if inputTextsMax > 0 && inputTextsMax != batchTextsMax {
		parts = append(parts,
			fmt.Sprintf("input_texts_p50=%d", inputTextsP50),
			fmt.Sprintf("input_texts_p95=%d", inputTextsP95),
			fmt.Sprintf("input_texts_max=%d", inputTextsMax),
		)
	}
	if inputBytesMax > 0 && inputBytesMax != batchBytesMax {
		parts = append(parts,
			fmt.Sprintf("input_bytes_p50=%s", humanBytes(inputBytesP50)),
			fmt.Sprintf("input_bytes_p95=%s", humanBytes(inputBytesP95)),
			fmt.Sprintf("input_bytes_max=%s", humanBytes(inputBytesMax)),
		)
	}
	if batchFillMax > 0 {
		parts = append(parts,
			fmt.Sprintf("batch_fill_ratio_p50=%d%%", batchFillP50),
			fmt.Sprintf("batch_fill_ratio_p95=%d%%", batchFillP95),
			fmt.Sprintf("batch_fill_ratio_max=%d%%", batchFillMax),
		)
	}
	if bytesFillMax > 0 {
		parts = append(parts,
			fmt.Sprintf("bytes_fill_ratio_p50=%d%%", bytesFillP50),
			fmt.Sprintf("bytes_fill_ratio_p95=%d%%", bytesFillP95),
			fmt.Sprintf("bytes_fill_ratio_max=%d%%", bytesFillMax),
		)
	}
	if hashPrefetchCalls := c.phaseCounterTotal(phase, "codeembed.hash_prefetch.calls"); hashPrefetchCalls > 0 {
		parts = append(parts, fmt.Sprintf("hash_prefetch_calls=%d", hashPrefetchCalls))
	}
	if hashPrefetchHashes := c.phaseCounterTotal(phase, "codeembed.hash_prefetch.hashes"); hashPrefetchHashes > 0 {
		parts = append(parts, fmt.Sprintf("hash_prefetch_hashes=%d", hashPrefetchHashes))
	}
	if hashPrefetchHits := c.phaseCounterTotal(phase, "codeembed.hash_prefetch.hits"); hashPrefetchHits > 0 {
		parts = append(parts, fmt.Sprintf("hash_prefetch_hits=%d", hashPrefetchHits))
	}
	if hashPrefetchBatchMax > 0 {
		parts = append(parts,
			fmt.Sprintf("hash_prefetch_batch_p50=%d", hashPrefetchBatchP50),
			fmt.Sprintf("hash_prefetch_batch_p95=%d", hashPrefetchBatchP95),
			fmt.Sprintf("hash_prefetch_batch_max=%d", hashPrefetchBatchMax),
		)
	}
	if reuseMemHits := c.phaseCounterTotal(phase, "codeembed.reuse_mem.hit"); reuseMemHits > 0 {
		parts = append(parts, fmt.Sprintf("reuse_mem_hits=%d", reuseMemHits))
	}
	if reuseMemMisses := c.phaseCounterTotal(phase, "codeembed.reuse_mem.miss"); reuseMemMisses > 0 {
		parts = append(parts, fmt.Sprintf("reuse_mem_misses=%d", reuseMemMisses))
	}
	for _, reason := range []string{
		"no_chunks",
		"unchanged",
		"unchanged_hash_fallback",
		"first_build_missing_state",
		"legacy_missing_item_hash",
		"missing_item_hash",
		"item_hash_mismatch",
		"chunk_hash_mismatch",
	} {
		if count := c.phaseCounterTotal(phase, "codeembed.need."+reason); count > 0 {
			parts = append(parts, fmt.Sprintf("need_%s=%d", reason, count))
		}
	}
	for _, reason := range []string{
		"missing_fingerprint",
		"fingerprint_mismatch",
		"schema_or_fingerprint_mismatch",
		"missing_item_hash",
		"first_build_candidate",
	} {
		if count := c.phaseCounterTotal(phase, "codeembed.state."+reason); count > 0 {
			parts = append(parts, fmt.Sprintf("state_%s=%d", reason, count))
		}
	}
	if c.phaseCounterTotal(phase, "codeembed.state.first_build_candidate") > 0 {
		parts = append(parts, "state_interpretation=first_build_or_empty_state")
	} else if c.phaseCounterTotal(phase, "codeembed.state.schema_or_fingerprint_mismatch") > 0 {
		parts = append(parts, "state_interpretation=fingerprint_version_or_schema_change")
	} else if c.phaseCounterTotal(phase, "codeembed.need.legacy_missing_item_hash") > 0 {
		parts = append(parts, "state_interpretation=legacy_rows_missing_item_hash")
	}
	if readyEmpty := c.phaseIntervalUnion(phase, "embed.ready_state.empty"); readyEmpty > 0 {
		parts = append(parts, fmt.Sprintf("ready_empty=%s", shortDur(readyEmpty)))
	}
	if readyNonzero := c.phaseIntervalUnion(phase, "embed.ready_state.nonzero"); readyNonzero > 0 {
		parts = append(parts, fmt.Sprintf("ready_nonzero=%s", shortDur(readyNonzero)))
	}
	for _, bucket := range []struct {
		label string
		name  string
	}{
		{label: "slots_0", name: "provider.slots.0"},
		{label: "slots_1_2", name: "provider.slots.1_2"},
		{label: "slots_3_6", name: "provider.slots.3_6"},
		{label: "slots_7_12", name: "provider.slots.7_12"},
		{label: "slots_13p", name: "provider.slots.13_plus"},
	} {
		if dur := c.phaseIntervalUnion(phase, bucket.name); dur > 0 {
			parts = append(parts, fmt.Sprintf("%s=%s", bucket.label, shortDur(dur)))
		}
	}
	if callEdgeFlushCount := c.phaseCounterTotal(phase, "calledge.flush.count"); callEdgeFlushCount > 0 {
		parts = append(parts, fmt.Sprintf("calledge_flushes=%d", callEdgeFlushCount))
		if callEdgePathsMax > 0 {
			parts = append(parts,
				fmt.Sprintf("calledge_paths_p50=%d", callEdgePathsP50),
				fmt.Sprintf("calledge_paths_p95=%d", callEdgePathsP95),
				fmt.Sprintf("calledge_paths_max=%d", callEdgePathsMax),
			)
		}
		if callEdgeWorkMax > 0 {
			parts = append(parts,
				fmt.Sprintf("calledge_work_p50=%d", callEdgeWorkP50),
				fmt.Sprintf("calledge_work_p95=%d", callEdgeWorkP95),
				fmt.Sprintf("calledge_work_max=%d", callEdgeWorkMax),
			)
		}
	}
	parts = c.appendSummaryLatencyMetrics(parts, phase, callEdgeLatencyMetrics[:])
	parts = c.appendSummaryCounterMetrics(parts, phase, callEdgeCounterMetrics[:])
	parts = c.appendSummaryCounterMetrics(parts, phase, callEdgeLookupCounterMetrics[:])
	parts = c.appendSummaryLatencyMetrics(parts, phase, codeStageLatencyMetrics[:])
	parts = c.appendSummaryLatencyMetrics(parts, phase, codeIndexLatencyMetrics[:])
	if noteFinalize := c.phaseLatencyTotal(phase, "noteembed.finalize"); noteFinalize > 0 {
		parts = append(parts, fmt.Sprintf("finalize_cum=%s", shortDur(noteFinalize)))
		if noteFinalizeMax > 0 {
			parts = append(parts,
				fmt.Sprintf("finalize_p50=%s", shortDur(noteFinalizeP50)),
				fmt.Sprintf("finalize_p95=%s", shortDur(noteFinalizeP95)),
				fmt.Sprintf("finalize_max=%s", shortDur(noteFinalizeMax)),
			)
		}
	}
	if noteReuseHits := c.phaseCounterTotal(phase, "noteembed.reuse_hits"); noteReuseHits > 0 {
		parts = append(parts, fmt.Sprintf("reuse_hits=%d", noteReuseHits))
	}
	parts = c.appendSummaryLatencyMetrics(parts, phase, noteFinalizeLatencyMetrics[:])
	parts = c.appendSummaryWritebackMetrics(parts, phase)
	parts = c.appendSummaryLatencyMetrics(parts, phase, codePersistLatencyMetrics[:])
	if queueDepthAvg := c.phaseGaugeAverage(phase, "queue.depth"); queueDepthAvg > 0 {
		parts = append(parts, fmt.Sprintf("queue_depth_avg=%.1f", queueDepthAvg))
	}
	if codeQueueDepthPeak := c.phaseGaugePeak(phase, "queue.code_depth"); codeQueueDepthPeak > 0 {
		parts = append(parts, fmt.Sprintf("code_queue_depth_peak=%d", codeQueueDepthPeak))
	}
	if codeQueueDepthAvg := c.phaseGaugeAverage(phase, "queue.code_depth"); codeQueueDepthAvg > 0 {
		parts = append(parts, fmt.Sprintf("code_queue_depth_avg=%.1f", codeQueueDepthAvg))
	}
	if otherQueueDepthPeak := c.phaseGaugePeak(phase, "queue.other_depth"); otherQueueDepthPeak > 0 {
		parts = append(parts, fmt.Sprintf("other_queue_depth_peak=%d", otherQueueDepthPeak))
	}
	if otherQueueDepthAvg := c.phaseGaugeAverage(phase, "queue.other_depth"); otherQueueDepthAvg > 0 {
		parts = append(parts, fmt.Sprintf("other_queue_depth_avg=%.1f", otherQueueDepthAvg))
	}
	if fileQueueDepthPeak := c.phaseGaugePeak(phase, "codeindex.file_queue_depth"); fileQueueDepthPeak > 0 {
		parts = append(parts, fmt.Sprintf("file_queue_depth_peak=%d", fileQueueDepthPeak))
	}
	if fileQueueDepthAvg := c.phaseGaugeAverage(phase, "codeindex.file_queue_depth"); fileQueueDepthAvg > 0 {
		parts = append(parts, fmt.Sprintf("file_queue_depth_avg=%.1f", fileQueueDepthAvg))
	}
	if activeWorkersPeak := c.phaseGaugePeak(phase, "codeindex.active_workers"); activeWorkersPeak > 0 {
		parts = append(parts, fmt.Sprintf("active_workers_peak=%d", activeWorkersPeak))
	}
	if activeWorkersAvg := c.phaseGaugeAverage(phase, "codeindex.active_workers"); activeWorkersAvg > 0 {
		parts = append(parts, fmt.Sprintf("active_workers_avg=%.1f", activeWorkersAvg))
	}
	if semanticQueueDepthPeak := c.phaseGaugePeak(phase, "codeindex.semantic_queue_depth"); semanticQueueDepthPeak > 0 {
		parts = append(parts, fmt.Sprintf("semantic_queue_depth_peak=%d", semanticQueueDepthPeak))
	}
	if semanticQueueDepthAvg := c.phaseGaugeAverage(phase, "codeindex.semantic_queue_depth"); semanticQueueDepthAvg > 0 {
		parts = append(parts, fmt.Sprintf("semantic_queue_depth_avg=%.1f", semanticQueueDepthAvg))
	}
	if resultQueueDepthPeak := c.phaseGaugePeak(phase, "codeindex.result_queue_depth"); resultQueueDepthPeak > 0 {
		parts = append(parts, fmt.Sprintf("result_queue_depth_peak=%d", resultQueueDepthPeak))
	}
	if resultQueueDepthAvg := c.phaseGaugeAverage(phase, "codeindex.result_queue_depth"); resultQueueDepthAvg > 0 {
		parts = append(parts, fmt.Sprintf("result_queue_depth_avg=%.1f", resultQueueDepthAvg))
	}
	parts = c.appendSummaryWritebackQueueMetrics(parts, phase)
	if embedQueueAgeAvg := c.phaseLatencyAvg(phase, "embed.queue_age"); embedQueueAgeAvg > 0 {
		parts = append(parts, fmt.Sprintf("embed_queue_age_avg=%s", shortDur(embedQueueAgeAvg)))
	}
	if embedQueueAgeMax := c.phaseLatencyMax(phase, "embed.queue_age"); embedQueueAgeMax > 0 {
		parts = append(parts, fmt.Sprintf("embed_queue_age_max=%s", shortDur(embedQueueAgeMax)))
	}
	if batchOldestAgeMax := c.phaseLatencyMax(phase, "embed.batch_oldest_age"); batchOldestAgeMax > 0 {
		parts = append(parts, fmt.Sprintf("embed_batch_oldest_max=%s", shortDur(batchOldestAgeMax)))
	}
	if prepareIn > 0 || prepareOut > 0 {
		parts = append(parts, fmt.Sprintf("prepare_tasks=%d/%d", prepareIn, prepareOut))
	}
	if embedIn > 0 || embedOut > 0 {
		parts = append(parts, fmt.Sprintf("embed_texts=%d/%d", embedIn, embedOut))
	}
	if writerIn > 0 || writerOut > 0 {
		parts = append(parts, fmt.Sprintf("writer_cmds=%d", writerIn))
		parts = append(parts, fmt.Sprintf("writer_rows=%d", writerOut))
	}
	return parts
}
