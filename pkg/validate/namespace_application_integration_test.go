//go:build integration

package validate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/cli/serve"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// This subprocess calls public adapters against a genuine durable journal
// prepared by the owner-package test. It provides no mock recovery or exports.
func TestIntegrationNamespaceApplicationHelper(t *testing.T) {
	root := os.Getenv("RZM_NAMESPACE_TEST_ROOT")
	if root == "" {
		t.Skip("only invoked by native-journal integration fixtures")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vaultDef := obsidian.VaultDefinition{Path: root}
	if os.Getenv("RZM_NAMESPACE_TEST_MODE") == "startup" {
		if os.Getenv("RZM_NAMESPACE_TEST_EXPECT") == "stabilize" {
			// This is the same public ontology recovery that startup invokes.
			// Its refusal before stabilization makes startup order observable.
			require.ErrorContains(t, ontology.RecoverInterruptedEditsContext(ctx, root), os.Getenv("RZM_NAMESPACE_TEST_TRANSACTION"))
			namespaceApplicationLeaseReleased(t, root)
		}
		require.NoError(t, serve.RecoverInterruptedWrites(ctx, vaultDef))
		namespaceApplicationLeaseReleased(t, root)
		return
	}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	noteReader := &obsidian.Note{}
	var applied bool
	switch os.Getenv("RZM_NAMESPACE_TEST_MODE") {
	case "edit-session":
		session := ontology.NewEditSession(vaultDef, noteReader, schema)
		require.NoError(t, session.SetScalarField(ontology.NodeRef{NotePath: "edit.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}, "summary", "Updated"))
		result, commitErr := session.Commit(ctx)
		err, applied = commitErr, result.Applied
	case "heading":
		formats, runtimeErr := builtin.NewRuntime()
		require.NoError(t, runtimeErr)
		metadata, indexerErr := notemeta.NewIndexer(formats)
		require.NoError(t, indexerErr)
		result, renameErr := actions.RenameHeading(&obsidian.Vault{Name: root}, actions.RenameHeadingParams{
			Context: ctx, NoteMetadata: metadata, Path: "edit.md", OldHeading: "Old heading", NewHeading: "New heading", Apply: true,
			UpgradeToBlockID: actions.HeadingRenameUpgradeNever, Fallback: actions.HeadingRenameFallbackHeading,
		})
		err, applied = renameErr, result.Applied
	case "delete":
		err = actions.DeleteNote(&obsidian.Vault{Name: root}, actions.DeleteParams{Context: ctx, NotePath: "edit.md"})
		applied = err == nil
	case "tags":
		result, tagErr := actions.AddTagsToFiles(ctx, &obsidian.Vault{Name: root}, noteReader, []string{"namespace-fixture"}, []string{"edit.md"}, false)
		err, applied = tagErr, result.NotesTouched > 0
	case "property":
		result, propertyErr := actions.SetPropertyOnFiles(ctx, &obsidian.Vault{Name: root}, noteReader, "summary", "Updated", []string{"edit.md"}, true, false)
		err, applied = propertyErr, result.NotesTouched > 0
	case "held-link":
		lockPath := filepath.Join(root, ".rhizome/index.lock")
		release, acquired, lockErr := indexlock.TryAcquire(lockPath)
		require.NoError(t, lockErr)
		require.True(t, acquired)
		defer func() {
			if release != nil {
				require.NoError(t, release())
			}
		}()
		projection, projectErr := ontology.ProjectNote(ctx, vaultDef, noteReader, schema, "edit.md")
		require.NoError(t, projectErr)
		refs := projection.Fields["topics"].SectionNodes
		require.Len(t, refs, 1)
		service := ontology.NodeLinkService{VaultDef: vaultDef, NoteReader: noteReader, Schema: schema, VaultWriteLeaseHeld: true}
		result, linkErr := service.LinkTargets(ctx, ontology.LinkTargetRequest{Refs: refs, Ensure: ontology.EnsureLinkTargetApply})
		err, applied = linkErr, result.Applied
		secondRelease, secondAcquired, secondErr := indexlock.TryAcquire(lockPath)
		require.NoError(t, secondErr)
		if secondAcquired {
			require.NoError(t, secondRelease())
		}
		require.False(t, secondAcquired, "borrowed writer lease must remain caller-owned after the public adapter returns")
		require.NoError(t, release())
		release = nil
	default:
		t.Fatal("unknown public adapter")
	}
	if os.Getenv("RZM_NAMESPACE_TEST_EXPECT") == "refuse" {
		require.ErrorContains(t, err, os.Getenv("RZM_NAMESPACE_TEST_TRANSACTION"))
		require.ErrorContains(t, err, "rzm validate fix --apply")
		require.False(t, applied)
	} else {
		require.NoError(t, err)
		require.True(t, applied, "settled terminal journal must permit a real source edit")
	}
	namespaceApplicationLeaseReleased(t, root)
}

func namespaceApplicationLeaseReleased(t *testing.T, root string) {
	t.Helper()
	// Probe before the subprocess exits: dead-PID reclamation must not hide a
	// public adapter that returned without releasing its owned writer lease.
	release, acquired, err := indexlock.TryAcquire(filepath.Join(root, ".rhizome/index.lock"))
	require.NoError(t, err)
	require.True(t, acquired, "public adapter leaked the vault writer lease")
	require.NoError(t, release())
}
