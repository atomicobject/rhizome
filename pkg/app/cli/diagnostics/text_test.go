package diagnostics

import (
	"bytes"
	"strings"
	"testing"
	"time"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestTextKeepsReportSummariesTogetherAndExplainsWarnings(t *testing.T) {
	result := Result{
		Reports: []evidence.Report{
			{OperationID: "index-first", Kind: "index", Status: "error", Trigger: "cli", ReasonCode: "provider_unavailable", Attributes: map[string]any{"files": 11}},
			{OperationID: "index-second", Kind: "index", Status: "success", Trigger: "startup", Attributes: map[string]any{"files": 22}},
		},
		IndexSummaries: []IndexSummary{
			{OperationID: "index-second", SlowPhases: []PhaseDuration{{Name: "second-phase", DurationNS: int64(2 * time.Second)}}},
			{OperationID: "index-first", SlowPhases: []PhaseDuration{{Name: "first-phase", DurationNS: int64(time.Second)}}},
		},
		Events: []evidence.Event{{Level: "WARN", Subsystem: "search", Name: "fallback", OperationID: "warning-operation", Attributes: map[string]any{"reason": "rank_timeout", "count": 3, "oversized": strings.Repeat("x", maxTextAttributeBytes+1)}}},
	}
	var output bytes.Buffer
	require.NoError(t, WriteText(&output, result))
	text := output.String()
	firstReport := strings.Index(text, "index-first index error")
	firstSummary := strings.Index(text, "phase first-phase: 1s")
	secondReport := strings.Index(text, "index-second index success")
	secondSummary := strings.Index(text, "phase second-phase: 2s")
	warning := strings.Index(text, "[warning-operation]")
	require.True(t, firstReport >= 0 && firstReport < firstSummary && firstSummary < secondReport && secondReport < secondSummary && secondSummary < warning, text)
	require.Contains(t, text, "trigger: cli\n  reason: provider_unavailable")
	require.Contains(t, text, `attributes: {"files":11}`)
	require.Contains(t, text, `attributes (partial; use --json for retained values): {"count":3,"reason":"rank_timeout"}`)
	require.NotContains(t, text, `"oversized"`)
	output.Reset()
	require.NoError(t, WriteText(&output, result))
	require.Equal(t, text, output.String(), "attribute admission and presentation must be deterministic")
}
