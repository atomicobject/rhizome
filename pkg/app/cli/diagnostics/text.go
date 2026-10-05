package diagnostics

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const maxTextAttributeBytes = 4096

func WriteText(out io.Writer, result Result) error {
	summaries := make(map[string]IndexSummary, len(result.IndexSummaries))
	for _, summary := range result.IndexSummaries {
		summaries[summary.OperationID] = summary
	}
	for _, report := range result.Reports {
		if _, err := fmt.Fprintf(out, "%s %s %s %s (%dms; queue %dms, lock %dms, execution %dms)\n", report.OperationID, report.Kind, report.Status, report.FinishedAt.Format(time.RFC3339), report.DurationMS, report.QueueWaitMS, report.LockWaitMS, report.ExecutionMS); err != nil {
			return err
		}
		if report.Trigger != "" {
			if _, err := fmt.Fprintln(out, "  trigger: "+report.Trigger); err != nil {
				return err
			}
		}
		if report.ReasonCode != "" {
			if _, err := fmt.Fprintln(out, "  reason: "+report.ReasonCode); err != nil {
				return err
			}
		}
		if report.Summary != "" {
			if _, err := fmt.Fprintln(out, "  "+report.Summary); err != nil {
				return err
			}
		}
		if report.Error != "" {
			if _, err := fmt.Fprintln(out, "  error: "+report.Error); err != nil {
				return err
			}
		}
		if err := writeTextAttributes(out, report.Attributes); err != nil {
			return err
		}
		if summary, ok := summaries[report.OperationID]; ok {
			if err := writeIndexSummary(out, summary); err != nil {
				return err
			}
		}
	}
	for _, event := range result.Events {
		if _, err := fmt.Fprintf(out, "%s %s %s %s [%s] %s\n", event.Time.Format(time.RFC3339), event.Level, event.Subsystem, event.Name, event.OperationID, event.Message); err != nil {
			return err
		}
		if err := writeTextAttributes(out, event.Attributes); err != nil {
			return err
		}
	}
	if len(result.Reports) == 0 && len(result.Events) == 0 {
		if _, err := fmt.Fprintln(out, "No retained matching records."); err != nil {
			return err
		}
	}
	for _, warning := range result.Coverage.Warnings {
		if _, err := fmt.Fprintln(out, "Evidence gap: "+warning); err != nil {
			return err
		}
	}
	if result.Coverage.Truncated {
		if _, err := fmt.Fprintln(out, "Evidence gap: the bounded read was truncated."); err != nil {
			return err
		}
	}
	return nil
}

func writeIndexSummary(out io.Writer, summary IndexSummary) error {
	for _, phase := range summary.SlowPhases {
		if _, err := fmt.Fprintf(out, "  phase %s: %s (%s)\n", phase.Name, time.Duration(phase.DurationNS), phase.Status); err != nil {
			return err
		}
	}
	for _, fallback := range summary.FallbackCounts {
		if _, err := fmt.Fprintf(out, "  reason %s: %d observed\n", fallback.Reason, fallback.Count); err != nil {
			return err
		}
	}
	for _, gap := range summary.EvidenceGaps {
		if _, err := fmt.Fprintln(out, "  evidence gap: "+gap); err != nil {
			return err
		}
	}
	return nil
}

// Keep retained attributes useful in text without dumping large payloads. Sorted
// admission makes both the displayed keys and partial-output marker deterministic.
func writeTextAttributes(out io.Writer, attributes map[string]any) error {
	if len(attributes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var compact strings.Builder
	compact.WriteByte('{')
	partial := false
	for _, key := range keys {
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return err
		}
		encodedValue, err := json.Marshal(attributes[key])
		if err != nil {
			return err
		}
		field := string(encodedKey) + ":" + string(encodedValue)
		if compact.Len()+len(field)+2 > maxTextAttributeBytes {
			partial = true
			continue
		}
		if compact.Len() > 1 {
			compact.WriteByte(',')
		}
		compact.WriteString(field)
	}
	compact.WriteByte('}')
	label := "  attributes: "
	if partial {
		label = "  attributes (partial; use --json for retained values): "
	}
	_, err := fmt.Fprintln(out, label+compact.String())
	return err
}
