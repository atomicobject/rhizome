package ontology

import (
	"github.com/atomicobject/rhizome/pkg/sourceownership"
	"github.com/stretchr/testify/require"
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
