package diagnostics

import (
	"strings"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
)

const webClientSubsystem = "web.client"

// WebSummary is filled in once the client event shape from SPEC-0116 B3 lands.
type WebSummary struct{}

type webAggregator struct{}

func newWebAggregator() *webAggregator { return &webAggregator{} }

func (w *webAggregator) visit(evidence.Event) {}

func (w *webAggregator) summaries() []WebSummary { return nil }

func (w *webAggregator) flags() []PerfFlag { return nil }

func writeWebText(*strings.Builder, []WebSummary) {}
