package operations

import (
	"bytes"
	"io"
	"testing"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestSuppressionDiagnosticsBelongToInvocation(t *testing.T) {
	t.Parallel()
	reader := factReader{facts: NewNoteFacts([]notemeta.NoteSourceSnapshot{
		{Path: paths.NormalizeNotePath("private.future"), Tags: []string{"Context/Private"}},
	})}
	files := []string{"private.future", "public.future", "missing.md"}
	for _, name := range []string{"debug", "quiet"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			var writer io.Writer
			if name == "debug" {
				writer = &output
			}
			filtered := FilterSuppressedFiles(files, reader, []string{"context"}, writer)
			require.Equal(t, []string{"public.future", "missing.md"}, filtered)
			if name == "debug" {
				require.Equal(t, "Suppressing file private.future due to suppressed tags\n", output.String())
			} else {
				require.Empty(t, output.String())
			}
		})
	}
}
