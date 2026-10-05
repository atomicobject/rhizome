package semantic

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func recordSemanticChunkCounts(ctx context.Context, chunks []SemanticChunk) {
	if len(chunks) > 0 {
		indexingperf.AddCount(ctx, "semantic.chunks", int64(len(chunks)))
	}
}

func recordCodeTaskChunkCounts(ctx context.Context, task codeTask) {
	recordCodeChunkDimensions(ctx, "semantic.code_chunks", task)
}

func recordPlannedCodeTaskCounts(ctx context.Context, tasks []codeTask) {
	for _, task := range tasks {
		recordCodeChunkDimensions(ctx, "semantic.code_plan", task)
	}
}

func recordCodeChunkDimensions(ctx context.Context, prefix string, task codeTask) {
	kind := sanitizeMetricPart(task.payload.ownerKind)
	if kind == "" {
		kind = "unknown"
	}
	for _, chunk := range task.payload.chunks {
		granularity := sanitizeMetricPart(chunk.Input.Granularity)
		if granularity == "" {
			granularity = "unknown"
		}
		indexingperf.AddCount(ctx, prefix+".kind."+kind, 1)
		indexingperf.AddCount(ctx, prefix+".granularity."+granularity, 1)
		indexingperf.AddCount(ctx, prefix+".kind_granularity."+kind+"."+granularity, 1)
	}
}

func sanitizeMetricPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
