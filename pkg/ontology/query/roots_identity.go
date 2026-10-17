package query

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"io/fs"
	"sort"
	"strings"
)

func (e *executor) rootNote(ctx context.Context, rawPath string) (*noteRecord, error) {
	notePath, err := normalizeNotePath(e.loaders.vaultPaths, rawPath)
	if err != nil {
		return nil, err
	}
	row, ok, err := e.resolvedType(ctx, notePath)
	if err != nil {
		return nil, err
	}
	record, err := e.loaders.noteByPath(ctx, notePath)
	if err != nil || record == nil {
		return nil, err
	}
	if ok {
		record.TypeName = row.TypeName
	}
	return record, nil
}

func (e *executor) rootNoteFromArgs(ctx context.Context, args map[string]any) (*noteRecord, error) {
	if args == nil {
		return nil, fmt.Errorf("note(...) requires path or ref")
	}
	if rawPath := strings.TrimSpace(stringValue(args["path"])); rawPath != "" {
		if ref, ok, err := e.resolveNodeRefTarget(ctx, rawPath); err != nil {
			return nil, err
		} else if ok {
			if ref.Kind != "" && ref.Kind != ontology.NodeKindNote {
				return nil, fmt.Errorf("note(path:) resolved to %s, expected NOTE", ref.Kind)
			}
			return e.rootNote(ctx, ref.NotePath)
		}
		return e.rootNote(ctx, rawPath)
	}
	rawRef := strings.TrimSpace(stringValue(args["ref"]))
	if rawRef == "" {
		return nil, fmt.Errorf("note(...) requires path or ref")
	}
	ref, candidates, err := e.resolveNodeRefString(ctx, rawRef)
	if err != nil {
		return nil, err
	}
	if len(candidates) > 1 {
		return nil, nil
	}
	if ref.NotePath == "" {
		return nil, fmt.Errorf("note ref requires a note path")
	}
	if ref.Kind != "" && ref.Kind != ontology.NodeKindNote {
		return nil, fmt.Errorf("note(ref:) requires a NOTE ref")
	}
	return e.rootNote(ctx, ref.NotePath)
}

func (e *executor) rootNode(ctx context.Context, input string) (*resolvedNode, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("node ref is required")
	}
	ref, candidates, err := e.resolveNodeRefString(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(candidates) > 1 {
		return nil, nil
	}
	if ref.NotePath == "" {
		return nil, fmt.Errorf("node ref requires a path or ref")
	}
	if isCodeNodeKind(ref.Kind) && e.deps.PathOwner != nil && e.deps.PathOwner(ref.NotePath) != PathOwnerCode {
		return nil, nil
	}
	node, err := e.rootNodeFromRef(ctx, ref)
	if err == nil && node != nil && node.Code == nil && e.noteSourceDeleted(ref.NotePath) {
		return nil, nil
	}
	if err != nil || node != nil || !allowsCodeFileFallback(ref) {
		return node, err
	}
	if !e.allowsCodeFallback(ref.NotePath) {
		return nil, nil
	}
	codeRef := ref
	codeRef.Kind = "CODE_FILE"
	code, err := e.rootNodeFromRef(ctx, codeRef)
	if err != nil || code == nil {
		return code, err
	}
	if e.deps.PathOwner == nil && (code.Code == nil || len(code.Code.Symbols) == 0) {
		return nil, nil
	}
	// A fragment request must resolve to an indexed code symbol. A filesystem
	// path alone is not evidence that a missing note heading is code.
	if strings.TrimSpace(ref.Fragment) != "" && (code.Code == nil || code.Code.Anchor == nil) {
		return nil, nil
	}
	return code, nil
}

// noteSourceDeleted reports whether a singular root's note file is gone while
// its indexed rows remain because the watcher has not published the deletion.
// Only exact roots check: broad selectors stay on indexed state alone.
func (e *executor) noteSourceDeleted(notePath string) bool {
	if e.deps.NoteReader == nil || e.deps.ReadOverlay.TouchesPath(notePath) {
		return false
	}
	_, err := e.deps.NoteReader.GetModTime(e.deps.VaultDef, notePath)
	return err != nil && (errors.Is(err, fs.ErrNotExist) || err.Error() == obsidian.NoteDoesNotExistError)
}

func (e *executor) allowsCodeFallback(path string) bool {
	if e.deps.PathOwner != nil {
		return e.deps.PathOwner(path) == PathOwnerCode
	}
	// Callers that do not compose configured ownership can only fall back to
	// the indexed symbols checked by rootNode. They must not classify an
	// arbitrary filesystem path as code.
	return true
}

func (e *executor) resolveNodeRefString(ctx context.Context, input string) (ontology.NodeRef, []ontology.NodeRef, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return ontology.NodeRef{}, nil, nil
	}
	if ref, ok := explicitNodePathRefFromString(input); ok {
		return ref, nil, nil
	}
	if ref, candidates, ok, err := e.resolveNodeRefTargetCandidates(ctx, input); err != nil || ok || len(candidates) > 1 {
		return ref, candidates, err
	}
	return nodeRefFromString(input), nil, nil
}

func (e *executor) rootNodeFromRef(ctx context.Context, ref ontology.NodeRef) (*resolvedNode, error) {
	if ref.Structural != "" && ref.Fragment == "" && ref.NodeID == "" {
		note, err := e.rootNote(ctx, ref.NotePath)
		if err != nil || note == nil {
			return nil, err
		}
		projection, err := e.projectNoteRecord(ctx, note)
		if err != nil {
			return nil, err
		}
		// A structural locator can identify the document root as well as a section.
		// Only the current projected fingerprint authorizes resolving it as the note.
		if projection != nil && projection.Ref.Structural == ref.Structural {
			return &resolvedNode{Note: note}, nil
		}
	}
	switch ref.Kind {
	case "", ontology.NodeKindNote:
		if ref.Fragment != "" || ref.NodeID != "" || ref.Structural != "" {
			section, err := e.rootSection(ctx, ref)
			if err != nil || section != nil {
				return &resolvedNode{Section: section}, err
			}
		}
		record, err := e.rootNote(ctx, ref.NotePath)
		if err != nil || record == nil {
			return nil, err
		}
		return &resolvedNode{Note: record}, nil
	case ontology.NodeKindSection, ontology.NodeKindEmbedded:
		section, err := e.rootSection(ctx, ref)
		if err != nil || section == nil {
			return nil, err
		}
		return &resolvedNode{Section: section}, nil
	default:
		code, err := e.rootCode(ctx, ref)
		if err != nil || code == nil {
			return nil, err
		}
		return &resolvedNode{Code: code}, nil
	}
}

func sortedUniqueNodeRefs(refs []ontology.NodeRef) []ontology.NodeRef {
	refs = append([]ontology.NodeRef(nil), refs...)
	sort.Slice(refs, func(i, j int) bool {
		left := refs[i].NotePath + "\x00" + refs[i].Fragment + "\x00" + refs[i].Structural + "\x00" + refs[i].NodeID + "\x00" + refs[i].TypeName + "\x00" + string(refs[i].Kind)
		right := refs[j].NotePath + "\x00" + refs[j].Fragment + "\x00" + refs[j].Structural + "\x00" + refs[j].NodeID + "\x00" + refs[j].TypeName + "\x00" + string(refs[j].Kind)
		return left < right
	})
	unique := refs[:0]
	for _, ref := range refs {
		if len(unique) == 0 || ref != unique[len(unique)-1] {
			unique = append(unique, ref)
		}
	}
	return unique
}
