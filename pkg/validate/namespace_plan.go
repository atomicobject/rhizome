package validate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

type compiledNamespacePlan struct {
	operations  []RepairOperation
	transaction RepairTransaction
	fingerprint string
	summary     json.RawMessage
}

type namespaceEndpoint struct {
	rel, abs string
	info     os.FileInfo
}

func compileNamespacePlan(runCtx RunContext, plan NamespaceMutationPlan) (compiledNamespacePlan, error) {
	if err := validateNamespaceSummary(plan.Summary); err != nil {
		return compiledNamespacePlan{}, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return compiledNamespacePlan{}, err
	}
	identity := "namespace:" + hex.EncodeToString(token[:])
	operations := make([]RepairOperation, len(plan.Operations))
	var sources, destinations []namespaceEndpoint
	var writes []namespaceEndpoint
	for i, input := range plan.Operations {
		if input.Kind != RepairOperationRename && input.Kind != RepairOperationWrite {
			return compiledNamespacePlan{}, fmt.Errorf("namespace operation must be a rename or write")
		}
		if input.SourceMode == nil || !validNamespaceHash(input.SourceHash) {
			return compiledNamespacePlan{}, fmt.Errorf("namespace source hash and mode are required for %s", input.Path)
		}
		if input.Lifecycle.Decision != "" || input.Lifecycle.Reason != "" || len(input.Lifecycle.ProtectedRanges) > 0 || len(input.LifecycleClaims) > 0 || len(input.RequiredChecks) > 0 || len(input.PlanningConflicts) > 0 || input.DestinationVacancy != nil {
			return compiledNamespacePlan{}, fmt.Errorf("namespace operation contains generic repair policy")
		}
		input.ID = fmt.Sprintf("%s:%08d", identity, i)
		input.ActionID, input.IssueKey = "", ""
		input.ActionIDs, input.IssueKeys = nil, nil
		input.Identities = []string{identity}
		op, err := canonicalRepairOperation(input)
		if err != nil {
			return compiledNamespacePlan{}, err
		}
		source, err := inspectNamespaceEndpoint(runCtx, op.Path, true)
		if err != nil {
			return compiledNamespacePlan{}, err
		}
		op.Path = source.rel
		if op.Kind == RepairOperationRename {
			if op.DestinationState == nil {
				return compiledNamespacePlan{}, fmt.Errorf("namespace rename destination witness is required")
			}
			destination, err := inspectNamespaceEndpoint(runCtx, op.DestinationPath, false)
			if err != nil {
				return compiledNamespacePlan{}, err
			}
			op.DestinationPath = destination.rel
			if (source.abs == destination.abs || (source.info != nil && destination.info != nil && os.SameFile(source.info, destination.info))) && op.DestinationState.Kind != RepairDestinationCaseOnly {
				return compiledNamespacePlan{}, fmt.Errorf("namespace rename source and destination are the same file")
			}
			sources = append(sources, source)
			destinations = append(destinations, destination)
		} else {
			writes = append(writes, source)
		}
		operations[i] = op
	}
	if len(sources) == 0 {
		return compiledNamespacePlan{}, fmt.Errorf("namespace plan requires a rename")
	}
	for i, source := range sources {
		for j := 0; j < i; j++ {
			if sameNamespaceEndpoint(source, sources[j]) || sameNamespaceEndpoint(destinations[i], destinations[j]) || namespaceEndpointAncestry(destinations[i], destinations[j]) || sameNamespaceEndpoint(source, destinations[j]) || sameNamespaceEndpoint(destinations[i], sources[j]) {
				return compiledNamespacePlan{}, fmt.Errorf("namespace move endpoints overlap: %s and %s", source.rel, sources[j].rel)
			}
		}
	}
	for _, write := range writes {
		for i, destination := range destinations {
			if sameNamespaceEndpoint(write, destination) && write.rel != sources[i].rel {
				return compiledNamespacePlan{}, fmt.Errorf("namespace write addresses a move destination: %s", write.rel)
			}
		}
	}
	// Exact original source names own spans. Portable grouping may connect extra
	// paths, but actual endpoint admission above owns native namespace conflicts.
	transactions, err := GroupRepairTransactions(operations)
	if err != nil || len(transactions) != 1 {
		return compiledNamespacePlan{}, fmt.Errorf("namespace plan must form one transaction: %w", err)
	}
	for _, conflict := range transactions[0].Conflicts {
		if conflict.Kind == RepairConflictEditOverlap {
			return compiledNamespacePlan{}, fmt.Errorf("namespace edits overlap at %s", conflict.Path)
		}
	}
	for i, op := range operations {
		if op.Kind != RepairOperationWrite {
			continue
		}
		for _, other := range operations[:i] {
			if other.Kind == RepairOperationWrite && other.Path == op.Path && (op.Content != nil || other.Content != nil) {
				return compiledNamespacePlan{}, fmt.Errorf("namespace full-file writes overlap at %s", op.Path)
			}
		}
	}
	transactions[0].Conflicts = nil
	fingerprint, err := RepairPlanFingerprint(RepairPlan{Operations: operations})
	if err != nil {
		return compiledNamespacePlan{}, err
	}
	return compiledNamespacePlan{operations: operations, transaction: transactions[0], fingerprint: fingerprint, summary: append(json.RawMessage(nil), plan.Summary...)}, nil
}

func inspectNamespaceEndpoint(runCtx RunContext, rel string, source bool) (namespaceEndpoint, error) {
	if namespaceInternalPath(rel) {
		return namespaceEndpoint{}, fmt.Errorf("namespace operation cannot address internal path %s", rel)
	}
	abs, err := repairAbsPath(runCtx, rel)
	if err != nil {
		return namespaceEndpoint{}, err
	}
	canonical, err := filepath.Rel(filepath.FromSlash(runCtx.VaultPath), abs)
	if err != nil || strings.HasPrefix(canonical, ".."+string(filepath.Separator)) {
		return namespaceEndpoint{}, fmt.Errorf("namespace endpoint escapes vault")
	}
	rel = filepath.ToSlash(canonical)
	if namespaceInternalPath(rel) {
		return namespaceEndpoint{}, fmt.Errorf("namespace operation resolves to internal path %s", rel)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if !source && os.IsNotExist(err) {
			return namespaceEndpoint{rel: rel, abs: abs}, nil
		}
		return namespaceEndpoint{}, err
	}
	if !info.Mode().IsRegular() {
		return namespaceEndpoint{}, fmt.Errorf("namespace endpoint is not a regular file: %s", rel)
	}
	if source {
		entries, err := os.ReadDir(filepath.Dir(abs))
		if err != nil {
			return namespaceEndpoint{}, err
		}
		// Resolve only the actual source entry. Preserve authored target spelling.
		for _, entry := range entries {
			if entry.Name() == filepath.Base(abs) {
				return namespaceEndpoint{rel: rel, abs: abs, info: info}, nil
			}
		}
		for _, entry := range entries {
			if !paths.CaseEqual(entry.Name(), filepath.Base(abs)) {
				continue
			}
			candidate := filepath.Join(filepath.Dir(abs), entry.Name())
			candidateInfo, err := os.Lstat(candidate)
			if err == nil && os.SameFile(info, candidateInfo) {
				abs = candidate
				rel = filepath.ToSlash(filepath.Join(filepath.Dir(rel), entry.Name()))
				break
			}
		}
	}
	return namespaceEndpoint{rel: rel, abs: abs, info: info}, nil
}

func namespaceInternalPath(rel string) bool {
	first := strings.Split(filepath.ToSlash(rel), "/")[0]
	return rel == "" || paths.CaseEqual(first, ".rhizome") || paths.CaseEqual(first, ".git")
}

func sameNamespaceEndpoint(a, b namespaceEndpoint) bool {
	return paths.CaseEqual(a.abs, b.abs) || (a.info != nil && b.info != nil && os.SameFile(a.info, b.info))
}

func namespaceEndpointAncestry(a, b namespaceEndpoint) bool {
	aParts, bParts := strings.Split(filepath.ToSlash(a.abs), "/"), strings.Split(filepath.ToSlash(b.abs), "/")
	if len(aParts) < len(bParts) {
		return paths.CaseEqual(filepath.ToSlash(a.abs), strings.Join(bParts[:len(aParts)], "/"))
	}
	if len(bParts) < len(aParts) {
		return paths.CaseEqual(filepath.ToSlash(b.abs), strings.Join(aParts[:len(bParts)], "/"))
	}
	return false
}

func validNamespaceHash(hash string) bool {
	return strings.HasPrefix(hash, "sha256:") && validRepairJournalDigest(strings.TrimPrefix(hash, "sha256:"))
}

func namespaceMoves(operations []RepairOperation) []PathRename {
	var moves []PathRename
	for _, op := range operations {
		if op.Kind == RepairOperationRename {
			moves = append(moves, PathRename{From: op.Path, To: op.DestinationPath})
		}
	}
	return moves
}
