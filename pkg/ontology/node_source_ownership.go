package ontology

import (
	"github.com/atomicobject/rhizome/pkg/sourceownership"
	"strings"
)

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
	if workspace.Content.Source != nil {
		if refusal, ok := policy.Check([]string{workspace.Node.NotePath}).(*sourceownership.Refusal); ok && !strings.Contains(strings.ToLower(reason), strings.ToLower(refusal.Owner)) {
			reason = "Managed by " + refusal.Owner + ". " + reason
		}
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
