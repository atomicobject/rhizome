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
	progress.SetTailStep("Finalizing", 4, 8)
	progress.Finish("Done")
	require.Equal(t, []string{"Indexing code", "Finalizing", "Done"}, base.labels)

	want := [][2]int{
		{0, unifiedProgressTotal},
		{475, unifiedProgressTotal},
		{950, unifiedProgressTotal},
		{975, unifiedProgressTotal},
		{1000, unifiedProgressTotal},
	}
	if len(base.updates) != len(want) {
		t.Fatalf("got %v updates want %v", base.updates, want)
	}
	for i := range want {
		if base.updates[i] != want[i] {
			t.Fatalf("update %d = %v want %v", i, base.updates[i], want[i])
		}
	}
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
