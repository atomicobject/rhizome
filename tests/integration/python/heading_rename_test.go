//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cli "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestPythonFixture_NoteRenameHeadingEndToEnd(t *testing.T) {
	ws := fixture.NewWorkspace(t)

	require.NoError(t, os.MkdirAll(filepath.Join(ws.CodeRoot, "notes", "heading-rename"), 0o755))
	targetRel := "notes/heading-rename/target.md"
	sourceRel := "notes/heading-rename/source.md"
	require.NoError(t, os.WriteFile(filepath.Join(ws.CodeRoot, targetRel), []byte(`# Target

## Old Heading ^stable-old-heading

Durable target.

## Old Headng

Near-match heading that should not be rewritten.
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ws.CodeRoot, sourceRel), []byte(`# Source

Exact heading ref: [[notes/heading-rename/target#Old Heading]]

Already durable ref: [[notes/heading-rename/target#^stable-old-heading]]

Near-match but valid ref: [[notes/heading-rename/target#Old Headng]]

`+"```md"+`
Code sample remains literal: [[notes/heading-rename/target#Old Heading]]
`+"```"+`
`), 0o644))

	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot,
		"note", "rename-heading", targetRel, "Old Heading", "New Heading",
		"--vault", ws.CodeRoot,
		"--upgrade-to-block-id", "never",
		"--fallback", "heading",
	)
	require.NoError(t, err, stdout+stderr)
	require.Empty(t, stderr)

	var plan cli.RenameHeadingResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &plan))
	require.False(t, plan.Applied)
	require.Equal(t, 2, plan.MatchedReferences)
	require.Equal(t, 1, plan.Rewritten)
	require.Len(t, plan.Skipped, 2)
	require.Contains(t, skipReasons(plan.Skipped), "code_block")
	require.Contains(t, skipReasons(plan.Skipped), "ambiguous_target")

	stdout, stderr, err = runOntologyCLI(t, ws.CodeRoot,
		"note", "rename-heading", targetRel, "Old Heading", "New Heading",
		"--vault", ws.CodeRoot,
		"--upgrade-to-block-id", "never",
		"--fallback", "heading",
		"--apply",
	)
	require.NoError(t, err, stdout+stderr)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "rewrites: 1")

	target, err := os.ReadFile(filepath.Join(ws.CodeRoot, targetRel))
	require.NoError(t, err)
	require.Contains(t, string(target), "## New Heading ^stable-old-heading")

	source, err := os.ReadFile(filepath.Join(ws.CodeRoot, sourceRel))
	require.NoError(t, err)
	require.Contains(t, string(source), "[[notes/heading-rename/target#New Heading]]")
	require.Contains(t, string(source), "[[notes/heading-rename/target#^stable-old-heading]]")
	require.Contains(t, string(source), "[[notes/heading-rename/target#Old Headng]]")
	require.Contains(t, string(source), "Code sample remains literal: [[notes/heading-rename/target#Old Heading]]")

	stdout, stderr, err = runOntologyCLI(t, ws.CodeRoot,
		"validate", "fragile-external",
		"--vault", ws.CodeRoot,
		"--scope-note", sourceRel,
		"--scope-target", targetRel,
		"--max-issues", "20",
	)
	require.NoError(t, err, stdout+stderr)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "no fragile external heading drift")
}

func skipReasons(diags []cli.HeadingRenameDiagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, diag := range diags {
		out = append(out, diag.Reason)
	}
	return out
}
