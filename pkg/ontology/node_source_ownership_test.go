package ontology

import (
	"github.com/atomicobject/rhizome/pkg/sourceownership"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedWorkspaceReportsNodeAndFieldReason(t *testing.T) {
	root := t.TempDir()
	ownTestSource(t, root)
	workspace := &NodeWorkspace{Node: NodeDescriptor{NotePath: "owned.md"}, Capabilities: NodeCapabilities{CanEdit: true, CanEditFields: true, CanEditCollections: true, CanNavigateChildren: true}, Fields: []NodeFieldState{{Name: "title", Capability: NodeFieldCapability{WriteOperation: NodeFieldWriteOperationSetField}}}}
	ApplySourceOwnership(sourceownership.Load(root), workspace)
	require.False(t, workspace.Capabilities.CanEdit)
	require.False(t, workspace.Capabilities.CanEditFields)
	require.False(t, workspace.Capabilities.CanEditCollections)
	require.True(t, workspace.Capabilities.CanNavigateChildren)
	require.Equal(t, "Managed by AO KB. Change it with the AO KB skills.", workspace.Capabilities.ReadOnlyReason)
	require.Equal(t, workspace.Capabilities.ReadOnlyReason, workspace.Fields[0].Capability.ReadOnlyReason)
	require.Empty(t, workspace.Fields[0].Capability.WriteOperation)
}

func TestNativeWorkspaceNamesDeclaredOwnerWithoutRepeatingIt(t *testing.T) {
	for _, reason := range []string{"Change it with the owning skills.", "Managed by AO KB. Change it with the AO KB skills."} {
		t.Run(reason, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, sourceownership.DeclarationPath), []byte("sources:\n  - owner: AO KB\n    reason: "+reason+"\n    paths: [services.yaml]\n"), 0644))
			workspace := &NodeWorkspace{Node: NodeDescriptor{NotePath: "services.yaml"}, Content: NodeContent{Source: &NodeSourceContent{Representation: "source_value"}}}
			ApplySourceOwnership(sourceownership.Load(root), workspace)
			expected := reason
			if reason == "Change it with the owning skills." {
				expected = "Managed by AO KB. " + reason
			}
			require.Equal(t, expected, workspace.Capabilities.ReadOnlyReason)
		})
	}
}
