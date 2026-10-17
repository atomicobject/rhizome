package indexingperf

func (c *Collector) phaseSummaryParts(phase string) []string {
	return c.metricReportParts(phase, false, c.phaseDuration(phase))
}
