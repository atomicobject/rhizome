package validate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
)

// RepairFollowUp preserves source-bound, non-executable evidence discovered
// while constructing a repair. Follow-ups are fingerprinted review/report
// authority; they never become operations or block an otherwise valid repair.
type RepairFollowUp struct {
	MembershipKeys []string                             `json:"membershipKeys"`
	SourcePath     string                               `json:"sourcePath"`
	SourceHash     string                               `json:"sourceHash"`
	Diagnostic     identifierreconcile.RepairDiagnostic `json:"diagnostic"`
}

func collectIdentifierRepairFollowUps(snapshot *identifierreconcile.RepairAssembly) ([]RepairFollowUp, error) {
	sourceHashes := make(map[string]string, len(snapshot.SourcePreconditions))
	for _, source := range snapshot.SourcePreconditions {
		sourceHashes[source.NotePath] = source.SourceHash
	}
	byKey := make(map[string]RepairFollowUp)
	for _, component := range snapshot.Components {
		for _, diagnostic := range component.Diagnostics {
			if diagnostic.Blocking {
				continue
			}
			path, err := repairDiagnosticSourcePath(diagnostic)
			if err != nil {
				return nil, err
			}
			hash := strings.TrimSpace(sourceHashes[path])
			if hash == "" {
				return nil, fmt.Errorf("identifier repair follow-up source %s is absent from the complete source snapshot", path)
			}
			followUp := RepairFollowUp{
				MembershipKeys: sortedUnique(diagnostic.MembershipKeys),
				SourcePath:     path,
				SourceHash:     hash,
				Diagnostic:     diagnostic,
			}
			followUp.Diagnostic.MembershipKeys = append([]string(nil), followUp.MembershipKeys...)
			byKey[identifierJSONKey(followUp)] = followUp
		}
	}
	followUps := make([]RepairFollowUp, 0, len(byKey))
	for _, followUp := range byKey {
		followUps = append(followUps, followUp)
	}
	sort.Slice(followUps, func(i, j int) bool { return identifierJSONKey(followUps[i]) < identifierJSONKey(followUps[j]) })
	return followUps, nil
}

func canonicalRepairFollowUps(input []RepairFollowUp) ([]RepairFollowUp, error) {
	if len(input) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var followUps []RepairFollowUp
	if err := json.Unmarshal(encoded, &followUps); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(followUps))
	for index := range followUps {
		followUp := &followUps[index]
		followUp.MembershipKeys = sortedUnique(followUp.MembershipKeys)
		followUp.SourcePath = strings.TrimSpace(followUp.SourcePath)
		followUp.SourceHash = strings.TrimSpace(followUp.SourceHash)
		followUp.Diagnostic.MembershipKeys = sortedUnique(followUp.Diagnostic.MembershipKeys)
		if followUp.SourcePath == "" || followUp.SourceHash == "" || len(followUp.MembershipKeys) == 0 {
			return nil, fmt.Errorf("repair follow-up requires source path, source hash, and membership")
		}
		canonicalPath, err := paths.CleanRelPath(followUp.SourcePath)
		if err != nil || canonicalPath.String() != followUp.SourcePath {
			return nil, fmt.Errorf("repair follow-up source path must be canonical and vault-relative")
		}
		if followUp.Diagnostic.Blocking {
			return nil, fmt.Errorf("blocking repair diagnostic cannot be reported as a follow-up")
		}
		if identifierJSONKey(followUp.MembershipKeys) != identifierJSONKey(followUp.Diagnostic.MembershipKeys) {
			return nil, fmt.Errorf("repair follow-up membership diverges from diagnostic evidence")
		}
		path, err := repairDiagnosticSourcePath(followUp.Diagnostic)
		if err != nil {
			return nil, err
		}
		if path != followUp.SourcePath {
			return nil, fmt.Errorf("repair follow-up source path diverges from diagnostic evidence")
		}
		key := identifierJSONKey(*followUp)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("duplicate repair follow-up evidence")
		}
		seen[key] = struct{}{}
	}
	sort.Slice(followUps, func(i, j int) bool { return identifierJSONKey(followUps[i]) < identifierJSONKey(followUps[j]) })
	return followUps, nil
}

func repairDiagnosticSourcePath(diagnostic identifierreconcile.RepairDiagnostic) (string, error) {
	if diagnostic.Field != nil && diagnostic.Link == nil && diagnostic.MoveConflict == nil {
		path := strings.TrimSpace(diagnostic.Field.OwnerRef.NotePath)
		if path == "" {
			return "", fmt.Errorf("identifier repair field follow-up requires a source path")
		}
		return path, nil
	}
	if diagnostic.Link != nil && diagnostic.Field == nil && diagnostic.MoveConflict == nil {
		path := strings.TrimSpace(diagnostic.Link.NotePath)
		if path == "" {
			return "", fmt.Errorf("identifier repair link follow-up requires a source path")
		}
		return path, nil
	}
	return "", fmt.Errorf("identifier repair follow-up requires exactly one field or link diagnostic")
}
