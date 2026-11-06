package ontology

import "github.com/atomicobject/rhizome/pkg/sourceownership"

// ApplySourceOwnership overlays current host-file ownership on every native
// edit capability, including fallback nodes and nested body workspaces.
func ApplySourceOwnership(policy *sourceownership.Policy, workspace *NodeWorkspace) {
	if workspace == nil {
		return
	}
	reason := policy.ReadOnlyReason(workspace.Node.NotePath)
	if reason == "" {
		return
	}
	workspace.Capabilities.CanEdit = false
	workspace.Capabilities.CanEditFields = false
	workspace.Capabilities.CanEditCollections = false
	workspace.Capabilities.ReadOnlyReason = reason
	for i := range workspace.Fields {
		workspace.Fields[i].Capability.WriteOperation = ""
		workspace.Fields[i].Capability.ReadOnlyReason = reason
	}
	workspace.Version = buildNodeVersion(workspace)
}
