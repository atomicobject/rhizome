package ontology

import (
	"github.com/atomicobject/rhizome/pkg/sourceownership"
	"strings"
)

// ApplySourceOwnership overlays current host-file ownership on every native
// edit capability, including fallback nodes and nested body workspaces. An
// unversioned workspace stays unversioned.
func ApplySourceOwnership(policy *sourceownership.Policy, workspace *NodeWorkspace) {
	if workspace == nil {
		return
	}
	reason := sourceOwnershipReason(policy, workspace.Node.NotePath, workspace.Content.Source != nil)
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
	if workspace.Version != "" {
		workspace.Version = buildNodeVersion(workspace)
	}
}

// sourceOwnershipReason is the read-only reason for notePath; native source
// records also name the owner.
func sourceOwnershipReason(policy *sourceownership.Policy, notePath string, nativeSource bool) string {
	reason := policy.ReadOnlyReason(notePath)
	if reason == "" || !nativeSource {
		return reason
	}
	if refusal, ok := policy.Check([]string{notePath}).(*sourceownership.Refusal); ok && !strings.Contains(strings.ToLower(reason), strings.ToLower(refusal.Owner)) {
		reason = "Managed by " + refusal.Owner + ". " + reason
	}
	return reason
}
