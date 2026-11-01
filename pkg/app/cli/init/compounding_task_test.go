package init

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// The compounding add-on installs its mining task as a module task that the
// task loader accepts (SPEC-0124 US8).
func TestCompoundingInstallsTheMineTask(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", repoconfig.LocalWorkflowConfig{})
	_, err := runInit(t, RunOptions{Dir: root, AddAddon: templateCompounding})
	require.NoError(t, err)

	rel := ".rhizome/modules/compounding/tasks/mine.yml"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)
	def, err := taskdef.ParseDefinition(rel, data)
	require.NoError(t, err)
	require.Equal(t, "compounding/mine", def.ID)
	require.Equal(t, taskdef.ScopePerson, def.Scope)
	require.Equal(t, 7*24*time.Hour, def.Cadence)
	require.Equal(t, []string{"node", ".rhizome/modules/compounding/scripts/task-check.mjs"}, def.Check.Command)
	require.FileExists(t, filepath.Join(root, ".rhizome", "modules", "compounding", "scripts", "task-check.mjs"))
	require.Contains(t, def.Prompt, "mine.mjs --harness <h> --task-mode --budget 10")
	require.Contains(t, def.Prompt, "scripts/report.mjs")
	requireUpToDate(t, root)
}
