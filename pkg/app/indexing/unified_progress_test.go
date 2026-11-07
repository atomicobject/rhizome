package indexing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type progressRecorder struct {
	lines   []string
	updates [][2]int
	labels  []string
}

func (p *progressRecorder) Println(msg string) {
	p.lines = append(p.lines, msg)
}

func (p *progressRecorder) Update(done, total int) {
	p.updates = append(p.updates, [2]int{done, total})
}

func (p *progressRecorder) SetLabel(label string) {
	p.labels = append(p.labels, label)
}

func TestIntegratedProgressScalesStagesIntoSingleBar(t *testing.T) {
	t.Parallel()

	base := &progressRecorder{}
	progress := newIntegratedProgress(base, false)

	progress.SetSegment(0, mainPhaseEnd)
	progress.Update(50, 100)
	progress.AdvanceTo("Flushing semantic writes", finalDrainEnd)
	progress.AdvanceTo("Finalizing metadata", metadataSyncEnd)
	progress.AdvanceTo("Computing graph", ontologyEnd)
	progress.AdvanceTo("Saving index state", graphEnd)
	progress.Finish("Done")
	require.Equal(t, []string{"Indexing code", "Flushing semantic writes", "Finalizing metadata", "Computing graph", "Saving index state", "Done"}, base.labels)

	require.Equal(t, [][2]int{{0, 1000}, {475, 1000}, {978, 1000}, {989, 1000}, {992, 1000}, {994, 1000}, {1000, 1000}}, base.updates)
}

func TestIntegratedProgressSuppressesNonVerboseLines(t *testing.T) {
	t.Parallel()

	base := &progressRecorder{}
	progress := newIntegratedProgress(base, false)

	progress.Println("[index] Indexing code…")
	progress.Println("[index] Warning: slow phase")

	if len(base.lines) != 1 || base.lines[0] != "[index] Warning: slow phase" {
		t.Fatalf("got lines %v", base.lines)
	}
}

func TestIntegratedProgressLeavesVisibleHeadroomBeforeFinish(t *testing.T) {
	t.Parallel()

	base := &progressRecorder{}
	progress := newIntegratedProgress(base, false)

	progress.AdvanceTo("Computing graph", graphEnd)
	progress.AdvanceTo("Saving index state", graphEnd)
	progress.AdvanceTo("Optimizing index", graphEnd)

	last := base.updates[len(base.updates)-1]
	if got, want := last[0], graphEnd; got != want {
		t.Fatalf("last pre-finish progress = %d want %d", got, want)
	}
	if graphEnd >= 995 {
		t.Fatalf("graphEnd=%d leaves too little headroom before finish", graphEnd)
	}
}
