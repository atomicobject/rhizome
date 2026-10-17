package indexingperf

import (
	"fmt"
	"strings"
	"time"
)

func (c *Collector) renderWindowPhaseLine(window time.Duration, phase string) string {
	parts := c.metricReportParts(phase, true, window)
	if len(parts) == 0 {
		return ""
	}
	prefix := "[index-perf]"
	if window > 0 {
		prefix += fmt.Sprintf(" window=%s", window.Truncate(time.Second))
	}
	if phase != "" {
		prefix += " phase=" + phase
	}
	return prefix + " " + strings.Join(parts, " ")
}
