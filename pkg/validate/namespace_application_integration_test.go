//go:build integration

package validate_test

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/serve"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// This subprocess calls public adapters against a genuine durable journal
// prepared by the owner-package test. It provides no mock recovery or exports.
func TestIntegrationNamespaceApplicationHelper(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	root := os.Getenv("RZM_NAMESPACE_TEST_ROOT")
	if root == "" {
		t.Skip("only invoked by native-journal integration fixtures")
	}
	ctx, cancel := context.WithTimeout(vaultconfig.WithEnv(context.Background(), env), 5*time.Second)
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
	schema, err := ontoschema.LoadSchema(root)
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
		result, renameErr := actions.RenameHeading(&repoconfig.Vault{Env: env, Name: root}, actions.RenameHeadingParams{
			Context: ctx, NoteMetadata: metadata, Path: "edit.md", OldHeading: "Old heading", NewHeading: "New heading", Apply: true,
			UpgradeToBlockID: actions.HeadingRenameUpgradeNever, Fallback: actions.HeadingRenameFallbackHeading,
		})
		err, applied = renameErr, result.Applied
	case "delete":
		err = actions.DeleteNote(&repoconfig.Vault{Env: env, Name: root}, actions.DeleteParams{Context: ctx, NotePath: "edit.md"})
		applied = err == nil
	case "tags":
		result, tagErr := actions.AddTagsToFilesWithWorkers(ctx, &repoconfig.Vault{Env: env, Name: root}, noteReader, []string{"namespace-fixture"}, []string{"edit.md"}, false, 1)
		err, applied = tagErr, result.NotesTouched > 0
	case "property":
		result, propertyErr := actions.SetPropertyOnFilesWithWorkers(ctx, &repoconfig.Vault{Env: env, Name: root}, noteReader, "summary", "Updated", []string{"edit.md"}, true, false, 1)
		err, applied = propertyErr, result.NotesTouched > 0
	case "held-link":
		lockPath := filepath.Join(root, ".rhizome/index.lock")
		lease, acquired, lockErr := indexlock.TryAcquireLease(root, indexlock.AcquireOptions{})
		require.NoError(t, lockErr)
		require.True(t, acquired)
		defer func() {
			if lease != nil {
				require.NoError(t, lease.Release())
			}
		}()
		projection, projectErr := ontology.ProjectNote(ctx, vaultDef, noteReader, schema, "edit.md")
		require.NoError(t, projectErr)
		refs := projection.Fields["topics"].SectionNodes
		require.Len(t, refs, 1)
		service := ontology.NodeLinkService{VaultDef: vaultDef, NoteReader: noteReader, Schema: schema, WriteLease: lease}
		result, linkErr := service.LinkTargets(ctx, ontology.LinkTargetRequest{Refs: refs, Ensure: ontology.EnsureLinkTargetApply})
		err, applied = linkErr, result.Applied
		secondRelease, secondAcquired, secondErr := indexlock.TryAcquire(lockPath)
		require.NoError(t, secondErr)
		if secondAcquired {
			require.NoError(t, secondRelease())
		}
		require.False(t, secondAcquired, "borrowed writer lease must remain caller-owned after the public adapter returns")
		require.NoError(t, lease.Release())
		lease = nil
	default:
		t.Fatal("unknown public adapter")
	}
	if os.Getenv("RZM_NAMESPACE_TEST_EXPECT") == "refuse" {
		require.ErrorContains(t, err, os.Getenv("RZM_NAMESPACE_TEST_TRANSACTION"))
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
