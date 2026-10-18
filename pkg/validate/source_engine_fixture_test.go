package validate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sourcetxn"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

type repairWriterFixture struct {
	run      RunContext
	writer   *sourcetxn.Writer
	recovery sourcetxn.RecoveryOutcomes
	close    func()
}

func openRepairWriterFixture(t *testing.T, run RunContext, hooks *repairExecutionHooks) (*repairWriterFixture, error) {
	t.Helper()
	root := run.VaultPath
	if root == "" {
		root = run.VaultDef.BasePath()
	}
	lease := run.sourceLease
	owned := false
	if lease != nil {
		if err := lease.Validate(root); err != nil {
			return nil, err
		}
	} else {
		var err error
		lease, owned, err = indexlock.TryAcquireLease(root, indexlock.AcquireOptions{Role: "test/source-coordinator"})
		if err != nil {
			return nil, err
		}
		if !owned {
			return nil, fmt.Errorf("fixture vault already has a writer")
		}
	}
	run.VaultPath, run.sourceLease = root, lease
	writer, recovered, err := sourcetxn.Open(root, lease, sourcetxn.WriterOptions{
		Role: sourcetxn.RepairOwner, RepairPolicy: validateSourceJournalChecks, Hooks: hooks,
	})
	run.sourceWriter = writer
	fixture := &repairWriterFixture{run: run, writer: writer, recovery: recovered}
	var once sync.Once
	fixture.close = func() {
		once.Do(func() {
			writer.Close()
			if owned {
				require.NoError(t, lease.Release())
			}
		})
	}
	t.Cleanup(fixture.close)
	return fixture, err
}

func (f *repairWriterFixture) prepare(fingerprint string, transaction RepairTransaction, operations []RepairOperation, historical bool, completion ...*RepairCompletionArtifact) (*sourcetxn.Transaction, error) {
	return prepareRepairTransaction(f.run, fingerprint, transaction, operations, historical, completion...)
}

func (f *repairWriterFixture) prepareNamespace(ctx context.Context, plan compiledNamespacePlan, withoutGit ...bool) (*sourcetxn.Transaction, error) {
	return prepareNativeNamespace(ctx, f.run, plan, withoutGit...)
}

// Fixture paths describe the durable wire layout; they never authorize mutation.
func repairFixtureJournalRoot(root string) string {
	return filepath.Join(root, ".rhizome", "repair-journal", "v1")
}

func repairFixtureJournalDir(root, transactionID string) string {
	return filepath.Join(repairFixtureJournalRoot(root), fmt.Sprintf("%x", sha256.Sum256([]byte(transactionID))))
}

// These helpers author persisted fixtures, not product publication paths.
func writeRepairJournalManifest(dir string, m repairJournalManifest) error {
	m.Version = 1
	if m.Namespace != nil {
		m.Version = 2
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), encoded, 0600)
}
func readRepairJournalManifest(dir string) (repairJournalManifest, error) {
	content, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return repairJournalManifest{}, err
	}
	var m repairJournalManifest
	err = json.Unmarshal(content, &m)
	return m, err
}
func repairArtifactPath(abs, short string, index int, kind string) string {
	return fmt.Sprintf("%s.rzm-repair-%s-%03d.%s", abs, short, index, kind)
}
