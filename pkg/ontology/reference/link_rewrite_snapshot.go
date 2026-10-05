package reference

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

type structuredLinkRewritePayload struct {
	NotePath          string                  `json:"notePath,omitempty"`
	SourceFingerprint string                  `json:"sourceFingerprint,omitempty"`
	Edits             []StructuredLinkEdit    `json:"edits"`
	Diagnostics       []LinkRewriteDiagnostic `json:"diagnostics,omitempty"`
}

func finalizeStructuredLinkRewritePlan(plan StructuredLinkRewritePlan) StructuredLinkRewritePlan {
	plan = cloneStructuredLinkRewritePlan(plan)
	plan.Fingerprint = structuredLinkRewriteFingerprint(plan)
	plan.sealed = plan.Fingerprint
	return plan
}

// ValidatedSnapshot returns a detached copy only when the exported plan still
// matches the canonical payload sealed by the planner.
func (plan StructuredLinkRewritePlan) ValidatedSnapshot() (*StructuredLinkRewritePlan, error) {
	fingerprint := structuredLinkRewriteFingerprint(plan)
	if plan.sealed == "" || plan.Fingerprint != plan.sealed || fingerprint != plan.sealed {
		return nil, fmt.Errorf("structured link rewrite plan changed after planning")
	}
	snapshot := cloneStructuredLinkRewritePlan(plan)
	return &snapshot, nil
}

func structuredLinkRewriteFingerprint(plan StructuredLinkRewritePlan) string {
	payload, _ := json.Marshal(structuredLinkRewritePayload{
		NotePath: plan.NotePath, SourceFingerprint: plan.SourceFingerprint, Edits: plan.Edits, Diagnostics: plan.Diagnostics,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func cloneStructuredLinkRewritePlan(plan StructuredLinkRewritePlan) StructuredLinkRewritePlan {
	out := plan
	out.Edits = append([]StructuredLinkEdit(nil), plan.Edits...)
	out.Diagnostics = append([]LinkRewriteDiagnostic(nil), plan.Diagnostics...)
	for i := range out.Diagnostics {
		out.Diagnostics[i].Candidates = append([]ontology.NodeRef(nil), plan.Diagnostics[i].Candidates...)
	}
	return out
}
