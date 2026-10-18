package ontology

import (
	"context"
	"errors"
	"fmt"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type EnsureLinkTargetMode string

const (
	EnsureLinkTargetNever EnsureLinkTargetMode = "never"
	EnsureLinkTargetPlan  EnsureLinkTargetMode = "plan"
	EnsureLinkTargetApply EnsureLinkTargetMode = "apply"
)

// NodeLocatorStatus describes whether a node has an author-facing address.
type NodeLocatorStatus string

const (
	NodeLocatorLinkable    NodeLocatorStatus = "linkable"
	NodeLocatorRequiresFix NodeLocatorStatus = "requires_fix"
	NodeLocatorUnsupported NodeLocatorStatus = "unsupported"
	NodeLocatorUnresolved  NodeLocatorStatus = "unresolved"
)

type NodeLinkService struct {
	notePaths    []string
	renderer     *wikilinkRenderer
	LoadSnapshot func(context.Context, string) (*DocumentSnapshot, error)
	VaultDef     obsidian.VaultDefinition
	NoteReader   obsidian.NoteReader
	Schema       *ontoschema.Schema
	WriteLease   *indexlock.Lease
}

// WithNotePaths prepares one immutable link renderer for a request's note list.
func (s NodeLinkService) WithNotePaths(notePaths []string) *NodeLinkService {
	s.notePaths = append([]string{}, notePaths...)
	renderer := newWikilinkRenderer(notePaths)
	s.renderer = &renderer
	return &s
}

type LinkTargetRequest struct {
	Refs    []NodeRef
	Ensure  EnsureLinkTargetMode
	Purpose string
}

type LinkTargetResult struct {
	// Applied reports that this request committed a source edit. It remains true
	// when the post-commit reread cannot produce refreshed targets, so callers
	// can complete any required durable convergence before reporting that error.
	Applied     bool                      `json:"applied,omitempty"`
	Targets     map[string]NodeLinkTarget `json:"targets,omitempty"`
	FixPlan     *NodeLinkFixPlan          `json:"fixPlan,omitempty"`
	Diagnostics []NodeLinkDiagnostic      `json:"diagnostics,omitempty"`
}

type NodeLinkTarget struct {
	Ref          NodeRef `json:"ref"`
	Markdown     string  `json:"markdown,omitempty"`
	Wikilink     string  `json:"wikilink,omitempty"`
	DisplayLabel string  `json:"displayLabel,omitempty"`
	Exists       bool    `json:"exists"`
	RequiresFix  bool    `json:"requiresFix,omitempty"`
	BlockID      string  `json:"blockId,omitempty"`
}

type NodeLinkFixPlan struct {
	Actions []NodeLinkFixAction `json:"actions,omitempty"`
}

type NodeLinkFixAction struct {
	Ref     NodeRef `json:"ref"`
	BlockID string  `json:"blockId"`
}

type NodeLinkDiagnostic struct {
	Code     string  `json:"code"`
	NotePath string  `json:"notePath,omitempty"`
	Ref      NodeRef `json:"ref,omitempty"`
	BlockID  string  `json:"blockId,omitempty"`
	Message  string  `json:"message"`
}

// NodeLocator projects a canonical NodeRef into the stable author-facing
// locator contract used by APIs and UI copy-link actions.
// Docs: [[ontology-browser-workspace#^spec-0014-us5-ac3]] and
// [[linkable-embedded-node-identifiers#^spec-0023-us1-ac3]] require explicit
// requires-fix status instead of fragile heading-only links.
type NodeLocator struct {
	Ref           NodeRef              `json:"ref"`
	Kind          NodeKind             `json:"kind"`
	SourceLocator string               `json:"sourceLocator"`
	LinkTarget    *NodeLinkTarget      `json:"linkTarget,omitempty"`
	Status        NodeLocatorStatus    `json:"status"`
	Diagnostics   []NodeLinkDiagnostic `json:"diagnostics,omitempty"`
	FixActions    []NodeLinkFixAction  `json:"fixActions,omitempty"`
}

// Locators returns structured locator projections. It only mutates files when
// the caller explicitly uses EnsureLinkTargetApply.
// Docs: [[linkable-embedded-node-identifiers#^spec-0023-us3-ac1]] owns that
// explicit write-capable make-linkable flow.
func (s *NodeLinkService) Locators(ctx context.Context, req LinkTargetRequest) (map[string]NodeLocator, error) {
	result, err := s.LinkTargets(ctx, req)
	if err != nil {
		return nil, err
	}
	diagnostics := map[string][]NodeLinkDiagnostic{}
	for _, diagnostic := range result.Diagnostics {
		key := nodeLocatorMapKey(normalizeLinkNodeRef(diagnostic.Ref))
		if key == "" {
			key = diagnostic.NotePath
		}
		diagnostics[key] = append(diagnostics[key], diagnostic)
	}
	fixes := map[string][]NodeLinkFixAction{}
	if result.FixPlan != nil {
		for _, action := range result.FixPlan.Actions {
			key := nodeLocatorMapKey(normalizeLinkNodeRef(action.Ref))
			fixes[key] = append(fixes[key], action)
		}
	}

	locators := make(map[string]NodeLocator, len(req.Refs))
	for _, inputRef := range req.Refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ref := normalizeLinkNodeRef(inputRef)
		if ref.IsZero() {
			continue
		}
		key := nodeLocatorMapKey(ref)
		target, hasTarget := result.Targets[key]
		locatorDiagnostics := diagnostics[key]
		locatorFixes := fixes[key]
		if hasTarget {
			canonicalKey := nodeLocatorMapKey(normalizeLinkNodeRef(target.Ref))
			if canonicalKey != key {
				locatorDiagnostics = append(append([]NodeLinkDiagnostic(nil), locatorDiagnostics...), diagnostics[canonicalKey]...)
				locatorFixes = append(append([]NodeLinkFixAction(nil), locatorFixes...), fixes[canonicalKey]...)
			}
		}
		status := NodeLocatorUnresolved
		var targetPtr *NodeLinkTarget
		if hasTarget {
			targetCopy := target
			targetPtr = &targetCopy
			if target.RequiresFix {
				status = NodeLocatorRequiresFix
			} else if target.Exists {
				status = NodeLocatorLinkable
			}
		} else {
			status = locatorStatusFromDiagnostics(locatorDiagnostics)
		}
		fixActions := nodeLocatorFixActions(ref, locatorFixes, result.FixPlan)
		if len(fixActions) == 0 && targetPtr != nil && targetPtr.RequiresFix && strings.TrimSpace(targetPtr.BlockID) != "" {
			fixActions = []NodeLinkFixAction{{Ref: targetPtr.Ref, BlockID: targetPtr.BlockID}}
		}
		canonical := ref
		if targetPtr != nil && !targetPtr.Ref.IsZero() {
			canonical = targetPtr.Ref
		}
		locators[key] = NodeLocator{
			Ref:           canonical,
			Kind:          nodeLocatorKind(canonical),
			SourceLocator: nodeSourceLocator(canonical),
			LinkTarget:    targetPtr,
			Status:        status,
			Diagnostics:   locatorDiagnostics,
			FixActions:    fixActions,
		}
	}
	return locators, nil
}

// LinkTargets renders durable author-facing links for ontology nodes. It never
// mutates source unless EnsureLinkTargetApply is requested.
// Docs: [[ontology-browser-workspace#^spec-0014-us5-ac4]] keeps workspace,
// coderef, and agent copy-link flows on this one target contract.
func (s *NodeLinkService) LinkTargets(ctx context.Context, req LinkTargetRequest) (LinkTargetResult, error) {
	if err := ctx.Err(); err != nil {
		return LinkTargetResult{}, err
	}
	if s == nil {
		return LinkTargetResult{}, fmt.Errorf("node link service is nil")
	}
	if s.NoteReader == nil {
		return LinkTargetResult{}, fmt.Errorf("note reader is required")
	}
	mode := req.Ensure
	if mode == "" {
		mode = EnsureLinkTargetNever
	}
	allNotes := s.notePaths
	if allNotes == nil {
		var err error
		allNotes, err = s.NoteReader.GetNotesList(s.VaultDef)
		if canceled := ctx.Err(); canceled != nil {
			return LinkTargetResult{}, canceled
		}
		if err != nil {
			return LinkTargetResult{}, err
		}
	}
	var renderer wikilinkRenderer
	if s.renderer != nil {
		renderer = *s.renderer
	} else {
		renderer = newWikilinkRenderer(allNotes)
	}
	result := LinkTargetResult{Targets: map[string]NodeLinkTarget{}}
	snapshots := map[string]*DocumentSnapshot{}
	fixPlan := &NodeLinkFixPlan{}

	for _, inputRef := range req.Refs {
		if err := ctx.Err(); err != nil {
			return LinkTargetResult{}, err
		}
		ref := normalizeLinkNodeRef(inputRef)
		if ref.IsZero() {
			continue
		}
		key := nodeLocatorMapKey(ref)
		if ref.Kind == "" || ref.Kind == NodeKindNote {
			target := noteLinkTarget(ref, renderer)
			result.Targets[key] = target
			continue
		}
		if ref.Kind != NodeKindEmbedded {
			result.Diagnostics = append(result.Diagnostics, NodeLinkDiagnostic{
				Code:     "unsupported_node_kind",
				NotePath: ref.NotePath,
				Ref:      ref,
				Message:  "only embedded ontology nodes are linkable with block identifiers",
			})
			continue
		}
		snapshot := snapshots[ref.NotePath]
		if snapshot == nil {
			load := s.LoadSnapshot
			if load == nil {
				load = func(ctx context.Context, path string) (*DocumentSnapshot, error) {
					return LoadDocumentSnapshot(ctx, s.VaultDef, s.NoteReader, path)
				}
			}
			loaded, err := load(ctx, ref.NotePath)
			if canceled := ctx.Err(); canceled != nil {
				return result, canceled
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return result, err
			}
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, NodeLinkDiagnostic{
					Code:     "snapshot_load_failed",
					NotePath: ref.NotePath,
					Ref:      ref,
					Message:  err.Error(),
				})
				continue
			}
			snapshot = loaded
			snapshots[ref.NotePath] = snapshot
		}
		projection, err := ProjectNodeFromSnapshot(snapshot, s.Schema, ref)
		if err != nil || projection == nil {
			msg := "embedded node could not be resolved"
			if err != nil {
				msg = err.Error()
			}
			result.Diagnostics = append(result.Diagnostics, NodeLinkDiagnostic{
				Code:     "node_unresolved",
				NotePath: ref.NotePath,
				Ref:      ref,
				Message:  msg,
			})
			continue
		}
		target, action, diags := embeddedLinkTarget(snapshot, projection, renderer)
		result.Targets[key] = target
		result.Diagnostics = append(result.Diagnostics, diags...)
		if action != nil && mode != EnsureLinkTargetNever {
			fixPlan.Actions = append(fixPlan.Actions, *action)
		}
	}

	if len(fixPlan.Actions) > 0 {
		result.FixPlan = fixPlan
	}
	if mode == EnsureLinkTargetApply && len(fixPlan.Actions) > 0 {
		session := NewEditSession(s.VaultDef, s.NoteReader, s.Schema)
		for _, action := range fixPlan.Actions {
			if err := session.EnsureBlockID(action.Ref, action.BlockID); err != nil {
				return result, err
			}
		}
		commit, err := session.CommitWithOptions(ctx, CommitOptions{WriteLease: s.WriteLease})
		if err != nil {
			return result, err
		}
		if !commit.Applied || len(commit.Conflicts) > 0 {
			result.Diagnostics = append(result.Diagnostics, NodeLinkDiagnostic{
				Code:    "apply_failed",
				Message: "block ID fix plan could not be applied",
			})
			return result, nil
		}
		refresh := req
		refresh.Ensure = EnsureLinkTargetNever
		refreshService := *s
		refreshService.notePaths = allNotes
		refreshService.renderer = &renderer
		refreshService.LoadSnapshot = nil
		refreshed, refreshErr := refreshService.LinkTargets(ctx, refresh)
		refreshed.Applied = true
		return refreshed, refreshErr
	}

	return result, nil
}
