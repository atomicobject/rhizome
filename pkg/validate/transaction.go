package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// RepairOperationKind is the filesystem effect owned by one semantic repair.
type RepairOperationKind string

const (
	RepairOperationWrite  RepairOperationKind = "write"
	RepairOperationRename RepairOperationKind = "rename"
	RepairOperationDelete RepairOperationKind = "delete"
)

// ExpectedText is an exact byte-span precondition and replacement.
type ExpectedText struct {
	StartByte   int    `json:"startByte"`
	EndByte     int    `json:"endByte"`
	Text        string `json:"text"`
	Replacement string `json:"replacement,omitempty"`
}

// RepairDestinationVacancyPrecondition requires a rename destination to stay
// unoccupied through transaction preparation. It is stable plan authority;
// the directory inventory used to enforce it is deliberately runtime-only.
type RepairDestinationVacancyPrecondition struct {
	DestinationPath                string `json:"destinationPath"`
	RequireExactVacancy            bool   `json:"requireExactVacancy"`
	RequirePortableCaseFoldVacancy bool   `json:"requirePortableCaseFoldVacancy"`
}

// RepairDestinationState binds a native rename to its observed destination.
// Occupied content is rollback authority, never an overwrite permission alone.
type RepairDestinationState struct {
	Kind    RepairDestinationKind `json:"kind"`
	Content []byte                `json:"content,omitempty"`
	Hash    string                `json:"hash,omitempty"`
	Mode    uint32                `json:"mode,omitempty"`
}

type RepairDestinationKind string

const (
	RepairDestinationAbsent   RepairDestinationKind = "absent"
	RepairDestinationCaseOnly RepairDestinationKind = "case_only"
	RepairDestinationOccupied RepairDestinationKind = "occupied"
)

// RepairOperation is one deterministic filesystem operation. SourceHash is
// computed from raw on-disk bytes.
type RepairOperation struct {
	ID                 string                                `json:"id"`
	ActionID           string                                `json:"actionId,omitempty"`
	ActionIDs          []string                              `json:"actionIds,omitempty"`
	IssueKey           string                                `json:"issueKey,omitempty"`
	IssueKeys          []string                              `json:"issueKeys,omitempty"`
	RequiredChecks     []string                              `json:"requiredChecks,omitempty"`
	Kind               RepairOperationKind                   `json:"kind"`
	Path               string                                `json:"path"`
	DestinationPath    string                                `json:"destinationPath,omitempty"`
	DestinationVacancy *RepairDestinationVacancyPrecondition `json:"destinationVacancy,omitempty"`
	SourceHash         string                                `json:"sourceHash,omitempty"`
	SourceMode         *uint32                               `json:"sourceMode,omitempty"`
	DestinationState   *RepairDestinationState               `json:"destinationState,omitempty"`
	Expected           []ExpectedText                        `json:"expected,omitempty"`
	Identities         []string                              `json:"identities,omitempty"`
	Content            []byte                                `json:"content,omitempty"`
	Lifecycle          LifecyclePolicyResult                 `json:"lifecycle,omitempty"`
	LifecycleClaims    []LifecycleClaim                      `json:"lifecycleClaims,omitempty"`
	PlanningConflicts  []RepairConflict                      `json:"planningConflicts,omitempty"`
}

// RepairConflictKind identifies a transaction-local planning conflict.
type RepairConflictKind string

const (
	RepairConflictEditOverlap RepairConflictKind = "edit_overlap"
	RepairConflictDestination RepairConflictKind = "destination_collision"
	RepairConflictPathEffect  RepairConflictKind = "path_effect_collision"
	RepairConflictOntology    RepairConflictKind = "ontology_preview"
	RepairConflictIdentifier  RepairConflictKind = "identifier_reconciliation"
)

// RepairConflict blocks only the connected transaction that contains it.
type RepairConflict struct {
	Kind         RepairConflictKind `json:"kind"`
	Path         string             `json:"path,omitempty"`
	OperationIDs []string           `json:"operationIds,omitempty"`
	Message      string             `json:"message"`
}

// RepairTransaction is a connected component over affected paths/identities.
type RepairTransaction struct {
	ID            string           `json:"id"`
	OperationIDs  []string         `json:"operationIds"`
	AffectedPaths []string         `json:"affectedPaths"`
	Checks        []string         `json:"checks,omitempty"`
	Identities    []string         `json:"identities,omitempty"`
	Conflicts     []RepairConflict `json:"conflicts,omitempty"`
}

type repairPathEffect struct {
	operationID string
	kind        RepairOperationKind
	role        string
	path        string
}

// SourceHash hashes raw source bytes without newline or encoding conversion.
func SourceHash(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// GroupRepairTransactions creates deterministic connected components. Semantic
// conflicts are attached to their component so independent work remains usable.
func GroupRepairTransactions(operations []RepairOperation) ([]RepairTransaction, error) {
	if len(operations) == 0 {
		return nil, nil
	}
	ops := append([]RepairOperation(nil), operations...)
	seenIDs := make(map[string]struct{}, len(ops))
	for i := range ops {
		canonical, err := canonicalRepairOperation(ops[i])
		if err != nil {
			return nil, err
		}
		ops[i] = canonical
		if ops[i].ID == "" {
			return nil, fmt.Errorf("repair operation id is required")
		}
		if _, exists := seenIDs[ops[i].ID]; exists {
			return nil, fmt.Errorf("duplicate repair operation id %q", ops[i].ID)
		}
		seenIDs[ops[i].ID] = struct{}{}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].ID < ops[j].ID })

	parent := make([]int, len(ops))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	pathOwner := map[string]int{}
	identityOwner := map[string]int{}
	for i, operation := range ops {
		for _, affected := range operationPaths(operation) {
			key := repairCollisionKey(affected)
			if owner, ok := pathOwner[key]; ok {
				union(i, owner)
			} else {
				pathOwner[key] = i
			}
		}
		for _, identity := range operation.Identities {
			if owner, ok := identityOwner[identity]; ok {
				union(i, owner)
			} else {
				identityOwner[identity] = i
			}
		}
	}

	components := map[int][]RepairOperation{}
	for i, operation := range ops {
		root := find(i)
		components[root] = append(components[root], operation)
	}
	transactions := make([]RepairTransaction, 0, len(components))
	for _, component := range components {
		transactions = append(transactions, newRepairTransaction(component))
	}
	sort.Slice(transactions, func(i, j int) bool {
		return strings.Join(transactions[i].OperationIDs, "\x00") <
			strings.Join(transactions[j].OperationIDs, "\x00")
	})
	return transactions, nil
}

func newRepairTransaction(operations []RepairOperation) RepairTransaction {
	sort.Slice(operations, func(i, j int) bool { return operations[i].ID < operations[j].ID })
	var ids, affected, identities []string
	var conflicts []RepairConflict
	for _, operation := range operations {
		ids = append(ids, operation.ID)
		affected = append(affected, operationPaths(operation)...)
		identities = append(identities, operation.Identities...)
		conflicts = append(conflicts, operation.PlanningConflicts...)
	}
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	return RepairTransaction{
		ID:            "transaction:v1:" + hex.EncodeToString(sum[:]),
		OperationIDs:  ids,
		AffectedPaths: sortedUnique(affected),
		Identities:    sortedUnique(identities),
		Conflicts:     append(conflicts, repairOperationConflicts(operations)...),
	}
}

func repairOperationConflicts(operations []RepairOperation) []RepairConflict {
	type ownedSpan struct {
		operationID string
		span        ExpectedText
	}
	byPath := map[string][]ownedSpan{}
	for _, operation := range operations {
		for _, expected := range operation.Expected {
			byPath[operation.Path] = append(byPath[operation.Path], ownedSpan{
				operationID: operation.ID,
				span:        expected,
			})
		}
	}
	conflicts := repairPathEffectConflicts(operations)
	for sourcePath, spans := range byPath {
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].span.StartByte != spans[j].span.StartByte {
				return spans[i].span.StartByte < spans[j].span.StartByte
			}
			return spans[i].span.EndByte < spans[j].span.EndByte
		})
		for i := 1; i < len(spans); i++ {
			if spans[i].span.StartByte >= spans[i-1].span.EndByte {
				continue
			}
			ids := sortedUnique([]string{spans[i-1].operationID, spans[i].operationID})
			conflicts = append(conflicts, RepairConflict{
				Kind:         RepairConflictEditOverlap,
				Path:         sourcePath,
				OperationIDs: ids,
				Message:      "expected edit spans overlap",
			})
		}
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Path != conflicts[j].Path {
			return conflicts[i].Path < conflicts[j].Path
		}
		return strings.Join(conflicts[i].OperationIDs, "\x00") <
			strings.Join(conflicts[j].OperationIDs, "\x00")
	})
	return conflicts
}

func repairPathEffectConflicts(operations []RepairOperation) []RepairConflict {
	byPath := map[string][]repairPathEffect{}
	for _, operation := range operations {
		add := func(value, role string) {
			if value == "" {
				return
			}
			key := repairCollisionKey(value)
			byPath[key] = append(byPath[key], repairPathEffect{
				operationID: operation.ID,
				kind:        operation.Kind,
				role:        role,
				path:        value,
			})
		}
		switch operation.Kind {
		case RepairOperationWrite:
			add(operation.Path, "output")
		case RepairOperationRename:
			add(operation.Path, "source")
			add(operation.DestinationPath, "output")
		case RepairOperationDelete:
			add(operation.Path, "delete")
		}
	}
	var conflicts []RepairConflict
	for _, effects := range byPath {
		operationIDs := make([]string, 0, len(effects))
		outputCount := 0
		allComposableWrites := true
		for _, effect := range effects {
			operationIDs = append(operationIDs, effect.operationID)
			if effect.role == "output" {
				outputCount++
			}
			if effect.kind != RepairOperationWrite || effect.role != "output" {
				allComposableWrites = false
			}
		}
		operationIDs = sortedUnique(operationIDs)
		if len(operationIDs) < 2 || allComposableWrites || composableRenameSourceWrites(effects) {
			continue
		}
		kind := RepairConflictPathEffect
		message := "operations have incompatible effects on the same path"
		if outputCount > 1 {
			kind = RepairConflictDestination
			message = "operations target the same destination"
		}
		conflicts = append(conflicts, RepairConflict{
			Kind:         kind,
			Path:         effects[0].path,
			OperationIDs: operationIDs,
			Message:      message,
		})
	}
	return conflicts
}

func composableRenameSourceWrites(effects []repairPathEffect) bool {
	renameSources := 0
	writes := 0
	for _, effect := range effects {
		switch {
		case effect.kind == RepairOperationRename && effect.role == "source":
			renameSources++
		case effect.kind == RepairOperationWrite && effect.role == "output":
			writes++
		default:
			return false
		}
	}
	return renameSources == 1 && writes > 0
}

func canonicalRepairOperation(operation RepairOperation) (RepairOperation, error) {
	var err error
	operation.Path, err = canonicalRepairPath(operation.Path)
	if err != nil {
		return RepairOperation{}, fmt.Errorf("operation %q source path: %w", operation.ID, err)
	}
	operation.DestinationPath, err = canonicalRepairPath(operation.DestinationPath)
	if err != nil {
		return RepairOperation{}, fmt.Errorf("operation %q destination path: %w", operation.ID, err)
	}
	if operation.SourceMode != nil {
		mode := *operation.SourceMode
		if mode != repairModeBits(repairFileMode(mode)) {
			return RepairOperation{}, fmt.Errorf("operation %q has unsupported source mode", operation.ID)
		}
		operation.SourceMode = &mode
	}
	if operation.DestinationState != nil {
		state := *operation.DestinationState
		state.Content = append([]byte(nil), state.Content...)
		if operation.Kind != RepairOperationRename || operation.DestinationVacancy != nil {
			return RepairOperation{}, fmt.Errorf("operation %q destination state requires a native rename", operation.ID)
		}
		switch state.Kind {
		case RepairDestinationAbsent, RepairDestinationCaseOnly:
			if len(state.Content) != 0 || state.Hash != "" || state.Mode != 0 {
				return RepairOperation{}, fmt.Errorf("operation %q unoccupied destination has content authority", operation.ID)
			}
		case RepairDestinationOccupied:
			if state.Hash != SourceHash(state.Content) || state.Mode != repairModeBits(repairFileMode(state.Mode)) {
				return RepairOperation{}, fmt.Errorf("operation %q occupied destination witness is invalid", operation.ID)
			}
		default:
			return RepairOperation{}, fmt.Errorf("operation %q destination state is unknown", operation.ID)
		}
		operation.DestinationState = &state
	}
	if operation.DestinationVacancy != nil {
		vacancy := *operation.DestinationVacancy
		vacancy.DestinationPath, err = canonicalRepairPath(vacancy.DestinationPath)
		if err != nil {
			return RepairOperation{}, fmt.Errorf("operation %q vacancy destination path: %w", operation.ID, err)
		}
		if vacancy.DestinationPath == "" {
			return RepairOperation{}, fmt.Errorf("operation %q vacancy destination path is required", operation.ID)
		}
		if !vacancy.RequireExactVacancy && !vacancy.RequirePortableCaseFoldVacancy {
			return RepairOperation{}, fmt.Errorf("operation %q destination vacancy requires at least one check", operation.ID)
		}
		operation.DestinationVacancy = &vacancy
	}
	operation.ActionIDs = append(operation.ActionIDs, operation.ActionID)
	operation.ActionIDs = sortedUnique(operation.ActionIDs)
	operation.IssueKeys = append(operation.IssueKeys, operation.IssueKey)
	operation.IssueKeys = sortedUnique(operation.IssueKeys)
	operation.RequiredChecks = sortedUnique(operation.RequiredChecks)
	for index, check := range operation.RequiredChecks {
		canonical, ok := CanonicalCheck(check)
		if !ok {
			return RepairOperation{}, fmt.Errorf("operation %q requires unknown check %q", operation.ID, check)
		}
		operation.RequiredChecks[index] = canonical
	}
	operation.RequiredChecks = sortedUnique(operation.RequiredChecks)
	if operation.ActionID == "" && len(operation.ActionIDs) > 0 {
		operation.ActionID = operation.ActionIDs[0]
	}
	if operation.IssueKey == "" && len(operation.IssueKeys) > 0 {
		operation.IssueKey = operation.IssueKeys[0]
	}
	for _, actionID := range operation.ActionIDs {
		operation.Identities = append(operation.Identities, "repair-action:"+actionID)
	}
	operation.Identities = sortedUnique(operation.Identities)
	operation.Expected = append([]ExpectedText(nil), operation.Expected...)
	sort.Slice(operation.Expected, func(i, j int) bool {
		if operation.Expected[i].StartByte != operation.Expected[j].StartByte {
			return operation.Expected[i].StartByte < operation.Expected[j].StartByte
		}
		return operation.Expected[i].EndByte < operation.Expected[j].EndByte
	})
	for _, expected := range operation.Expected {
		if expected.StartByte < 0 || expected.EndByte < expected.StartByte {
			return RepairOperation{}, fmt.Errorf(
				"operation %q has invalid expected span %d..%d",
				operation.ID, expected.StartByte, expected.EndByte,
			)
		}
	}
	if operation.Content != nil {
		operation.Content = append([]byte{}, operation.Content...)
	}
	operation.LifecycleClaims = append([]LifecycleClaim(nil), operation.LifecycleClaims...)
	sort.Slice(operation.LifecycleClaims, func(i, j int) bool {
		if operation.LifecycleClaims[i].StartByte != operation.LifecycleClaims[j].StartByte {
			return operation.LifecycleClaims[i].StartByte < operation.LifecycleClaims[j].StartByte
		}
		return operation.LifecycleClaims[i].EndByte < operation.LifecycleClaims[j].EndByte
	})
	operation.PlanningConflicts = append([]RepairConflict(nil), operation.PlanningConflicts...)
	sort.Slice(operation.PlanningConflicts, func(i, j int) bool {
		if operation.PlanningConflicts[i].Path != operation.PlanningConflicts[j].Path {
			return operation.PlanningConflicts[i].Path < operation.PlanningConflicts[j].Path
		}
		if operation.PlanningConflicts[i].Kind != operation.PlanningConflicts[j].Kind {
			return operation.PlanningConflicts[i].Kind < operation.PlanningConflicts[j].Kind
		}
		return operation.PlanningConflicts[i].Message < operation.PlanningConflicts[j].Message
	})
	switch operation.Kind {
	case RepairOperationWrite:
		if operation.Path == "" {
			return RepairOperation{}, fmt.Errorf("operation %q write source path is required", operation.ID)
		}
		if operation.DestinationPath != "" {
			return RepairOperation{}, fmt.Errorf("operation %q write destination path is not allowed", operation.ID)
		}
		if operation.DestinationVacancy != nil {
			return RepairOperation{}, fmt.Errorf("operation %q write destination vacancy is not allowed", operation.ID)
		}
	case RepairOperationRename:
		if operation.Path == "" || operation.DestinationPath == "" {
			return RepairOperation{}, fmt.Errorf("operation %q rename source and destination paths are required", operation.ID)
		}
		if operation.DestinationVacancy != nil && operation.DestinationVacancy.DestinationPath != operation.DestinationPath {
			return RepairOperation{}, fmt.Errorf("operation %q vacancy destination must equal rename destination", operation.ID)
		}
	case RepairOperationDelete:
		if operation.Path == "" {
			return RepairOperation{}, fmt.Errorf("operation %q delete source path is required", operation.ID)
		}
		if operation.DestinationPath != "" || len(operation.Content) > 0 {
			return RepairOperation{}, fmt.Errorf("operation %q delete destination or content is not allowed", operation.ID)
		}
		if operation.DestinationVacancy != nil {
			return RepairOperation{}, fmt.Errorf("operation %q delete destination vacancy is not allowed", operation.ID)
		}
	default:
		return RepairOperation{}, fmt.Errorf("operation %q has unknown kind %q", operation.ID, operation.Kind)
	}
	return operation, nil
}

func canonicalRepairPath(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	rel, err := paths.CleanRelPath(value)
	if err != nil {
		return "", err
	}
	return rel.String(), nil
}

func repairCollisionKey(value string) string {
	return strings.ToLower(value)
}

func operationPaths(operation RepairOperation) []string {
	return sortedUnique([]string{operation.Path, operation.DestinationPath})
}

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
