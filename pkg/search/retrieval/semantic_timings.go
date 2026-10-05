package retrieval

import (
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

type semanticTimingSink struct {
	t *search.Timings
}

func (s semanticTimingSink) Add(ev semantic.TimingEvent) {
	if s.t == nil {
		return
	}
	s.t.Add(search.TimingEvent{
		Name:     ev.Name,
		Kind:     ev.Kind,
		Started:  ev.Started,
		Duration: ev.Duration,
		Status:   ev.Status,
		Err:      ev.Err,
	})
}
