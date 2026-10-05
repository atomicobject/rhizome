package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatCodeOverviewOutput_Colorized(t *testing.T) {
	t.Parallel()

	markdown := "# Code Overview\n\n## pkg/foo/\n_files 5 | docs 3 | anchors 2_\n- Docs: foo — summary; bar\n- Anchors: Foo() (symbol) go mentions=2\n- Tests: pkg/foo_test.go\n"

	colored := formatCodeOverviewOutput(markdown, true)

	require.Contains(t, colored, lightGreen+"# Code Overview"+reset)
	require.Contains(t, colored, "## "+green+"pkg/foo/"+reset)
	require.Contains(t, colored, dimWhite+"_files 5 | docs 3 | anchors 2_"+reset)
	require.Contains(t, colored, "- "+dimWhite+"Docs:"+reset+" "+teal+"foo"+reset+" — "+dimWhite+"summary"+reset)
	require.Contains(t, colored, "- "+dimWhite+"Anchors:"+reset+" "+teal+"Foo()"+reset+" "+dimWhite+"(symbol) go mentions=2"+reset)
	require.Contains(t, colored, "- "+dimWhite+"Tests:"+reset+" "+dimWhite+"pkg/foo_test.go"+reset)
}

func TestFormatCodeOverviewOutput_NoColor(t *testing.T) {
	t.Parallel()

	markdown := "# Code Overview\n\n## pkg/foo/\n_files 5 | docs 3 | anchors 2_\n- Docs: foo — summary; bar\n- Anchors: Foo() (symbol) go mentions=2\n- Tests: pkg/foo_test.go\n"

	require.Equal(t, markdown, formatCodeOverviewOutput(markdown, false))
}
