package cmd

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
)

// codeCallMetrics aggregates one execution's host calls per catalog operation
// for its command report (SPEC-0116). The server admits only cataloged names,
// so cardinality is bounded; inputs and outputs are never recorded.
type codeCallMetrics struct {
	mu         sync.Mutex
	operations map[string]*codeOperationMetrics
}

type codeOperationMetrics struct {
	Operation string  `json:"operation"`
	Count     int     `json:"count"`
	Errors    int     `json:"errors"`
	TotalMS   float64 `json:"totalMs"`
	MaxMS     float64 `json:"maxMs"`
	InProcess int     `json:"inProcess"`
	Forwarded int     `json:"forwarded"`
}

type codeExecuteMetrics struct {
	NodeStartupMS float64 `json:"nodeStartupMs,omitempty"`
	ScriptMS      float64 `json:"scriptMs,omitempty"`
	HostCalls     struct {
		Count   int     `json:"count"`
		Errors  int     `json:"errors"`
		TotalMS float64 `json:"totalMs"`
	} `json:"hostCalls"`
	Operations []codeOperationMetrics `json:"operations"`
}

type codeCallMetricsKey struct{}

func withCodeCallMetrics(ctx context.Context, metrics *codeCallMetrics) context.Context {
	return context.WithValue(ctx, codeCallMetricsKey{}, metrics)
}

func codeCallMetricsFromContext(ctx context.Context) *codeCallMetrics {
	metrics, _ := ctx.Value(codeCallMetricsKey{}).(*codeCallMetrics)
	return metrics
}

func (m *codeCallMetrics) record(name string, forwarded, failed bool, elapsed time.Duration) {
	if m == nil {
		return
	}
	ms := milliseconds(elapsed)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operations == nil {
		m.operations = map[string]*codeOperationMetrics{}
	}
	operation := m.operations[name]
	if operation == nil {
		operation = &codeOperationMetrics{Operation: name}
		m.operations[name] = operation
	}
	operation.Count++
	operation.TotalMS += ms
	operation.MaxMS = max(operation.MaxMS, ms)
	if failed {
		operation.Errors++
	}
	if forwarded {
		operation.Forwarded++
	} else {
		operation.InProcess++
	}
}

func (m *codeCallMetrics) report(timing agentcode.ExecuteTiming) codeExecuteMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	report := codeExecuteMetrics{NodeStartupMS: milliseconds(timing.Startup), ScriptMS: milliseconds(timing.Script), Operations: []codeOperationMetrics{}}
	for _, operation := range m.operations {
		report.HostCalls.Count += operation.Count
		report.HostCalls.Errors += operation.Errors
		report.HostCalls.TotalMS += operation.TotalMS
		rounded := *operation
		rounded.TotalMS = roundMilliseconds(rounded.TotalMS)
		report.Operations = append(report.Operations, rounded)
	}
	report.HostCalls.TotalMS = roundMilliseconds(report.HostCalls.TotalMS)
	sort.Slice(report.Operations, func(i, j int) bool { return report.Operations[i].Operation < report.Operations[j].Operation })
	return report
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

// roundMilliseconds drops float noise from summed microsecond measurements.
func roundMilliseconds(ms float64) float64 {
	return math.Round(ms*1000) / 1000
}
