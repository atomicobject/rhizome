package diagnostics

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// maxTextGroups keeps the human view to the operations that cost the most
// time; --json carries every group.
const maxTextGroups = 25

func writePerf(out io.Writer, summary PerfSummary) error {
	var b strings.Builder
	if len(summary.Flags) == 0 {
		b.WriteString("No instability flags in the examined evidence.\n")
	} else {
		b.WriteString("Flags (observations to investigate, not diagnoses):\n")
	}
	for _, flag := range summary.Flags {
		fmt.Fprintf(&b, "  [%s] %s\n", flag.Kind, flag.Observation)
		fmt.Fprintf(&b, "    %d occurrences, %s to %s", flag.Count, flag.FirstAt.Format(time.RFC3339), flag.LastAt.Format(time.RFC3339))
		if len(flag.OperationIDs) > 0 {
			fmt.Fprintf(&b, "; operations: %s", strings.Join(flag.OperationIDs, ", "))
		}
		b.WriteByte('\n')
	}
	writeWebText(&b, summary.Web)
	if len(summary.Groups) == 0 {
		b.WriteString("No timed operations in the examined evidence.\n")
	} else {
		b.WriteString("Operations by total time (p95 shown at 20 or more samples):\n")
	}
	for i, group := range summary.Groups {
		if i == maxTextGroups {
			fmt.Fprintf(&b, "  ... %d more groups; use --json for all.\n", len(summary.Groups)-maxTextGroups)
			break
		}
		p95 := "-"
		if group.P95MS != nil {
			p95 = fmt.Sprintf("%dms", *group.P95MS)
		}
		fmt.Fprintf(&b, "  %s %s: n=%d total=%dms median=%dms p95=%s max=%dms", group.Kind, group.Name, group.Count, group.TotalMS, group.MedianMS, p95, group.MaxMS)
		if group.Errors > 0 || group.Canceled > 0 {
			fmt.Fprintf(&b, " errors=%d canceled=%d", group.Errors, group.Canceled)
		}
		if group.QueueWait != nil {
			fmt.Fprintf(&b, " queue(median/max)=%d/%dms", group.QueueWait.MedianMS, group.QueueWait.MaxMS)
		}
		if group.LockWait != nil {
			fmt.Fprintf(&b, " lock(median/max)=%d/%dms", group.LockWait.MedianMS, group.LockWait.MaxMS)
		}
		b.WriteByte('\n')
	}
	_, err := io.WriteString(out, b.String())
	return err
}
