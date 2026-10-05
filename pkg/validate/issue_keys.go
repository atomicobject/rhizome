package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// StableIssueKey returns a content-derived issue identity independent of
// output order, truncation, and JSON object key order.
func StableIssueKey(check string, issue Issue) (string, error) {
	var data any
	if len(issue.Data) > 0 {
		if err := json.Unmarshal(issue.Data, &data); err != nil {
			return "", err
		}
	}
	issuePath, err := stableIssuePath(issue.Path)
	if err != nil {
		return "", err
	}
	sourcePath, err := stableIssuePath(issue.Source)
	if err != nil {
		return "", err
	}
	payload := struct {
		Check  string `json:"check"`
		Code   string `json:"code"`
		Path   string `json:"path"`
		Type   string `json:"type"`
		Field  string `json:"field"`
		Source string `json:"source"`
		Target string `json:"target"`
		Data   any    `json:"data,omitempty"`
	}{
		Check: check, Code: issue.Code, Path: issuePath, Type: issue.Type,
		Field: issue.Field, Source: sourcePath, Target: issue.Target, Data: data,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "issue:v1:" + hex.EncodeToString(sum[:]), nil
}

func stableIssuePath(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	rel, err := paths.CleanRelPath(value)
	if err != nil {
		return "", err
	}
	return rel.String(), nil
}

// StableActionKey returns a semantic action identity that excludes mutable
// presentation fields such as title, summary, question, and instance count.
func StableActionKey(action FixAction) (string, error) {
	edits := append([]FixEdit(nil), action.Edits...)
	for i := range edits {
		var err error
		edits[i].NotePath, err = stableIssuePath(edits[i].NotePath)
		if err != nil {
			return "", err
		}
		edits[i].SourcePath, err = stableIssuePath(edits[i].SourcePath)
		if err != nil {
			return "", err
		}
	}
	affected, err := stableActionPaths(action.AffectedPaths)
	if err != nil {
		return "", err
	}
	candidates, err := stableActionPaths(action.CandidatePaths)
	if err != nil {
		return "", err
	}
	payload := struct {
		Check          string    `json:"check"`
		IssueCode      string    `json:"issueCode"`
		Kind           string    `json:"kind"`
		Safety         FixSafety `json:"safety"`
		IssueKeys      []string  `json:"issueKeys,omitempty"`
		AffectedPaths  []string  `json:"affectedPaths,omitempty"`
		CandidatePaths []string  `json:"candidatePaths,omitempty"`
		Edits          []FixEdit `json:"edits,omitempty"`
	}{
		Check:          action.Check,
		IssueCode:      action.IssueCode,
		Kind:           action.Kind,
		Safety:         action.Safety,
		IssueKeys:      sortedUnique(action.IssueKeys),
		AffectedPaths:  affected,
		CandidatePaths: candidates,
		Edits:          edits,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "action:v1:" + hex.EncodeToString(sum[:]), nil
}

func stableActionPaths(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized, err := stableIssuePath(value)
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	return sortedUnique(result), nil
}
