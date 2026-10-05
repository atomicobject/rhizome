package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/vektah/gqlparser/v2/ast"
)

func Execute(ctx context.Context, deps Deps, schema *ontology.Schema, prepared *PreparedQuery) Result {
	if schema == nil {
		return Result{Errors: []Error{{Message: "ontology schema is required"}}}
	}
	// Docs: [[ontology-graphql-query-contract#^spec-0042-batching]] and
	// [[noderef-batch-traversal-read-api#^spec-0022-us1-ac1]] keep broad
	// query execution batched and read-only over indexed state plus bounded
	// projection.
	if prepared == nil || prepared.Operation == nil {
		return Result{Errors: []Error{{Message: "prepared query is required"}}}
	}
	if deps.Store == nil {
		return Result{Errors: []Error{{Message: "ontology store is required"}}}
	}
	if deps.NoteReader == nil {
		return Result{Errors: []Error{{Message: "note reader is required"}}}
	}

	loaders, err := newLoaders(deps, schema)
	if err != nil {
		return Result{Errors: []Error{{Message: err.Error()}}}
	}
	exec := executor{
		schema:             schema,
		deps:               deps,
		loaders:            loaders,
		variables:          prepared.Variables,
		fragments:          prepared.Document.Fragments,
		assessmentByPath:   make(map[string]*ontology.NoteAssessment),
		assessmentKnown:    make(map[string]bool),
		sectionsByPath:     make(map[string][]*ontology.SectionNode),
		relationCounts:     make(map[string]map[string]int),
		typedRootCoverage:  make(map[string]typedRootCoverage),
		linkTargetResolver: pushdown.NewCatalogLinkResolver(schema, deps.Store),
		recordFactPaths:    make(map[string]struct{}),
		recordFactScopes:   make(map[semdb.ValidationScope]struct{}),
	}
	data := exec.resolveQuerySelectionSet(ctx, prepared.Operation.SelectionSet, nil)
	exec.fillRecordFacts(ctx, data)
	result := Result{
		Data:        data,
		Errors:      exec.errors,
		orderedData: orderResponseValue(data, prepared.Operation.SelectionSet, prepared.Document.Fragments),
	}
	if len(exec.typedRootCoverage) > 0 {
		result.Extensions = map[string]any{"typedRoots": exec.typedRootCoverage}
	}
	return result
}

// ExecutePrepared dispatches ordinary GraphQL reads and schema introspection
// through the same prepared-query contract so every caller observes the same
// public GraphQL behavior.
func ExecutePrepared(ctx context.Context, deps Deps, schema *ontology.Schema, execSchema *ExecutableSchema, prepared *PreparedQuery) Result {
	if prepared != nil && prepared.Introspection {
		return ExecuteIntrospection(execSchema, prepared)
	}
	return Execute(ctx, deps, schema, prepared)
}

type executor struct {
	schema    *ontology.Schema
	deps      Deps
	loaders   *loaders
	variables map[string]any
	fragments ast.FragmentDefinitionList
	errors    []Error

	assessmentMu     sync.Mutex
	assessmentByPath map[string]*ontology.NoteAssessment
	assessmentKnown  map[string]bool

	sectionsMu     sync.Mutex
	sectionsByPath map[string][]*ontology.SectionNode

	notePathCacheOnce sync.Once
	notePathCache     *obsidian.NotePathCache
	notePathCacheErr  error

	inboundSectionNeighborsOnce sync.Once
	inboundSectionNeighbors     map[string][]string
	inboundSectionNeighborsErr  error

	relationCounts     map[string]map[string]int
	reverseRelations   map[string]map[string][]ontology.NodeRef
	typedRootCoverage  map[string]typedRootCoverage
	rootCandidateBound bool
	linkTargetResolver *pushdown.CatalogLinkResolver

	// Record facts resolve after the selection tree (fillRecordFacts).
	recordFactPaths  map[string]struct{}
	recordFactScopes map[semdb.ValidationScope]struct{}
}

// Typed roots keep their historic array shape. Coverage is keyed by the
// response alias so independently bounded roots never share page state.
type typedRootCoverage struct {
	RequestedFirst int    `json:"requestedFirst"`
	ReturnedCount  int    `json:"returnedCount"`
	Offset         int    `json:"offset"`
	HasMore        *bool  `json:"hasMore"`
	NextOffset     *int   `json:"nextOffset"`
	Status         string `json:"status"`
}

type resolvedNode struct {
	Note    *noteRecord
	Section *sectionRecord
	Code    *codeRecord
}

type scoredMatch struct {
	path  string
	score float64
}

func (m scoredMatch) getPath() string { return m.path }

func (e *executor) resolveQuerySelectionSet(ctx context.Context, set ast.SelectionSet, path []string) map[string]any {
	return e.resolveSelections(set, path, "unsupported root selection", func(condition string) bool {
		return condition == "" || condition == "Query"
	}, func(field *ast.Field, fieldPath []string) (any, bool) {
		return e.resolveRootField(ctx, field, fieldPath)
	})
}

func (e *executor) fieldArgs(field *ast.Field) map[string]any {
	if field == nil {
		return nil
	}
	return field.ArgumentMap(e.variables)
}

func fieldHasArg(field *ast.Field, name string) bool {
	if field == nil {
		return false
	}
	for _, arg := range field.Arguments {
		if arg != nil && arg.Name == name {
			return true
		}
	}
	return false
}

func rootFirst(args map[string]any, semantic bool) int {
	return rootFirstMax(args, semantic, publicRootFirstMax)
}

func rootFirstMax(args map[string]any, semantic bool, maxFirst int) int {
	defaultFirst := 20
	if semantic {
		if present, _ := args["__firstPresent"].(bool); !present {
			defaultFirst = 100
		}
	}
	first := intValue(args["first"], defaultFirst)
	if first <= 0 {
		first = defaultFirst
	}
	if maxFirst > 0 && first > maxFirst {
		first = maxFirst
	}
	return first
}

func (e *executor) resolveRootField(ctx context.Context, field *ast.Field, path []string) (any, bool) {
	name := strings.TrimSpace(field.Name)
	if field.Definition != nil && strings.TrimSpace(field.Definition.Name) != "" {
		name = strings.TrimSpace(field.Definition.Name)
	}
	switch name {
	case "node":
		args := e.fieldArgs(field)
		resolved, err := e.rootNode(ctx, stringValue(args["ref"]))
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		if resolved == nil {
			return nil, true
		}
		return e.resolveAnyNodeSelectionSet(ctx, resolved, field.SelectionSet, path), true
	case "nodes":
		return e.resolveNodeBatchSelectionSet(ctx, field, path), true
	case "note":
		args := e.fieldArgs(field)
		record, err := e.rootNoteFromArgs(ctx, args)
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		if record == nil || e.noteSourceDeleted(record.Path) {
			return nil, true
		}
		return e.resolveNodeSelectionSet(ctx, record, field.SelectionSet, path), true
	case "notes":
		args := e.fieldArgs(field)
		args["__firstPresent"] = fieldHasArg(field, "first")
		requestedFirst := rootFirst(args, len(semanticArgList(args["semantic"])) > 0)
		records, err := e.rootNotesWithMax(ctx, argsWithFirst(args, requestedFirst+1), path, requestedFirst+1)
		if err != nil {
			e.addError(path, err.Error())
			records = nil
		}
		records, truncated := trimRecordsForPage(records, requestedFirst)
		return e.resolveNoteConnectionSelectionSet(ctx, records, nil, requestedFirst, truncated, field.SelectionSet, path), true
	case "search":
		return e.resolveSearchSelectionSet(ctx, field, path), true
	case "validation":
		return e.resolveValidationSelectionSet(ctx, field, path), true
	case "resolve":
		args := e.fieldArgs(field)
		return e.resolveRefSelectionSet(ctx, stringValue(args["ref"]), field.SelectionSet, path), true
	default:
		if _, ok := e.depsRuntimeRoot(name); ok {
			return e.resolveRuntimeRoot(ctx, field, path)
		}
		if typeName := queryRootTypeName(e.schema, name); typeName != "" {
			e.rootCandidateBound = false
			args := e.fieldArgs(field)
			args["__firstPresent"] = fieldHasArg(field, "first")
			first := rootFirst(args, len(semanticArgList(args["semantic"])) > 0)
			offset := intValue(args["offset"], 0)
			fetchFirst := first + 1
			if fetchFirst > publicRootFirstMax {
				fetchFirst = publicRootFirstMax
			}
			selector := stringValue(args["find"]) != "" || args["property"] != nil || len(semanticArgList(args["semantic"])) > 0
			if selector {
				if offset >= publicRootFirstMax {
					e.addError(path, fmt.Sprintf("%s(...) offset must be < %d for find, property, or semantic selection; narrow the query to continue", name, publicRootFirstMax))
					return nil, false
				}
				fetchFirst = min(fetchFirst, publicRootFirstMax-offset)
				if args["filters"] != nil || args["sort"] != nil {
					// Filters and sort must see the full bounded selector window
					// before pagination; otherwise a later match can disappear.
					args["first"] = publicRootFirstMax
				} else {
					args["first"] = offset + fetchFirst
				}
				if intValue(args["first"], 0) == publicRootFirstMax {
					args["__selectorCandidateLimit"] = true
				}
				args["offset"] = 0 // selector results are sliced after their ranked candidate read
			} else {
				args["first"] = fetchFirst
			}
			if noteType := e.schema.Types[typeName]; noteType != nil && noteType.Role == ontology.TypeRoleEmbeddedNode {
				records, err := e.rootEmbeddedNodes(ctx, typeName, args, field.SelectionSet)
				if err != nil {
					e.addError(path, err.Error())
					return nil, false
				}
				records, hasMore := pageTypedRootRecords(records, first, offset, false)
				e.typedRootCoverage[path[len(path)-1]] = newTypedRootCoverage(first, offset, len(records), hasMore, fetchFirst <= first, e.rootCandidateBound)
				return e.resolveSectionList(ctx, records, field.SelectionSet, path), true
			}
			records, err := e.rootTypedNotes(ctx, typeName, args)
			if err != nil {
				e.addError(path, err.Error())
				return nil, false
			}
			records, hasMore := pageTypedRootRecords(records, first, offset, selector)
			e.typedRootCoverage[path[len(path)-1]] = newTypedRootCoverage(first, offset, len(records), hasMore, fetchFirst <= first, e.rootCandidateBound)
			return e.resolveNodeList(ctx, records, field.SelectionSet, path), true
		}
		e.addError(path, fmt.Sprintf("unsupported root field %q", name))
		return nil, false
	}
}

func (e *executor) depsRuntimeRoot(name string) (RuntimeRootKind, bool) {
	roots := builtinRuntimeRoots()
	kind, ok := roots[name]
	return kind, ok
}

type nodeBatchItem struct {
	Input string
	Ref   *ontology.NodeRef
	Node  *resolvedNode
	Error *RuntimeWarning
}

func (e *executor) resolveNodeBatchSelectionSet(ctx context.Context, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	first := boundedInt(args["first"], 50, 1, nestedFieldFirstMax)
	refs := stringListValue(args["refs"])
	if len(refs) > first {
		refs = refs[:first]
	}
	items := make([]nodeBatchItem, 0, len(refs))
	for _, ref := range refs {
		label := strings.TrimSpace(ref)
		node, err := e.rootNode(ctx, ref)
		item := nodeBatchItem{Input: label}
		if err != nil {
			item.Error = &RuntimeWarning{Code: "unresolved_ref", Message: err.Error()}
			items = append(items, item)
			continue
		}
		if node == nil {
			item.Error = &RuntimeWarning{Code: "unresolved_ref", Message: "node ref did not resolve"}
			items = append(items, item)
			continue
		}
		ref := e.resolvedNodeRef(node)
		item.Ref = &ref
		item.Node = node
		items = append(items, item)
	}
	out := make(map[string]any)
	for _, selection := range field.SelectionSet {
		current, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(current)
		switch current.Name {
		case "items":
			rows := make([]any, 0, len(items))
			for _, item := range items {
				rows = append(rows, e.resolveNodeBatchItemSelectionSet(ctx, item, current.SelectionSet, append(path, key)))
			}
			out[key] = rows
		case "pageInfo":
			out[key] = e.resolvePageInfoSelectionSet(pageInfo{QueryShapeVersion: "v1", RequestedFirst: first, ReturnedCount: len(items), Truncated: len(stringListValue(args["refs"])) > len(items), MaxFirst: nestedFieldFirstMax}, current.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeBatchResult", current.Name))
		}
	}
	return out
}

func (e *executor) resolveNodeBatchItemSelectionSet(ctx context.Context, item nodeBatchItem, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "requestedRef":
			out[key] = item.Input
		case "ref":
			if item.Ref == nil {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(*item.Ref)
			}
		case "node":
			if item.Node == nil {
				out[key] = nil
			} else {
				out[key] = e.resolveAnyNodeSelectionSet(ctx, item.Node, field.SelectionSet, append(path, key))
			}
		case "error":
			if item.Error == nil {
				out[key] = nil
			} else {
				rows := e.resolveWarnings([]RuntimeWarning{*item.Error}, field.SelectionSet, append(path, key))
				if len(rows) > 0 {
					out[key] = rows[0]
				} else {
					out[key] = map[string]any{}
				}
			}
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeBatchItem", field.Name))
		}
	}
	return out
}

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

func nodeRefCandidatesRepresent(candidates []ontology.NodeRef, ref ontology.NodeRef) bool {
	for _, candidate := range candidates {
		if candidate == ref {
			return true
		}
		if candidate.Kind == ontology.NodeKindNote && ref.Kind == ontology.NodeKindNote &&
			candidate.NotePath == ref.NotePath && candidate.TypeName == ref.TypeName {
			return true
		}
	}
	return false
}

func (e *executor) resolvePreferredIdentifierTarget(ctx context.Context, target string) (ontology.NodeRef, []ontology.NodeRef, bool, error) {
	target = strings.TrimSpace(target)
	if target == "" || e.schema == nil {
		return ontology.NodeRef{}, nil, false, nil
	}
	refs := make([]ontology.NodeRef, 0, 2)
	typeNames := make([]string, 0, len(e.schema.Types))
	for typeName := range e.schema.Types {
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)
	for _, typeName := range typeNames {
		noteType := e.schema.Types[typeName]
		if noteType == nil || noteType.Role != ontology.TypeRoleNote {
			continue
		}
		field := preferredIdentifierField(noteType)
		if field == nil {
			continue
		}
		matches, err := e.resolveNoteIdentifierRefs(ctx, noteType, field, target)
		if err != nil {
			return ontology.NodeRef{}, nil, false, err
		}
		refs = append(refs, matches...)
	}
	for _, typeName := range typeNames {
		noteType := e.schema.Types[typeName]
		if noteType == nil || noteType.Role != ontology.TypeRoleEmbeddedNode || preferredIdentifierField(noteType) == nil {
			continue
		}
		matches, err := e.resolveSectionIdentifierRefs(ctx, noteType, target)
		if err != nil {
			return ontology.NodeRef{}, nil, false, err
		}
		refs = append(refs, matches...)
	}
	refs = sortedUniqueNodeRefs(refs)
	if len(refs) == 1 {
		return refs[0], nil, true, nil
	}
	if len(refs) > 1 {
		return ontology.NodeRef{}, refs, false, nil
	}
	return ontology.NodeRef{}, nil, false, nil
}

func (e *executor) resolveNoteIdentifierRefs(ctx context.Context, noteType *ontology.NoteType, field *ontology.Field, target string) ([]ontology.NodeRef, error) {
	if noteType == nil || field == nil {
		return nil, nil
	}
	candidates, err := e.identifierCandidatePaths(ctx, noteType, field, target, semdb.NotePropertySourceFrontmatter)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		candidates, err = e.deps.Store.OntologyPathsByType(ctx, noteType.Name, 100000)
		if err != nil {
			return nil, err
		}
		sort.Strings(candidates)
	}
	refs := make([]ontology.NodeRef, 0, len(candidates))
	for _, path := range candidates {
		note, err := e.rootNote(ctx, path)
		if err != nil || note == nil {
			return nil, err
		}
		if note.TypeName != noteType.Name || !identifierValuesContain(note.fieldValues(field), target) {
			continue
		}
		refs = append(refs, ontology.NodeRef{NotePath: note.Path, Kind: ontology.NodeKindNote, TypeName: note.TypeName})
	}
	return refs, nil
}

func (e *executor) resolveSectionIdentifierRefs(ctx context.Context, sectionType *ontology.NoteType, target string) ([]ontology.NodeRef, error) {
	if sectionType == nil {
		return nil, nil
	}
	candidatePaths, err := e.allTypedNotePaths(ctx)
	if err != nil {
		return nil, err
	}
	refs := make([]ontology.NodeRef, 0, 2)
	for _, path := range candidatePaths {
		note, err := e.rootNote(ctx, path)
		if err != nil || note == nil {
			return nil, err
		}
		sections, err := e.sectionRecordsForNote(ctx, note)
		if err != nil {
			return nil, err
		}
		for _, section := range sections {
			if section == nil || section.TypeName != sectionType.Name {
				continue
			}
			field := preferredIdentifierField(sectionType)
			if field == nil || !identifierValuesContain(section.fieldValues(field, section.PropertyCase), target) {
				continue
			}
			refs = append(refs, e.sectionNodeRef(section))
		}
	}
	return refs, nil
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

func (e *executor) identifierCandidatePaths(ctx context.Context, noteType *ontology.NoteType, field *ontology.Field, target string, source semdb.NotePropertySource) ([]string, error) {
	seen := map[string]struct{}{}
	var pathsList []string
	for _, name := range identifierFieldSourceNames(field, noteType.PropertyCase) {
		matches, err := e.deps.Store.CurrentNotePathsByPropertyValue(ctx, strings.ToLower(name), normalizePropertyValue(target), source)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			pathsList = append(pathsList, path)
		}
	}
	sort.Strings(pathsList)
	return pathsList, nil
}

func (e *executor) allTypedNotePaths(ctx context.Context) ([]string, error) {
	seen := map[string]struct{}{}
	var pathsList []string
	typeNames := make([]string, 0, len(e.schema.Types))
	for typeName, noteType := range e.schema.Types {
		if noteType != nil && noteType.Role == ontology.TypeRoleNote {
			typeNames = append(typeNames, typeName)
		}
	}
	sort.Strings(typeNames)
	for _, typeName := range typeNames {
		matches, err := e.deps.Store.OntologyPathsByType(ctx, typeName, 100000)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			pathsList = append(pathsList, path)
		}
	}
	sort.Strings(pathsList)
	return pathsList, nil
}

func preferredIdentifierField(noteType *ontology.NoteType) *ontology.Field {
	if noteType == nil {
		return nil
	}
	for _, field := range noteType.Fields {
		if field != nil && field.IsPreferredIdentifier {
			return field
		}
	}
	return nil
}

func identifierFieldSourceNames(field *ontology.Field, propertyCase ontology.PropertyCase) []string {
	names := ontology.FieldSourceNames(field)
	if len(names) == 0 && field != nil {
		names = append(names, ontology.DefaultPropertyName(field.Name, propertyCase))
	}
	return names
}

func identifierValuesContain(values []string, target string) bool {
	target = normalizeIdentifierValue(target)
	for _, value := range values {
		if normalizeIdentifierValue(value) == target {
			return true
		}
	}
	return false
}

func normalizeIdentifierValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "^")
	return normalizePropertyValue(value)
}

func (e *executor) rootSection(ctx context.Context, ref ontology.NodeRef) (*sectionRecord, error) {
	trace := os.Getenv("RZM_TRACE_GRAPHQL_NODE") != ""
	start := time.Now()
	last := start
	traceStep := func(label string) {
		if !trace {
			return
		}
		now := time.Now()
		fmt.Fprintf(os.Stderr, "graphql rootSection trace %s step=%s delta=%s total=%s\n", ref.String(), label, now.Sub(last), now.Sub(start))
		last = now
	}
	note, err := e.rootNote(ctx, ref.NotePath)
	traceStep("root-note")
	if err != nil || note == nil {
		return nil, err
	}
	if strings.TrimSpace(ref.Structural) != "" && strings.TrimSpace(ref.Fragment) == "" && strings.TrimSpace(ref.NodeID) == "" {
		noteProjection, err := e.projectNoteRecord(ctx, note)
		traceStep("project-note")
		if err != nil {
			return nil, err
		}
		if noteProjection == nil {
			return nil, nil
		}
		projection, err := ontology.ProjectNodeFromSnapshot(noteProjection.Snapshot, e.schema, ref)
		traceStep("project-structural")
		if err != nil {
			return nil, err
		}
		if projection == nil {
			return nil, nil
		}
		record := e.sectionRecordFromProjection(note, projection)
		if record != nil && record.Node != nil {
			return record, nil
		}
		return nil, nil
	}
	sections, err := e.headingSectionRecordsForNote(ctx, note)
	traceStep("heading-sections")
	if err != nil {
		return nil, err
	}
	if section := e.matchRootSection(ref, sections); section != nil {
		traceStep("heading-match")
		return section, nil
	}
	sections, err = e.itemProjectionRecordsForNote(ctx, note)
	traceStep("item-sections")
	if err != nil {
		return nil, err
	}
	section := e.matchRootSection(ref, sections)
	traceStep("all-match")
	return section, nil
}

func (e *executor) matchRootSection(ref ontology.NodeRef, sections []*sectionRecord) *sectionRecord {
	targetFragment := strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	for _, section := range sections {
		if section == nil || section.Node == nil {
			continue
		}
		node := section.Node
		if ref.NodeID != "" && node.ID != ref.NodeID {
			continue
		}
		if targetFragment != "" && !matchesRootSectionFragment(ref.NotePath, targetFragment, node) {
			continue
		}
		kind := ontology.NodeKindSection
		if noteType := e.schema.Types[section.TypeName]; noteType != nil && noteType.Role == ontology.TypeRoleEmbeddedNode {
			kind = ontology.NodeKindEmbedded
		}
		if ref.Kind != "" && ref.Kind != kind {
			continue
		}
		return section
	}
	return nil
}

func matchesRootSectionFragment(notePath, targetFragment string, node *ontology.SectionNode) bool {
	if node == nil {
		return false
	}
	if strings.HasPrefix(targetFragment, "^") {
		return strings.TrimPrefix(targetFragment, "^") == strings.TrimPrefix(strings.TrimSpace(node.BlockID), "^") && strings.TrimSpace(node.BlockID) != ""
	}
	return targetFragment == queryFragmentFromSectionID(notePath, node.ID)
}

func (e *executor) rootCode(ctx context.Context, ref ontology.NodeRef) (*codeRecord, error) {
	codePath, _, err := paths.ResolveCodeInputWithVaultPaths(e.loaders.vaultPaths, ref.NotePath)
	if err != nil {
		return nil, err
	}
	abs, err := e.loaders.vaultPaths.AbsCode(codePath)
	if err != nil {
		return nil, err
	}
	exists := false
	if info, err := os.Stat(abs.String()); err == nil {
		exists = !info.IsDir()
	} else {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	anchors, err := e.deps.Store.IntelAnchorsByPath(ctx, codePath.String())
	if err != nil {
		return nil, err
	}
	if !exists && len(anchors) == 0 {
		return nil, nil
	}
	record := &codeRecord{Path: codePath.String(), Title: filepath.Base(codePath.String()), Symbols: anchors}
	if len(anchors) > 0 {
		record.Lang = string(anchors[0].Lang)
	}
	if ref.NodeID != "" || ref.Fragment != "" {
		wanted := firstNonEmpty(strings.TrimPrefix(ref.Fragment, "symbol:"), ref.NodeID)
		for i := range anchors {
			anchor := anchors[i]
			if anchor.AnchorID == wanted || anchor.FQN == wanted || anchor.Symbol == wanted {
				record.Anchor = &anchor
				record.Title = firstNonEmpty(anchor.Symbol, anchor.FQN, anchor.AnchorID)
				record.Lang = string(anchor.Lang)
				break
			}
		}
		if ref.Kind == "CODE_SYMBOL" && record.Anchor == nil {
			return nil, nil
		}
	}
	return record, nil
}

func (e *executor) rootNotesWithMax(ctx context.Context, args map[string]any, path []string, maxFirst int) ([]*noteRecord, error) {
	typeFilter := strings.TrimSpace(stringValue(args["type"]))
	findArg := strings.TrimSpace(stringValue(args["find"]))
	semanticArgs := semanticArgList(args["semantic"])
	first := rootFirstMax(args, len(semanticArgs) > 0, maxFirst)

	switch {
	case findArg != "":
		return e.findTypedNotes(ctx, findArg, typeFilter, first)
	case args["property"] != nil:
		propertyArgs, _ := args["property"].(map[string]any)
		return e.propertyTypedNotes(ctx, propertyArgs, typeFilter, first)
	case len(semanticArgs) > 0:
		if typeFilter != "" {
			return e.semanticTypedNotes(ctx, semanticArgs, typeFilter, first)
		}
		return e.semanticNotes(ctx, semanticArgs, first)
	case typeFilter != "":
		if _, ok := e.schema.Interfaces[typeFilter]; ok {
			rows, err := e.loaders.selectorMetadataRows(ctx)
			if err != nil {
				return nil, err
			}
			pathsList := make([]string, 0, len(rows))
			for _, row := range rows {
				pathsList = append(pathsList, row.Path)
			}
			sort.Strings(pathsList)
			return e.filterTypedRecords(ctx, pathsList, typeFilter, first)
		}
		return e.loaders.noteRowsByType(ctx, typeFilter, first)
	default:
		return nil, fmt.Errorf("notes(...) requires at least one of type, find, property, or semantic")
	}
}

func (e *executor) rootTypedNotes(ctx context.Context, typeName string, args map[string]any) ([]*noteRecord, error) {
	if err := e.validateLinkRootFilters(ctx, typeName, args["filters"]); err != nil {
		return nil, err
	}
	pathArg := strings.TrimSpace(stringValue(args["path"]))
	findArg := strings.TrimSpace(stringValue(args["find"]))
	semanticArgs := semanticArgList(args["semantic"])
	first := rootFirst(args, len(semanticArgs) > 0)
	var records []*noteRecord
	var err error

	switch {
	case pathArg != "":
		if intValue(args["offset"], 0) > 0 {
			return []*noteRecord{}, nil
		}
		record, err := e.rootNote(ctx, pathArg)
		if err != nil {
			return nil, err
		}
		if record == nil || record.TypeName != typeName {
			return []*noteRecord{}, nil
		}
		records = []*noteRecord{record}
	case findArg != "":
		records, err = e.findTypedNotes(ctx, findArg, typeName, first)
	case args["property"] != nil:
		propertyArgs, _ := args["property"].(map[string]any)
		records, err = e.propertyTypedNotes(ctx, propertyArgs, typeName, first)
	case len(semanticArgs) > 0:
		records, err = e.semanticTypedNotes(ctx, semanticArgs, typeName, first)
	default:
		records, err = e.indexedRootTypedNotes(ctx, typeName, args)
		if err != nil {
			return nil, err
		}
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	if limited, _ := args["__selectorCandidateLimit"].(bool); limited && len(records) >= publicRootFirstMax {
		e.rootCandidateBound = true
	}
	return e.applyNoteRootConstraints(ctx, records, args)
}

func (e *executor) validateLinkRootFilters(ctx context.Context, typeName string, raw any) error {
	noteType := e.schema.Types[typeName]
	if noteType == nil {
		return nil
	}
	filters, _ := raw.([]any)
	for _, item := range filters {
		filter, _ := item.(map[string]any)
		field := noteType.ByName[strings.TrimSpace(stringValue(filter["field"]))]
		if field == nil || field.Kind != ontology.FieldKindLink {
			continue
		}
		op := strings.ToLower(strings.TrimSpace(stringValue(filter["op"])))
		if op == "" {
			op = "eq"
		}
		if op != "eq" && op != "in" && op != "exists" {
			return fmt.Errorf("%s.%s is a link: operator %q is unsupported; use eq or in for resolved target membership, or exists", typeName, field.Name, op)
		}
		if op == "exists" {
			continue
		}
		values := []string{stringValue(filter["value"])}
		if op == "in" {
			values = stringListValue(filter["values"])
		}
		if len(values) == 0 {
			return fmt.Errorf("%s.%s link filter %s requires a target value", typeName, field.Name, op)
		}
		for _, value := range values {
			resolved, warning := e.resolveLinkFilterTargetPath(ctx, field, value)
			if resolved == "" {
				return fmt.Errorf("%s.%s: %s", typeName, field.Name, warning.Message)
			}
		}
	}
	return nil
}

func (e *executor) indexedRootTypedNotes(ctx context.Context, typeName string, args map[string]any) ([]*noteRecord, error) {
	noteType := e.schema.Types[typeName]
	if noteType == nil || noteType.Role != ontology.TypeRoleNote {
		return []*noteRecord{}, nil
	}
	plan, residualFilters, residualSort, _ := e.indexedOntologyNodeRootPlan(ctx, typeName, args)
	offset := intValue(args["offset"], 0)
	if residualFilters == nil && residualSort == nil && (e.deps.ReadOverlay == nil || e.deps.ReadOverlay.Empty()) {
		plan.Offset = offset
	} else if offset >= publicRootFirstMax {
		return nil, fmt.Errorf("%s(...) offset must be < %d for residual filters or sort; narrow the query to continue", queryRootFieldName(typeName), publicRootFirstMax)
	}
	if e.deps.ReadOverlay != nil && !e.deps.ReadOverlay.Empty() {
		result, err := e.loaders.scope.TypeInstances(ctx, noderead.TypeInstancesRequest{
			TypeName:   typeName,
			Limit:      publicRootFirstMax,
			Predicates: plan.Predicates,
			Sort:       plan.Sort,
		})
		if err != nil {
			return nil, err
		}
		if len(result.Items) >= publicRootFirstMax {
			e.rootCandidateBound = true
		}
		pathsList := make([]string, 0, len(result.Items))
		for _, item := range result.Items {
			if strings.TrimSpace(item.NotePath) != "" {
				pathsList = append(pathsList, item.NotePath)
			}
		}
		records, err := e.loaders.recordsForPaths(ctx, pathsList)
		if err != nil {
			return nil, err
		}
		return e.applyNoteRootConstraints(ctx, records, map[string]any{
			"filters": residualFilters,
			"sort":    residualSort,
			"first":   args["first"],
			"offset":  offset,
		})
	}
	rows, err := e.deps.Store.OntologyNodesByTypePlan(ctx, plan)
	if err != nil {
		return nil, err
	}
	if (residualFilters != nil || residualSort != nil) && len(rows) >= publicRootFirstMax {
		e.rootCandidateBound = true
	}
	records, err := e.noteRecordsFromCatalogRows(ctx, rows)
	if err != nil {
		return nil, err
	}
	residualArgs := map[string]any{
		"filters": residualFilters,
		"sort":    residualSort,
		"first":   args["first"],
	}
	if plan.Offset == 0 {
		residualArgs["offset"] = offset
	}
	return e.applyNoteRootConstraints(ctx, records, residualArgs)
}

func (e *executor) applyNoteRootConstraints(ctx context.Context, records []*noteRecord, args map[string]any) ([]*noteRecord, error) {
	if args == nil {
		return records, nil
	}
	if rawFilters := args["filters"]; rawFilters != nil {
		filters, linkTargets, err := e.resolvedNoteLinkFilters(ctx, records, rawFilters)
		if err != nil {
			return nil, err
		}
		filtered := records[:0]
		for _, record := range records {
			matches, err := e.noteMatchesRootFilters(record, filters, linkTargets)
			if err != nil {
				return nil, err
			}
			if matches {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	if rawSort := args["sort"]; rawSort != nil {
		e.sortRootNotes(ctx, records, rawSort)
	}
	if offset := intValue(args["offset"], 0); offset > 0 {
		if offset >= len(records) {
			return records[:0], nil
		}
		records = records[offset:]
	}
	first := rootFirst(args, false)
	if first > 0 && len(records) > first {
		records = records[:first]
	}
	return records, nil
}

func (e *executor) resolvedNoteLinkFilters(ctx context.Context, records []*noteRecord, raw any) (any, map[string]map[string][]string, error) {
	filters, _ := raw.([]any)
	if len(filters) == 0 || len(records) == 0 {
		return raw, nil, nil
	}
	paths := make([]string, 0, len(records))
	for _, record := range records {
		paths = append(paths, record.Path)
	}
	resolved := make([]any, len(filters))
	targets := map[string]map[string][]string{}
	loaded := map[string]bool{}
	for i, item := range filters {
		filter, ok := item.(map[string]any)
		if !ok {
			resolved[i] = item
			continue
		}
		fieldName := strings.TrimSpace(stringValue(filter["field"]))
		var field *ontology.Field
		for _, record := range records {
			if noteType := e.schema.Types[record.TypeName]; noteType != nil && noteType.ByName[fieldName] != nil {
				field = noteType.ByName[fieldName]
				break
			}
		}
		if field == nil || field.Kind != ontology.FieldKindLink {
			resolved[i] = item
			continue
		}
		copyFilter := make(map[string]any, len(filter))
		for key, value := range filter {
			copyFilter[key] = value
		}
		if strings.EqualFold(stringValue(filter["op"]), "in") {
			values := stringListValue(filter["values"])
			canonical := make([]any, 0, len(values))
			for _, value := range values {
				path, warning := e.resolveLinkFilterTargetPath(ctx, field, value)
				if path == "" {
					return nil, nil, fmt.Errorf("%s.%s: %s", records[0].TypeName, field.Name, warning.Message)
				}
				canonical = append(canonical, path)
			}
			copyFilter["values"] = canonical
		} else if !strings.EqualFold(stringValue(filter["op"]), "exists") {
			path, warning := e.resolveLinkFilterTargetPath(ctx, field, stringValue(filter["value"]))
			if path == "" {
				return nil, nil, fmt.Errorf("%s.%s: %s", records[0].TypeName, field.Name, warning.Message)
			}
			copyFilter["value"] = path
		}
		resolved[i] = copyFilter
		if loaded[fieldName] {
			continue
		}
		loaded[fieldName] = true
		grouped, err := e.loaders.structuralEdges(ctx, paths, fieldName)
		if err != nil {
			return nil, nil, err
		}
		for path, edges := range grouped {
			if targets[path] == nil {
				targets[path] = map[string][]string{}
			}
			for _, edge := range edges {
				targets[path][fieldName] = append(targets[path][fieldName], edge.DstPath)
			}
		}
	}
	return resolved, targets, nil
}

func (e *executor) rootEmbeddedNodes(ctx context.Context, typeName string, args map[string]any, set ast.SelectionSet) ([]*sectionRecord, error) {
	if err := e.validateLinkRootFilters(ctx, typeName, args["filters"]); err != nil {
		return nil, err
	}
	if e.deps.Store == nil {
		return []*sectionRecord{}, nil
	}
	if records, ok, err := e.indexedRootEmbeddedNodes(ctx, typeName, args, set); ok || err != nil {
		return records, err
	}
	rows, err := e.deps.Store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return nil, err
	}
	pathsList := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Path) == "" {
			continue
		}
		pathsList = append(pathsList, row.Path)
	}
	sort.Strings(pathsList)
	records, err := e.loaders.recordsForPaths(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	typeRows, err := e.loaders.ontologyTypesByPaths(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	first := rootFirst(args, false)
	out := make([]*sectionRecord, 0)
	for _, record := range records {
		if row, ok := typeRows[record.Path]; ok {
			record.TypeName = row.TypeName
		}
		sections, err := e.sectionRecordsForNote(ctx, record)
		if err != nil {
			return nil, err
		}
		for _, section := range sections {
			if section == nil || !e.typeMatchesOrImplements(section.TypeName, typeName) {
				continue
			}
			matches, err := e.sectionMatchesRootFilters(section, args["filters"])
			if err != nil {
				return nil, err
			}
			if !matches {
				continue
			}
			out = append(out, section)
		}
	}
	e.sortEmbeddedRootSections(ctx, out, args["sort"])
	if first > 0 && len(out) > first {
		out = out[:first]
	}
	return out, nil
}

func (e *executor) indexedRootEmbeddedNodes(ctx context.Context, typeName string, args map[string]any, set ast.SelectionSet) ([]*sectionRecord, bool, error) {
	noteType := e.schema.Types[typeName]
	if noteType == nil || noteType.Role != ontology.TypeRoleEmbeddedNode {
		return nil, false, nil
	}
	plan, residualFilters, residualSort, _ := e.indexedOntologyNodeRootPlan(ctx, typeName, args)
	offset := intValue(args["offset"], 0)
	if residualFilters == nil && residualSort == nil && (e.deps.ReadOverlay == nil || e.deps.ReadOverlay.Empty()) {
		plan.Offset = offset
	} else if offset >= publicRootFirstMax {
		return nil, true, fmt.Errorf("%s(...) offset must be < %d for residual filters or sort; narrow the query to continue", queryRootFieldName(typeName), publicRootFirstMax)
	}
	if e.deps.ReadOverlay != nil && !e.deps.ReadOverlay.Empty() {
		result, err := e.loaders.scope.TypeInstances(ctx, noderead.TypeInstancesRequest{
			TypeName:   typeName,
			Limit:      publicRootFirstMax,
			Predicates: plan.Predicates,
			Sort:       plan.Sort,
		})
		if err != nil {
			return nil, true, err
		}
		if len(result.Items) >= publicRootFirstMax {
			e.rootCandidateBound = true
		}
		records, err := e.sectionRecordsFromNodeListItems(ctx, result.Items)
		if err != nil {
			return nil, true, err
		}
		if residualFilters != nil {
			filtered := records[:0]
			for _, record := range records {
				matches, err := e.sectionMatchesRootFilters(record, residualFilters)
				if err != nil {
					return nil, true, err
				}
				if matches {
					filtered = append(filtered, record)
				}
			}
			records = filtered
		}
		if residualSort != nil {
			e.sortEmbeddedRootSections(ctx, records, residualSort)
		}
		records = dropRootOffset(records, offset)
		first := rootFirst(args, false)
		if first > 0 && len(records) > first {
			records = records[:first]
		}
		if e.ontologyNodeRootSelectionNeedsProjection(typeName, set) {
			for i, record := range records {
				projection, err := e.projectSectionRecord(ctx, record)
				if err != nil {
					return nil, true, err
				}
				if projection != nil {
					records[i] = e.sectionRecordFromProjection(record.Note, projection)
				}
			}
		}
		return records, true, nil
	}
	rows, err := e.deps.Store.OntologyNodesByTypePlan(ctx, plan)
	if err != nil {
		return nil, true, err
	}
	if (residualFilters != nil || residualSort != nil) && len(rows) >= publicRootFirstMax {
		e.rootCandidateBound = true
	}
	records, err := e.sectionRecordsFromCatalogRows(ctx, rows)
	if err != nil {
		return nil, true, err
	}
	if residualFilters != nil {
		filtered := records[:0]
		for _, record := range records {
			matches, err := e.sectionMatchesRootFilters(record, residualFilters)
			if err != nil {
				return nil, true, err
			}
			if matches {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	if residualSort != nil {
		e.sortEmbeddedRootSections(ctx, records, residualSort)
	}
	if plan.Offset == 0 {
		records = dropRootOffset(records, offset)
	}
	first := rootFirst(args, false)
	if first > 0 && len(records) > first {
		records = records[:first]
	}
	if e.ontologyNodeRootSelectionNeedsProjection(typeName, set) {
		for i, record := range records {
			projection, err := e.projectSectionRecord(ctx, record)
			if err != nil {
				return nil, true, err
			}
			if projection != nil {
				records[i] = e.sectionRecordFromProjection(record.Note, projection)
			}
		}
	}
	return records, true, nil
}

func dropRootOffset[T any](records []T, offset int) []T {
	if offset <= 0 {
		return records
	}
	if offset >= len(records) {
		return records[:0]
	}
	return records[offset:]
}

func (e *executor) ontologyNodeRootSelectionNeedsProjection(typeName string, set ast.SelectionSet) bool {
	noteType := e.schema.Types[typeName]
	if noteType == nil {
		return true
	}
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			if e.ontologyNodeRootFieldNeedsProjection(noteType, current) {
				return true
			}
		case *ast.InlineFragment:
			if e.sectionMatchesTypeName(typeName, current.TypeCondition) && e.ontologyNodeRootSelectionNeedsProjection(typeName, current.SelectionSet) {
				return true
			}
		case *ast.FragmentSpread:
			fragment := e.fragments.ForName(current.Name)
			if fragment != nil && e.sectionMatchesTypeName(typeName, fragment.TypeCondition) && e.ontologyNodeRootSelectionNeedsProjection(typeName, fragment.SelectionSet) {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func (e *executor) ontologyNodeRootFieldNeedsProjection(noteType *ontology.NoteType, field *ast.Field) bool {
	if noteType == nil || field == nil {
		return true
	}
	switch field.Name {
	case "__typename", "ref", "nodeId", "nodeKind", "id", "path", "notePath", "title", "resolvedType", "locator", "neighborhood", "localGraph", "format", "sourceRepresentation", "evidenceRepresentation", "sourceCapabilities":
		return false
	case "workspace":
		return true
	case "content", "children", "level":
		return true
	}
	schemaField := noteType.ByName[field.Name]
	if schemaField == nil {
		return !isRecordFactField(field.Name)
	}
	switch schemaField.Kind {
	case ontology.FieldKindScalar, ontology.FieldKindEnum, ontology.FieldKindLink:
		_, ok := ontology.IndexedFieldCapabilityForField(e.schema, schemaField)
		return !ok
	case ontology.FieldKindSection, ontology.FieldKindNeighbor, ontology.FieldKindReverse:
		return true
	default:
		return true
	}
}

func (e *executor) sectionMatchesTypeName(typeName, condition string) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" || condition == typeName || condition == "Section" || condition == "Node" {
		return true
	}
	return e.typeMatchesOrImplements(typeName, condition)
}

// indexedOntologyNodeRootPlan plans the indexed ontology node query for one
// public GraphQL root (e.g. {technicalSpec(filters:..., sort:...)}). Filters
// the planner cannot push become residual filters; sort uses prefix-match
// (push the maximal supported prefix; first unsupported key + tail go
// residual). When any residual remains, the limit is widened to
// publicRootFirstMax so the in-memory operation sees the full bounded
// candidate set.
func (e *executor) indexedOntologyNodeRootPlan(ctx context.Context, typeName string, args map[string]any) (codeanchor.OntologyNodeQueryPlan, any, any, []RuntimeWarning) {
	first := rootFirst(args, false)
	plan := codeanchor.OntologyNodeQueryPlan{TypeNames: []string{typeName}, Limit: first}
	if plan.Limit <= 0 {
		plan.Limit = publicRootFirstMax
	}
	planner := pushdown.Planner{
		Schema:       e.schema,
		Resolver:     pushdown.SchemaResolver{NoteType: e.schema.Types[typeName]},
		LinkResolver: executorLinkResolver{e: e},
	}
	var residualFilters any
	var warnings []RuntimeWarning
	if rawFilters, ok := args["filters"].([]any); ok {
		residual := make([]any, 0)
		for _, item := range rawFilters {
			filterMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if noteType := e.schema.Types[typeName]; noteType != nil {
				if field := noteType.ByName[strings.TrimSpace(stringValue(filterMap["field"]))]; field != nil && field.Kind == ontology.FieldKindLink {
					op := strings.ToLower(strings.TrimSpace(stringValue(filterMap["op"])))
					if op != "" && op != "eq" && op != "in" && op != "exists" {
						warnings = append(warnings, RuntimeWarning{Code: "link_filter_operator_unsupported", Message: fmt.Sprintf("%s.%s supports eq, in, and exists; %s does not test resolved target membership", typeName, field.Name, op)})
					}
				}
			}
			result, supported := planner.BuildPredicate(ctx, graphqlFilterToInput(filterMap))
			warnings = append(warnings, planWarningsFromPushdown(result.Warnings)...)
			if supported {
				plan.Predicates = append(plan.Predicates, result.Predicate)
			} else {
				residual = append(residual, filterMap)
			}
		}
		if len(residual) > 0 {
			copied := make([]any, len(residual))
			copy(copied, residual)
			residualFilters = copied
			plan.Limit = publicRootFirstMax
		}
	}
	rawSort, _ := args["sort"].([]any)
	if len(rawSort) == 0 {
		return plan, residualFilters, nil, warnings
	}
	inputs := make([]pushdown.SortInput, 0, len(rawSort))
	for _, item := range rawSort {
		sortMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		inputs = append(inputs, pushdown.SortInput{
			Field:     stringValue(sortMap["field"]),
			Direction: stringValue(sortMap["direction"]),
		})
	}
	// Sorts alone may name the updatedAt record fact, which the store orders by
	// note modification time; filters keep the schema resolver so it never
	// becomes a path predicate. An edit overlay re-sorts in memory, where
	// updatedAt is unknown, so there it stays a residual sort.
	sortPlanner := planner
	if recordFactFree(e.schema, e.schema.Types[typeName], recordFieldUpdatedAt) && (e.deps.ReadOverlay == nil || e.deps.ReadOverlay.Empty()) {
		sortPlanner.Resolver = updatedAtSortResolver{planner.Resolver}
	}
	partition := sortPlanner.PlanSort(inputs)
	for _, item := range partition.Pushed {
		sortItem, ok := sortPlanner.BuildSort(item)
		if ok {
			plan.Sort = append(plan.Sort, sortItem)
		}
	}
	if len(partition.Residual) == 0 {
		return plan, residualFilters, nil, warnings
	}
	// WHY: residual sort runs over the indexed candidate set after the
	// store query returns. Truncating the candidate set to the requested
	// `first` before sorting would let the catalog's note-path/start-byte
	// order win and silently drop rows that should rank higher under the
	// requested sort. Load up to the residual cap so the client-side sort
	// sees the full bounded candidate set.
	plan.Limit = publicRootFirstMax
	// Reapply the full requested sort over the candidate set so the pushed
	// prefix remains semantically primary; the in-memory sort sees a stable,
	// coherent comparator. This matches the configured-views residual-sort
	// contract.
	return plan, residualFilters, rawSort, warnings
}

// graphqlFilterToInput adapts an untyped GraphQL filter map to the planner's
// FilterInput shape.
func graphqlFilterToInput(filter map[string]any) pushdown.FilterInput {
	return pushdown.FilterInput{
		Field:  stringValue(filter["field"]),
		Op:     stringValue(filter["op"]),
		Value:  stringValue(filter["value"]),
		Values: stringListValue(filter["values"]),
	}
}

// planWarningsFromPushdown converts pushdown.Warning to RuntimeWarning for the
// runtime warning channel.
func planWarningsFromPushdown(warnings []pushdown.Warning) []RuntimeWarning {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]RuntimeWarning, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, RuntimeWarning{Code: w.Code, Message: w.Message, Path: w.Path})
	}
	return out
}

// executorLinkResolver adapts the executor's link target resolver to the
// pushdown.LinkResolver interface.
type executorLinkResolver struct {
	e *executor
}

func (r executorLinkResolver) ResolveLinkTarget(ctx context.Context, field *ontology.Field, raw string) (string, pushdown.Warning) {
	resolved, warning := r.e.resolveLinkFilterTargetPath(ctx, field, raw)
	return resolved, pushdown.Warning{Code: warning.Code, Message: warning.Message, Path: warning.Path}
}

func (e *executor) resolveLinkFilterTargetPath(ctx context.Context, field *ontology.Field, raw string) (string, RuntimeWarning) {
	resolved, warning := e.linkTargetResolver.ResolveLinkTarget(ctx, field, raw)
	return resolved, RuntimeWarning{Code: warning.Code, Message: warning.Message, Path: warning.Path}
}

func (e *executor) sectionRecordsFromCatalogRows(ctx context.Context, rows []codeanchor.IntelOntologyNode) ([]*sectionRecord, error) {
	nodeIDs := make([]string, 0, len(rows))
	notePaths := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.NodeID) != "" {
			nodeIDs = append(nodeIDs, row.NodeID)
		}
		if strings.TrimSpace(row.NotePath) != "" {
			notePaths = append(notePaths, row.NotePath)
		}
	}
	fieldRows, err := e.deps.Store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	if err != nil {
		return nil, err
	}
	typeRows, err := e.loaders.ontologyTypesByPaths(ctx, normalizeStrings(notePaths))
	if err != nil {
		return nil, err
	}
	fieldsByNodeID := make(map[string]map[string][]codeanchor.IntelOntologyNodeFieldValue)
	for _, row := range fieldRows {
		fieldName := strings.ToLower(strings.TrimSpace(row.FieldName))
		if strings.TrimSpace(row.NodeID) == "" || fieldName == "" {
			continue
		}
		if fieldsByNodeID[row.NodeID] == nil {
			fieldsByNodeID[row.NodeID] = map[string][]codeanchor.IntelOntologyNodeFieldValue{}
		}
		fieldsByNodeID[row.NodeID][fieldName] = append(fieldsByNodeID[row.NodeID][fieldName], row)
	}
	out := make([]*sectionRecord, 0, len(rows))
	for _, row := range rows {
		if row.NodeKind != string(ontology.NodeKindEmbedded) {
			continue
		}
		ref := nodeRefFromQueryCatalogRow(row)
		node := &ontology.SectionNode{
			ID:        ref.NodeID,
			NotePath:  ref.NotePath,
			Title:     firstNonEmpty(row.DisplayLabel, row.Title, row.SourceLocator, row.NotePath),
			BlockID:   row.BlockID,
			StartByte: int(row.StartByte),
			EndByte:   int(row.EndByte),
		}
		noteTypeName := typeRows[row.NotePath].TypeName
		out = append(out, &sectionRecord{
			Node:          node,
			Note:          &noteRecord{Path: row.NotePath, TypeName: noteTypeName},
			TypeName:      row.TypeName,
			catalogRef:    ref,
			PropertyCase:  e.propertyCaseForType(noteTypeName),
			indexedFields: fieldsByNodeID[row.NodeID],
		})
	}
	return out, nil
}

func (e *executor) sectionRecordsFromNodeListItems(ctx context.Context, items []ontology.NodeListItem) ([]*sectionRecord, error) {
	out := make([]*sectionRecord, 0, len(items))
	for _, item := range items {
		ref := item.Ref
		if ref.NotePath == "" {
			ref.NotePath = item.NotePath
		}
		if ref.Kind == "" {
			ref.Kind = ontology.NodeKindEmbedded
		}
		note, err := e.rootNote(ctx, ref.NotePath)
		if err != nil {
			return nil, err
		}
		if note == nil {
			continue
		}
		noteProjection, err := e.projectNoteRecord(ctx, note)
		if err != nil || noteProjection == nil {
			return nil, err
		}
		projection, err := ontology.ProjectNodeFromSnapshot(noteProjection.Snapshot, e.schema, ref)
		if err != nil || projection == nil {
			continue
		}
		out = append(out, e.sectionRecordFromProjection(note, projection))
	}
	return out, nil
}

func (e *executor) noteRecordsFromCatalogRows(ctx context.Context, rows []codeanchor.IntelOntologyNode) ([]*noteRecord, error) {
	seen := make(map[string]struct{}, len(rows))
	out := make([]*noteRecord, 0, len(rows))
	for _, row := range rows {
		if row.NodeKind != string(ontology.NodeKindNote) {
			continue
		}
		notePath := strings.TrimSpace(row.NotePath)
		if notePath == "" {
			continue
		}
		if _, ok := seen[notePath]; ok {
			continue
		}
		seen[notePath] = struct{}{}
		record, err := e.rootNote(ctx, notePath)
		if err != nil {
			return nil, err
		}
		if record == nil {
			continue
		}
		if record.TypeName == "" {
			record.TypeName = row.TypeName
		}
		out = append(out, record)
	}
	return out, nil
}

func nodeRefFromQueryCatalogRow(row codeanchor.IntelOntologyNode) ontology.NodeRef {
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(row.NodeRefJSON), &ref); err != nil {
		ref = ontology.NodeRef{NotePath: row.NotePath, Kind: ontology.NodeKind(row.NodeKind), NodeID: row.NodeID, TypeName: row.TypeName, Fragment: row.Fragment}
	}
	if ref.NotePath == "" {
		ref.NotePath = row.NotePath
	}
	if ref.NodeID == "" {
		ref.NodeID = row.NodeID
	}
	if ref.TypeName == "" {
		ref.TypeName = row.TypeName
	}
	if ref.Kind == "" {
		ref.Kind = ontology.NodeKind(row.NodeKind)
	}
	if ref.Fragment == "" {
		ref.Fragment = row.Fragment
	}
	return ref
}

func (e *executor) findTypedNotes(ctx context.Context, queryText, typeFilter string, first int) ([]*noteRecord, error) {
	rows, err := e.loaders.selectorMetadataRows(ctx)
	if err != nil {
		return nil, err
	}
	var matches []scoredMatch
	queryLower := strings.ToLower(strings.TrimSpace(queryText))
	tokens := strings.Fields(queryLower)
	for _, row := range rows {
		combined := strings.ToLower(strings.TrimSpace(row.Title) + " " + filepath.ToSlash(row.Path))
		score := 0.0
		if strings.Contains(combined, queryLower) {
			score += 1
		}
		for _, token := range tokens {
			if strings.Contains(combined, token) {
				score += 0.4
			}
		}
		if score == 0 {
			continue
		}
		matches = append(matches, scoredMatch{path: row.Path, score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].path < matches[j].path
	})
	return e.filterTypedRecords(ctx, scoredPaths(matches), typeFilter, first)
}

func (e *executor) propertyTypedNotes(ctx context.Context, args map[string]any, typeFilter string, first int) ([]*noteRecord, error) {
	name := strings.TrimSpace(stringValue(args["name"]))
	value := strings.TrimSpace(stringValue(args["value"]))
	source := PropertySource(strings.ToUpper(strings.TrimSpace(stringValue(args["source"]))))
	if source == "" {
		source = PropertySourceAny
	}
	sourceFilter := semdb.NotePropertySource(0)
	switch source {
	case PropertySourceFrontmatter:
		sourceFilter = semdb.NotePropertySourceFrontmatter
	case PropertySourceInline:
		sourceFilter = semdb.NotePropertySourceInline
	}
	pathsList, err := e.deps.Store.CurrentNotePathsByPropertyValue(ctx, strings.ToLower(name), normalizePropertyValue(value), sourceFilter)
	if err != nil {
		return nil, err
	}
	return e.filterTypedRecords(ctx, pathsList, typeFilter, first)
}

func (e *executor) semanticNotes(ctx context.Context, queryTerms []string, first int) ([]*noteRecord, error) {
	if e.deps.SemanticSearcher == nil {
		return nil, fmt.Errorf("semantic search is unavailable")
	}
	var results []semantic.Result
	for _, queryText := range queryTerms {
		found, err := e.deps.SemanticSearcher.Search(ctx, semantic.SearchRequest{
			QueryText: queryText,
			Filters:   semantic.SearchFilters{Types: []string{"note"}},
			K:         max(first*4, 40),
		})
		if err != nil {
			return nil, err
		}
		results = append(results, found...)
	}
	pathsList := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	scoresByPath := make(map[string]float64, len(results))
	for _, result := range results {
		notePath := strings.TrimSpace(result.NoteID)
		if notePath == "" {
			notePath = strings.TrimSpace(result.Path)
		}
		if notePath == "" {
			continue
		}
		if existing, ok := scoresByPath[notePath]; !ok || result.Score > existing {
			scoresByPath[notePath] = result.Score
		}
		if _, ok := seen[notePath]; ok {
			continue
		}
		seen[notePath] = struct{}{}
		pathsList = append(pathsList, notePath)
	}
	records, err := e.filterTypedRecords(ctx, pathsList, "", first)
	if err != nil {
		return nil, err
	}
	return recordsWithSemanticScores(records, scoresByPath), nil
}

// semanticTypedNotes is a typed-slice survey, not a fallback for unbounded or
// failed queries. Docs: [[ontology-graphql-query-contract#^SPEC-0042-US5]]
func (e *executor) semanticTypedNotes(ctx context.Context, queryTerms []string, typeFilter string, first int) ([]*noteRecord, error) {
	if e.deps.SemanticSearcher == nil {
		return nil, fmt.Errorf("semantic search is unavailable")
	}
	results, err := e.deps.SemanticSearcher.SurveyNodesByType(ctx, semantic.SurveyNodesByTypeRequest{
		QueryTerms: queryTerms,
		TypeNames:  e.possibleConcreteTypes(typeFilter),
		K:          first,
	})
	if err != nil {
		return nil, err
	}
	pathsList := make([]string, 0, len(results))
	scoresByPath := make(map[string]float64, len(results))
	for _, result := range results {
		notePath := strings.TrimSpace(result.NoteID)
		if notePath == "" {
			notePath = strings.TrimSpace(result.Path)
		}
		if notePath == "" {
			continue
		}
		pathsList = append(pathsList, notePath)
		if existing, ok := scoresByPath[notePath]; !ok || result.Score > existing {
			scoresByPath[notePath] = result.Score
		}
	}
	records, err := e.filterTypedRecords(ctx, pathsList, typeFilter, first)
	if err != nil {
		return nil, err
	}
	return recordsWithSemanticScores(records, scoresByPath), nil
}

func (e *executor) filterTypedRecords(ctx context.Context, pathsList []string, typeFilter string, first int) ([]*noteRecord, error) {
	typeRows, err := e.loaders.ontologyTypesByPaths(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	filteredPaths := make([]string, 0, len(pathsList))
	for _, notePath := range pathsList {
		row, ok := typeRows[notePath]
		if !ok {
			continue
		}
		if typeFilter != "" && !e.typeMatchesOrImplements(row.TypeName, typeFilter) {
			continue
		}
		filteredPaths = append(filteredPaths, notePath)
		if first > 0 && len(filteredPaths) >= first {
			break
		}
	}
	if len(filteredPaths) == 0 {
		return []*noteRecord{}, nil
	}
	records, err := e.loaders.recordsForPaths(ctx, filteredPaths)
	if err != nil {
		return nil, err
	}
	out := make([]*noteRecord, 0, len(records))
	for _, record := range records {
		row, ok := typeRows[record.Path]
		if !ok {
			continue
		}
		record.TypeName = row.TypeName
		out = append(out, record)
	}
	return out, nil
}

func recordsWithSemanticScores(records []*noteRecord, scoresByPath map[string]float64) []*noteRecord {
	if len(records) == 0 {
		return records
	}
	out := make([]*noteRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			out = append(out, record)
			continue
		}
		score, ok := scoresByPath[record.Path]
		if !ok {
			out = append(out, record)
			continue
		}
		scored := *record
		scoreCopy := score
		scored.Score = &scoreCopy
		out = append(out, &scored)
	}
	return out
}

func argsWithFirst(args map[string]any, first int) map[string]any {
	out := make(map[string]any, len(args)+1)
	for key, value := range args {
		out[key] = value
	}
	out["first"] = first
	return out
}

func trimRecordsForPage(records []*noteRecord, first int) ([]*noteRecord, bool) {
	if first <= 0 || len(records) <= first {
		return records, false
	}
	return records[:first], true
}

func pageTypedRootRecords[T any](records []T, first, offset int, dropOffset bool) ([]T, bool) {
	if dropOffset {
		if offset >= len(records) {
			return records[:0], false
		}
		records = records[offset:]
	}
	if len(records) > first {
		return records[:first], true
	}
	return records, false
}

func newTypedRootCoverage(first, offset, returned int, hasMore, noLookahead, candidateBound bool) typedRootCoverage {
	coverage := typedRootCoverage{RequestedFirst: first, ReturnedCount: returned, Offset: offset, Status: "complete"}
	if hasMore {
		coverage.Status = "capped"
		coverage.HasMore = &hasMore
		next := offset + returned
		coverage.NextOffset = &next
	} else if candidateBound {
		coverage.Status = "unknown_candidate_bound"
	} else if noLookahead && returned == first {
		coverage.Status = "unknown_page_bound"
		next := offset + returned
		coverage.NextOffset = &next
	} else {
		coverage.HasMore = &hasMore
	}
	return coverage
}

func (e *executor) resolveNodeList(ctx context.Context, records []*noteRecord, set ast.SelectionSet, path []string) []any {
	e.prefetchAssessments(ctx, records)
	e.executeNodeReadPlan(ctx, records, set)
	records = e.loaders.refreshRecords(records)
	out := make([]any, 0, len(records))
	for _, record := range records {
		out = append(out, e.resolveNodeSelectionSet(ctx, record, set, path))
	}
	return out
}

func (e *executor) resolveSectionList(ctx context.Context, records []*sectionRecord, set ast.SelectionSet, path []string) []any {
	refs := make([]ontology.NodeRef, 0, len(records))
	for _, record := range records {
		if record != nil {
			refs = append(refs, e.sectionNodeRef(record))
		}
	}
	e.prefetchRelationCounts(ctx, refs, set)
	out := make([]any, 0, len(records))
	for _, record := range records {
		out = append(out, e.resolveSectionSelectionSet(ctx, record, set, path))
	}
	return out
}

type pageInfo struct {
	QueryShapeVersion string
	RequestedFirst    int
	ReturnedCount     int
	Truncated         bool
	MaxFirst          int
}

func (e *executor) resolveNoteConnectionSelectionSet(ctx context.Context, records []*noteRecord, warnings []RuntimeWarning, first int, truncated bool, set ast.SelectionSet, path []string) map[string]any {
	if warnings == nil {
		warnings = []RuntimeWarning{}
	}
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "nodes":
			out[key] = e.resolveNodeList(ctx, records, field.SelectionSet, append(path, key))
		case "pageInfo":
			out[key] = e.resolvePageInfoSelectionSet(pageInfo{QueryShapeVersion: "v1", RequestedFirst: first, ReturnedCount: len(records), Truncated: truncated, MaxFirst: publicRootFirstMax}, field.SelectionSet, append(path, key))
		case "warnings":
			out[key] = e.resolveWarnings(warnings, field.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NoteConnection", field.Name))
		}
	}
	return out
}

func (e *executor) resolveSearchSelectionSet(ctx context.Context, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	queryTerms := semanticArgList(args["query"])
	first := boundedInt(args["first"], 20, 1, nestedFieldFirstMax)
	typeFilter := strings.TrimSpace(stringValue(args["type"]))
	warnings := []RuntimeWarning{}
	var records []*noteRecord
	fetchFirst := first + 1
	switch {
	case len(queryTerms) == 0:
		warnings = append(warnings, RuntimeWarning{Code: "missing_query", Message: "search requires at least one query term"})
	case e.deps.NoteSearcher == nil:
		warnings = append(warnings, RuntimeWarning{Code: "search_unavailable", Message: "search is unavailable"})
	default:
		var noteTypes []string
		if typeFilter != "" {
			noteTypes = e.possibleConcreteTypes(typeFilter)
		}
		response, err := e.deps.NoteSearcher.SearchNotes(ctx, NoteSearchRequest{Queries: queryTerms, NoteTypes: noteTypes, First: fetchFirst})
		warnings = append(warnings, response.Warnings...)
		if err != nil {
			warnings = append(warnings, RuntimeWarning{Code: "search_failed", Message: err.Error()})
			break
		}
		pathsList := make([]string, 0, len(response.Hits))
		scoresByPath := make(map[string]float64, len(response.Hits))
		for _, hit := range response.Hits {
			pathsList = append(pathsList, hit.Path)
			scoresByPath[hit.Path] = hit.Score
		}
		found, err := e.filterTypedRecords(ctx, pathsList, typeFilter, fetchFirst)
		if err != nil {
			warnings = append(warnings, RuntimeWarning{Code: "search_failed", Message: err.Error()})
			break
		}
		records = recordsWithSemanticScores(found, scoresByPath)
	}
	records, truncated := trimRecordsForPage(records, first)
	out := make(map[string]any)
	for _, selection := range field.SelectionSet {
		current, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(current)
		switch current.Name {
		case "nodes":
			out[key] = e.resolveNodeList(ctx, records, current.SelectionSet, append(path, key))
		case "pageInfo":
			out[key] = e.resolvePageInfoSelectionSet(pageInfo{QueryShapeVersion: "v1", RequestedFirst: first, ReturnedCount: len(records), Truncated: truncated, MaxFirst: nestedFieldFirstMax}, current.SelectionSet, append(path, key))
		case "query":
			out[key] = stringsSliceAny(queryTerms)
		case "warnings":
			out[key] = e.resolveWarnings(warnings, current.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeSearchResult", current.Name))
		}
	}
	return out
}

func (e *executor) resolvePageInfoSelectionSet(info pageInfo, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "queryShapeVersion":
			out[key] = firstNonEmpty(info.QueryShapeVersion, "v1")
		case "requestedFirst":
			out[key] = info.RequestedFirst
		case "returnedCount":
			out[key] = info.ReturnedCount
		case "truncated":
			out[key] = info.Truncated
		case "maxFirst":
			out[key] = info.MaxFirst
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on PageInfo", field.Name))
		}
	}
	return out
}

func (e *executor) executeNodeReadPlan(ctx context.Context, records []*noteRecord, set ast.SelectionSet) {
	if e == nil || len(records) == 0 || len(set) == 0 {
		return
	}
	if e.loaders == nil || e.loaders.scope == nil {
		return
	}
	roots := nodeReadRootsForRecords(records)
	if len(roots) == 0 {
		return
	}
	sourceTypes := possibleTypesForRecords(records)
	rootRefs := make([]ontology.NodeRef, 0, len(roots))
	for _, root := range roots {
		rootRefs = append(rootRefs, root.Ref)
	}
	e.prefetchRelationCounts(ctx, rootRefs, set)
	relations := e.compileNodeRelationSelections(set, sourceTypes)
	fields := e.compileNodeFieldSelection(set, sourceTypes)
	if len(relations) == 0 && len(fields.Names) == 0 && !fields.IncludeBuiltin && !fields.IncludeContent && !fields.IncludeIssues && !fields.IncludeLocators {
		return
	}
	result, err := e.loaders.scope.Execute(ctx, noderead.NodeReadPlan{
		Roots:       roots,
		Fields:      fields,
		Relations:   relations,
		Diagnostics: false,
	})
	if err != nil {
		e.addError(nil, err.Error())
		return
	}
	e.loaders.rememberNodeViews(result.Roots)
	e.loaders.rememberNodeViews(result.Nodes)
	for _, group := range result.Groups {
		noteType := e.schema.Types[group.Source.TypeName]
		if noteType == nil {
			continue
		}
		fieldName, _, _ := strings.Cut(group.Name, relationCacheSeparator)
		field := noteType.ByName[fieldName]
		if field == nil || field.Kind != ontology.FieldKindReverse {
			continue
		}
		key := noderead.RefIdentityKey(group.Source)
		if e.reverseRelations == nil {
			e.reverseRelations = map[string]map[string][]ontology.NodeRef{}
		}
		if e.reverseRelations[key] == nil {
			e.reverseRelations[key] = map[string][]ontology.NodeRef{}
		}
		refs := make([]ontology.NodeRef, 0, len(group.Edges))
		for _, edge := range group.Edges {
			refs = append(refs, edge.Target)
		}
		e.reverseRelations[key][group.Name] = refs
	}
}

func (e *executor) compileNodeRelationSelections(set ast.SelectionSet, sourceTypes []string) []noderead.RelationSelection {
	if e == nil || len(set) == 0 {
		return nil
	}
	out := make([]noderead.RelationSelection, 0)
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			out = append(out, e.compileNodeRelationField(current, sourceTypes)...)
		case *ast.InlineFragment:
			fragmentTypes := sourceTypes
			if current.TypeCondition != "" && current.TypeCondition != "NoteNode" {
				fragmentTypes = intersectStrings(sourceTypes, e.possibleConcreteTypes(current.TypeCondition))
			}
			out = append(out, e.compileNodeRelationSelections(current.SelectionSet, fragmentTypes)...)
		}
	}
	return out
}

func (e *executor) prefetchRelationCounts(ctx context.Context, refs []ontology.NodeRef, set ast.SelectionSet) {
	if e == nil || e.loaders == nil || e.loaders.scope == nil || len(refs) == 0 {
		return
	}
	fields := e.selectedRelationCountFields(set, refs)
	for _, fieldName := range fields {
		selections := noderead.RelationCountSelectionsForField(e.schema, refs, fieldName)
		if len(selections) == 0 {
			continue
		}
		result, err := e.loaders.scope.RelationCounts(ctx, noderead.RelationCountsRequest{Sources: refs, Selections: selections})
		if err != nil {
			e.addError(nil, err.Error())
			continue
		}
		for _, item := range result.Sources {
			key := noderead.RefIdentityKey(item.Source)
			if e.relationCounts[key] == nil {
				e.relationCounts[key] = map[string]int{}
			}
			e.relationCounts[key][fieldName] = item.Count
		}
	}
}

func (e *executor) selectedRelationCountFields(set ast.SelectionSet, refs []ontology.NodeRef) []string {
	types := map[string]struct{}{}
	for _, ref := range refs {
		if ref.TypeName != "" {
			types[ref.TypeName] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	var visit func(ast.SelectionSet)
	visit = func(current ast.SelectionSet) {
		for _, selection := range current {
			switch value := selection.(type) {
			case *ast.Field:
				for typeName := range types {
					if relationField, ok := e.relationFieldForCount(typeName, value.Name); ok {
						seen[relationField.Name] = struct{}{}
					}
				}
			case *ast.InlineFragment:
				visit(value.SelectionSet)
			case *ast.FragmentSpread:
				if fragment := e.fragments.ForName(value.Name); fragment != nil {
					visit(fragment.SelectionSet)
				}
			}
		}
	}
	visit(set)
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (e *executor) relationFieldForCount(typeName, countName string) (*ontology.Field, bool) {
	if e == nil || e.schema == nil || !strings.HasSuffix(countName, "Count") {
		return nil, false
	}
	noteType := e.schema.Types[typeName]
	if noteType == nil || noteType.ByName[countName] != nil {
		return nil, false
	}
	field := noteType.ByName[strings.TrimSuffix(countName, "Count")]
	return field, field != nil && (field.Kind == ontology.FieldKindNeighbor || field.Kind == ontology.FieldKindReverse) && field.List
}

func (e *executor) relationCount(ref ontology.NodeRef, typeName, countName string) (int, bool) {
	field, ok := e.relationFieldForCount(typeName, countName)
	if !ok {
		return 0, false
	}
	counts := e.relationCounts[noderead.RefIdentityKey(ref)]
	if counts == nil {
		return 0, true
	}
	return counts[field.Name], true
}

func (e *executor) compileNodeRelationField(field *ast.Field, sourceTypes []string) []noderead.RelationSelection {
	if field == nil {
		return nil
	}
	switch field.Name {
	case "linked", "backlinked", "connected":
		args := e.fieldArgs(field)
		typeFilter := strings.TrimSpace(stringValue(args["type"]))
		first := intValue(args["first"], 20)
		if first <= 0 {
			first = 20
		}
		loadType := typeFilter
		if _, ok := e.schema.Interfaces[typeFilter]; ok {
			loadType = ""
		}
		targetTypes := e.possibleConcreteTypes(typeFilter)
		return []noderead.RelationSelection{{
			Name:           field.Name,
			SourceTypes:    cloneStrings(sourceTypes),
			Provenance:     builtinProvenance(field.Name),
			IncludeAmbient: true,
			TargetTypes:    optionalSingle(loadType),
			FirstPerSource: first,
			Fields:         e.compileNodeFieldSelection(field.SelectionSet, targetTypes),
			Children:       e.compileNodeRelationSelections(field.SelectionSet, targetTypes),
		}}
	}
	out := make([]noderead.RelationSelection, 0)
	for _, typeName := range sourceTypes {
		noteType := e.schema.Types[typeName]
		if noteType == nil {
			continue
		}
		schemaField := noteType.ByName[field.Name]
		if schemaField == nil {
			continue
		}
		switch schemaField.Kind {
		case ontology.FieldKindLink:
			out = append(out, noderead.RelationSelection{
				Name:              schemaField.Name,
				SourceTypes:       []string{typeName},
				RelationNames:     []string{schemaField.Name},
				IncludeStructural: true,
				Fields:            e.compileNodeFieldSelection(field.SelectionSet, e.possibleConcreteTypes(schemaField.TypeName)),
				Children:          e.compileNodeRelationSelections(field.SelectionSet, e.possibleConcreteTypes(schemaField.TypeName)),
			})
		case ontology.FieldKindNeighbor, ontology.FieldKindReverse:
			selection, ok := noderead.RelationSelectionForField(e.schema, schemaField)
			if !ok {
				continue
			}
			first := e.relationFirst(field)
			selection.SourceTypes = []string{typeName}
			selection.FirstPerSource = first
			selection.Name = relationCacheName(schemaField.Name, first)
			selection.Fields = e.compileNodeFieldSelection(field.SelectionSet, e.possibleConcreteTypes(schemaField.TypeName))
			selection.Children = e.compileNodeRelationSelections(field.SelectionSet, e.possibleConcreteTypes(schemaField.TypeName))
			out = append(out, selection)
		}
	}
	return out
}

func (e *executor) compileNodeFieldSelection(set ast.SelectionSet, sourceTypes []string) noderead.FieldSelection {
	fields := noderead.FieldSelection{}
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			fields.Names = append(fields.Names, current.Name)
			switch current.Name {
			case "path", "title", "frontmatter", "tags", "format", "sourceRepresentation", "evidenceRepresentation", "sourceCapabilities":
				fields.IncludeBuiltin = true
			case "content":
				fields.IncludeBuiltin = true
				fields.IncludeContent = true
			case "locator":
				fields.IncludeBuiltin = true
				fields.IncludeLocators = true
			case "linked", "backlinked", "connected":
				continue
			default:
				for _, sourceType := range sourceTypes {
					noteType := e.schema.Types[sourceType]
					if noteType == nil {
						continue
					}
					schemaField := noteType.ByName[current.Name]
					if schemaField == nil {
						continue
					}
					fields.IncludeBuiltin = true
					if schemaField.Kind == ontology.FieldKindSection {
						fields.IncludeContent = true
					}
				}
			}
		case *ast.InlineFragment:
			fragmentTypes := sourceTypes
			if current.TypeCondition != "" && current.TypeCondition != "NoteNode" {
				fragmentTypes = intersectStrings(sourceTypes, e.possibleConcreteTypes(current.TypeCondition))
			}
			fields = mergeFieldSelections(fields, e.compileNodeFieldSelection(current.SelectionSet, fragmentTypes))
		}
	}
	fields.Names = normalizeStrings(fields.Names)
	return fields
}

func mergeFieldSelections(a, b noderead.FieldSelection) noderead.FieldSelection {
	a.Names = append(a.Names, b.Names...)
	a.Names = normalizeStrings(a.Names)
	a.IncludeBuiltin = a.IncludeBuiltin || b.IncludeBuiltin
	a.IncludeIssues = a.IncludeIssues || b.IncludeIssues
	a.IncludeContent = a.IncludeContent || b.IncludeContent
	a.IncludeLocators = a.IncludeLocators || b.IncludeLocators
	return a
}

func nodeReadRootsForRecords(records []*noteRecord) []noderead.NodeReadRoot {
	roots := make([]noderead.NodeReadRoot, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.Path) == "" {
			continue
		}
		ref := ontology.NodeRef{NotePath: record.Path, Kind: ontology.NodeKindNote, TypeName: record.TypeName}
		key := ref.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, noderead.NodeReadRoot{Ref: ref, TypeName: record.TypeName})
	}
	return roots
}

func possibleTypesForRecords(records []*noteRecord) []string {
	seen := make(map[string]struct{}, len(records))
	out := make([]string, 0, len(records))
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.TypeName) == "" {
			continue
		}
		if _, ok := seen[record.TypeName]; ok {
			continue
		}
		seen[record.TypeName] = struct{}{}
		out = append(out, record.TypeName)
	}
	sort.Strings(out)
	return out
}

func (e *executor) possibleConcreteTypes(typeName string) []string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" || typeName == "NoteNode" {
		out := make([]string, 0, len(e.schema.Types))
		for name, typ := range e.schema.Types {
			if typ != nil && typ.Role != ontology.TypeRoleEmbeddedNode {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	if _, ok := e.schema.Interfaces[typeName]; ok {
		out := make([]string, 0)
		for name := range e.schema.Types {
			if e.typeMatchesOrImplements(name, typeName) {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	return []string{typeName}
}

func intersectStrings(a, b []string) []string {
	if len(a) == 0 {
		return cloneStrings(b)
	}
	allowed := make(map[string]struct{}, len(b))
	for _, value := range b {
		allowed[value] = struct{}{}
	}
	out := make([]string, 0)
	for _, value := range a {
		if _, ok := allowed[value]; ok {
			out = append(out, value)
		}
	}
	return out
}

func optionalSingle(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}

func (e *executor) resolveNodeSelectionSet(ctx context.Context, node *noteRecord, set ast.SelectionSet, path []string) map[string]any {
	return e.resolveSelections(set, path, "unsupported selection on NoteNode", func(condition string) bool {
		return e.noteMatchesTypeCondition(node, condition)
	}, func(field *ast.Field, fieldPath []string) (any, bool) {
		return e.resolveNodeField(ctx, node, field, fieldPath)
	})
}

func (e *executor) resolveNodeField(ctx context.Context, node *noteRecord, field *ast.Field, path []string) (any, bool) {
	switch field.Name {
	case "ref":
		return nodeRefGraphQLValue(ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}), true
	case "nodeId":
		return "note:" + node.Path, true
	case "nodeKind":
		return "NOTE", true
	case "path":
		return node.Path, true
	case "title":
		return node.Title, true
	case "resolvedType":
		return node.TypeName, true
	case "content":
		if node.Content == "" {
			if record, err := e.loaders.hydrateContentForPath(ctx, node.Path); err == nil && record != nil {
				node.Content = record.Content
				node.InlineProps = cloneInlineProps(record.InlineProps)
			}
		}
		return node.Content, true
	case "format":
		return string(node.Format), true
	case "sourceRepresentation":
		return string(node.SourceRepresentation), true
	case "evidenceRepresentation":
		return string(node.EvidenceRepresentation), true
	case "sourceCapabilities":
		values := make([]string, 0, len(node.Capabilities))
		for _, capability := range node.Capabilities {
			values = append(values, string(capability))
		}
		return values, true
	case "frontmatter":
		return node.cloneFrontmatter(), true
	case "tags":
		return node.cloneTags(), true
	case "score":
		if node.Score == nil {
			return nil, true
		}
		return *node.Score, true
	case "locator":
		ref := ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote}
		return e.resolveLocator(ctx, ref), true
	case "workspace":
		return e.resolveWorkspaceSelectionSet(ctx, ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, field.SelectionSet, path), true
	case "neighborhood":
		return e.resolveNeighborhoodSelectionSet(ctx, ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, field, path), true
	case "localGraph":
		return e.resolveLocalGraphSelectionSet(ctx, ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, field, path), true
	case "linked", "backlinked", "connected":
		return e.resolveAmbientBuiltin(ctx, node, field, path)
	}

	schemaField := e.noteFieldForNode(node, field.Name)
	if schemaField == nil {
		if count, ok := e.relationCount(ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, node.TypeName, field.Name); ok {
			return count, true
		}
		switch field.Name {
		case recordFieldUpdatedAt:
			return e.recordUpdatedAt(node.Path), true
		case recordFieldIssueCount:
			return e.recordIssueCount(ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote}), true
		}
		typeName := strings.TrimSpace(node.TypeName)
		if typeName == "" {
			typeName = "NoteNode"
		}
		e.addError(path, fmt.Sprintf("field %q does not exist on %s", field.Name, typeName))
		return nil, false
	}

	switch schemaField.Kind {
	case ontology.FieldKindScalar, ontology.FieldKindEnum:
		return e.resolveScalarField(ctx, node, schemaField, path)
	case ontology.FieldKindLink:
		return e.resolveStructuralField(ctx, node, schemaField, field.SelectionSet, path)
	case ontology.FieldKindNeighbor:
		return e.resolveNeighborField(ctx, node, schemaField, field.SelectionSet, path, e.relationFirst(field))
	case ontology.FieldKindReverse:
		return e.resolveReverseField(ctx, ontology.NodeRef{NotePath: node.Path, Kind: ontology.NodeKindNote, TypeName: node.TypeName}, schemaField, field.SelectionSet, path, e.relationFirst(field))
	case ontology.FieldKindSection:
		return e.resolveSectionField(ctx, node, schemaField, field, path)
	default:
		e.addError(path, fmt.Sprintf("unsupported field kind %q", schemaField.Kind))
		return nil, false
	}
}

func (e *executor) noteFieldForNode(node *noteRecord, fieldName string) *ontology.Field {
	if e == nil || e.schema == nil || node == nil {
		return nil
	}
	if noteType := e.schema.Types[node.TypeName]; noteType != nil {
		if field := noteType.ByName[fieldName]; field != nil {
			return field
		}
	}
	if e.schema.NoteInterfaceAuthored {
		if iface := e.schema.Interfaces["Note"]; iface != nil {
			if field := iface.ByName[fieldName]; field != nil {
				return field
			}
		}
	}
	return nil
}

func (e *executor) resolveSectionField(ctx context.Context, node *noteRecord, field *ontology.Field, gqlField *ast.Field, path []string) (any, bool) {
	if node == nil {
		return nil, true
	}
	sections, err := e.matchSectionsForField(ctx, node, field)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	if field.SectionRequired && len(sections) == 0 {
		e.addError(path, fmt.Sprintf("required section %s.%s is missing", node.TypeName, field.Name))
		return nil, false
	}
	if !field.List && len(sections) > 1 {
		e.addError(path, fmt.Sprintf("section %s.%s must match a single heading", node.TypeName, field.Name))
		return nil, false
	}
	if field.List {
		out := make([]any, 0, len(sections))
		for _, section := range sections {
			out = append(out, e.resolveSectionSelectionSet(ctx, section, gqlField.SelectionSet, path))
		}
		return out, true
	}
	if len(sections) == 0 {
		return nil, true
	}
	return e.resolveSectionSelectionSet(ctx, sections[0], gqlField.SelectionSet, path), true
}

func (e *executor) resolveSectionSelectionSet(ctx context.Context, node *sectionRecord, set ast.SelectionSet, path []string) map[string]any {
	return e.resolveSelections(set, path, "unsupported selection on Section", func(condition string) bool {
		return e.sectionMatchesTypeCondition(node, condition)
	}, func(field *ast.Field, fieldPath []string) (any, bool) {
		return e.resolveSectionNodeField(ctx, node, field, fieldPath)
	})
}

func (e *executor) resolveAnyNodeSelectionSet(ctx context.Context, node *resolvedNode, set ast.SelectionSet, path []string) map[string]any {
	if node == nil {
		return nil
	}
	switch {
	case node.Note != nil:
		return e.resolveNodeSelectionSet(ctx, node.Note, set, path)
	case node.Section != nil:
		return e.resolveSectionSelectionSet(ctx, node.Section, set, path)
	case node.Code != nil:
		return e.resolveCodeSelectionSet(ctx, node.Code, set, path)
	default:
		return nil
	}
}

func (e *executor) resolveCodeSelectionSet(ctx context.Context, node *codeRecord, set ast.SelectionSet, path []string) map[string]any {
	return e.resolveSelections(set, path, "unsupported selection on Node", func(condition string) bool {
		return codeMatchesTypeCondition(node, condition)
	}, func(field *ast.Field, fieldPath []string) (any, bool) {
		return e.resolveCodeField(ctx, node, field, fieldPath)
	})
}

func (e *executor) resolveCodeField(ctx context.Context, node *codeRecord, field *ast.Field, path []string) (any, bool) {
	if node == nil {
		return nil, true
	}
	ref := codeNodeRef(node)
	switch field.Name {
	case "ref":
		return nodeRefGraphQLValue(ref), true
	case "nodeId":
		if node.Anchor != nil {
			return "code-symbol:" + node.Anchor.AnchorID, true
		}
		return "code-file:" + node.Path, true
	case "nodeKind":
		if node.Anchor != nil {
			return "CODE_SYMBOL", true
		}
		return "CODE_FILE", true
	case "path":
		return node.Path, true
	case "title":
		return node.Title, true
	case "resolvedType":
		if node.Anchor != nil {
			return "CodeSymbol", true
		}
		return "CodeFile", true
	case "locator":
		return locatorGraphQLValue(ontology.NodeLocator{
			Ref:           ref,
			Kind:          ref.Kind,
			SourceLocator: queryLocatorKey(ref),
			Status:        ontology.NodeLocatorLinkable,
			LinkTarget: &ontology.NodeLinkTarget{
				Ref:          ref,
				Markdown:     node.Path,
				DisplayLabel: node.Title,
				Exists:       true,
				RequiresFix:  false,
			},
		}), true
	case "neighborhood":
		return e.emptyNeighborhoodSelectionSet(field.SelectionSet, path), true
	case "localGraph":
		return e.emptyLocalGraphSelectionSet(field.SelectionSet, path), true
	case "workspace":
		return nil, true
	case "language":
		if node.Anchor != nil {
			return string(node.Anchor.Lang), true
		}
		return node.Lang, true
	case "symbol":
		if node.Anchor == nil {
			return nil, true
		}
		return node.Anchor.Symbol, true
	case "fqn":
		if node.Anchor == nil {
			return nil, true
		}
		return node.Anchor.FQN, true
	case "signature":
		if node.Anchor == nil {
			return nil, true
		}
		return node.Anchor.Signature, true
	case "docComment":
		if node.Anchor == nil {
			return nil, true
		}
		return node.Anchor.DocComment, true
	case "symbols":
		first := 50
		if args := e.fieldArgs(field); args != nil {
			first = intValue(args["first"], 50)
		}
		symbols := node.Symbols
		if first > 0 && len(symbols) > first {
			symbols = symbols[:first]
		}
		out := make([]any, 0, len(symbols))
		for i := range symbols {
			anchor := symbols[i]
			out = append(out, e.resolveCodeSelectionSet(ctx, &codeRecord{
				Path:   node.Path,
				Title:  firstNonEmpty(anchor.Symbol, anchor.FQN, anchor.AnchorID),
				Lang:   string(anchor.Lang),
				Anchor: &anchor,
			}, field.SelectionSet, path))
		}
		return out, true
	default:
		e.addError(path, fmt.Sprintf("field %q does not exist on code node", field.Name))
		return nil, false
	}
}

func (e *executor) resolveNeighborhoodSelectionSet(ctx context.Context, source ontology.NodeRef, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	first := intValue(args["first"], 20)
	if first <= 0 {
		first = 20
	}
	if first > nestedFieldFirstMax {
		first = nestedFieldFirstMax
	}
	req := noderead.NeighborhoodRequest{
		Sources:           []ontology.NodeRef{source},
		Direction:         traversalDirectionValue(args["direction"]),
		RelationNames:     optionalSingle(stringValue(args["relation"])),
		IncludeStructural: true,
		IncludeAmbient:    true,
		TargetTypes:       optionalSingle(stringValue(args["type"])),
		FirstPerSource:    first,
		FirstTotal:        first,
		Hydrate:           noderead.HydrateOptions{Profile: noderead.HydrateSummary},
	}
	result := noderead.NeighborhoodResult{}
	if e.loaders != nil && e.loaders.scope != nil {
		var err error
		result, err = e.loaders.scope.Neighborhood(ctx, req)
		if err != nil {
			e.addError(path, err.Error())
			result = noderead.NeighborhoodResult{}
		}
	}
	truncated := false
	if len(result.Sources) > 0 {
		truncated = result.Sources[0].Truncated
	}
	out := make(map[string]any)
	for _, selection := range field.SelectionSet {
		current, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(current)
		switch current.Name {
		case "edges":
			edges := make([]any, 0, len(result.Edges))
			for _, edge := range result.Edges {
				edges = append(edges, e.resolveRelationEdgeSelectionSet(edge, current.SelectionSet, append(path, key)))
			}
			out[key] = edges
		case "nodes":
			nodes := make([]any, 0, len(result.Nodes))
			for _, record := range result.Nodes {
				if node, err := e.rootNodeFromRef(ctx, record.Ref); err == nil && node != nil {
					nodes = append(nodes, e.resolveAnyNodeSelectionSet(ctx, node, current.SelectionSet, append(path, key)))
				}
			}
			out[key] = nodes
		case "truncated":
			out[key] = truncated
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeNeighborhood", current.Name))
		}
	}
	return out
}

func (e *executor) emptyNeighborhoodSelectionSet(set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "edges", "nodes":
			out[key] = []any{}
		case "truncated":
			out[key] = false
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeNeighborhood", field.Name))
		}
	}
	return out
}

func (e *executor) resolveRelationEdgeSelectionSet(edge noderead.NeighborhoodEdge, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "source":
			out[key] = nodeRefGraphQLValue(edge.Source)
		case "target":
			out[key] = nodeRefGraphQLValue(edge.Target)
		case "direction":
			out[key] = traversalDirectionGraphQLValue(edge.Direction)
		case "relation":
			out[key] = emptyNil(edge.RelationName)
		case "provenance":
			out[key] = emptyNil(edge.Provenance)
		case "structural":
			out[key] = edge.Structural
		case "targetType":
			out[key] = emptyNil(edge.TargetType)
		case "depth":
			out[key] = edge.Depth
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeRelationEdge", field.Name))
		}
	}
	return out
}

func (e *executor) resolveLocalGraphSelectionSet(ctx context.Context, source ontology.NodeRef, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	nodeLimit := boundedInt(args["nodeLimit"], 50, 1, 500)
	edgeLimit := boundedInt(args["edgeLimit"], 100, 1, 1000)
	result := noderead.GraphResult{}
	if e.loaders != nil && e.loaders.scope != nil {
		var err error
		req := noderead.GraphRequest{
			Sources:          []ontology.NodeRef{source},
			Profile:          graphProfileValue(args["profile"]),
			NodeLimit:        nodeLimit,
			EdgeLimit:        edgeLimit,
			IncludeCodeEdges: boolValue(args["includeCodeEdges"]),
			Diagnostics:      false,
		}
		result, err = e.loaders.scope.Graph(ctx, req)
		if err == nil && len(result.Nodes) == 0 && len(result.Edges) == 0 && (source.Kind == ontology.NodeKindEmbedded || source.Kind == ontology.NodeKindSection) {
			req.Sources = []ontology.NodeRef{{NotePath: source.NotePath, Kind: ontology.NodeKindNote, TypeName: source.TypeName}}
			result, err = e.loaders.scope.Graph(ctx, req)
		}
		if err != nil {
			e.addError(path, err.Error())
			result = noderead.GraphResult{}
		}
	}
	truncated := len(result.Nodes) >= nodeLimit || len(result.Edges) >= edgeLimit
	out := make(map[string]any)
	for _, selection := range field.SelectionSet {
		current, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(current)
		switch current.Name {
		case "nodes":
			nodes := make([]any, 0, len(result.Nodes))
			for _, node := range result.Nodes {
				nodes = append(nodes, e.resolveLocalGraphNodeSelectionSet(node, current.SelectionSet, append(path, key)))
			}
			out[key] = nodes
		case "edges":
			edges := make([]any, 0, len(result.Edges))
			for _, edge := range result.Edges {
				edges = append(edges, e.resolveLocalGraphEdgeSelectionSet(edge, current.SelectionSet, append(path, key)))
			}
			out[key] = edges
		case "truncated":
			out[key] = truncated
		case "diagnostics":
			out[key] = result.Diagnostics
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on LocalGraph", current.Name))
		}
	}
	return out
}

func (e *executor) emptyLocalGraphSelectionSet(set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "nodes", "edges":
			out[key] = []any{}
		case "truncated":
			out[key] = false
		case "diagnostics":
			out[key] = nil
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on LocalGraph", field.Name))
		}
	}
	return out
}

func (e *executor) resolveLocalGraphNodeSelectionSet(node noderead.GraphEndpoint, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "id":
			out[key] = node.ID
		case "ref":
			if strings.TrimSpace(queryLocatorKey(node.Ref)) == "" {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(node.Ref)
			}
		case "nodeKind":
			out[key] = strings.ToUpper(string(node.Kind))
		case "path":
			out[key] = emptyNil(node.Path)
		case "notePath":
			out[key] = emptyNil(node.NotePath)
		case "nodeId":
			out[key] = emptyNil(node.NodeID)
		case "typeName":
			out[key] = emptyNil(node.TypeName)
		case "title":
			out[key] = firstNonEmpty(node.Label, node.Path, node.ID)
		case "sourceLocator":
			out[key] = emptyNil(node.SourceLocator)
		case "parentId":
			out[key] = emptyNil(node.ParentID)
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on LocalGraphNode", field.Name))
		}
	}
	return out
}

func (e *executor) resolveLocalGraphEdgeSelectionSet(edge noderead.GraphReadEdge, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "source":
			out[key] = edge.Source
		case "target":
			out[key] = edge.Target
		case "kind":
			out[key] = edge.Kind
		case "relation":
			out[key] = emptyNil(edge.RelationName)
		case "relationLabel":
			out[key] = emptyNil(edge.RelationLabel)
		case "provenance":
			out[key] = emptyNil(edge.Provenance)
		case "structural":
			out[key] = edge.Structural
		case "weight":
			out[key] = edge.Weight
		case "confidence":
			out[key] = edge.Confidence
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on LocalGraphEdge", field.Name))
		}
	}
	return out
}

func (e *executor) resolveRefSelectionSet(ctx context.Context, input string, set ast.SelectionSet, path []string) map[string]any {
	resolution := e.resolveRef(ctx, input)
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "input":
			out[key] = resolution.Input
		case "found":
			out[key] = resolution.Found
		case "ref":
			if resolution.Ref == nil {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(*resolution.Ref)
			}
		case "candidates":
			candidates := make([]any, 0, len(resolution.Candidates))
			for _, candidate := range resolution.Candidates {
				candidates = append(candidates, nodeRefGraphQLValue(candidate))
			}
			out[key] = candidates
		case "kind":
			if resolution.Ref == nil {
				out[key] = nil
			} else {
				out[key] = graphQLNodeKind(resolution.Ref.Kind)
			}
		case "title":
			out[key] = resolution.Title
		case "path":
			out[key] = resolution.Path
		case "resolvedType":
			out[key] = resolution.ResolvedType
		case "reason":
			out[key] = resolution.Reason
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeRefResolution", field.Name))
		}
	}
	return out
}

type refResolution struct {
	Input        string
	Found        bool
	Ref          *ontology.NodeRef
	Candidates   []ontology.NodeRef
	Title        string
	Path         string
	ResolvedType string
	Reason       string
}

func (e *executor) resolveRef(ctx context.Context, input string) refResolution {
	input = strings.TrimSpace(input)
	res := refResolution{Input: input}
	if input == "" {
		res.Reason = "empty_input"
		return res
	}
	if ref, candidates, ok, err := e.resolveNodeRefTargetCandidates(ctx, input); err != nil {
		res.Reason = err.Error()
		return res
	} else if len(candidates) > 1 {
		res.Candidates = candidates
		res.Reason = "ambiguous"
		return res
	} else if ok {
		node, err := e.rootNodeFromRef(ctx, ref)
		if err != nil || node == nil {
			res.Reason = "unresolved"
			return res
		}
		e.populateRefResolutionFromNode(ctx, &res, node)
		return res
	}
	node, err := e.rootNode(ctx, input)
	if err != nil || node == nil {
		res.Reason = "unresolved"
		return res
	}
	e.populateRefResolutionFromNode(ctx, &res, node)
	return res
}

func (e *executor) populateRefResolutionFromNode(ctx context.Context, res *refResolution, node *resolvedNode) {
	if res == nil || node == nil {
		return
	}
	switch {
	case node.Note != nil:
		outRef := ontology.NodeRef{NotePath: node.Note.Path, Kind: ontology.NodeKindNote, TypeName: node.Note.TypeName}
		res.Ref = &outRef
		res.Found = true
		res.Title = node.Note.Title
		res.Path = node.Note.Path
		res.ResolvedType = node.Note.TypeName
	case node.Section != nil:
		if node.Section.Node == nil {
			res.Reason = "unresolved"
			return
		}
		outRef, err := e.publicSectionRef(ctx, node.Section)
		if err != nil {
			res.Reason = err.Error()
			return
		}
		res.Ref = &outRef
		res.Found = true
		res.Title = node.Section.Node.Title
		res.Path = outRef.String()
		res.ResolvedType = node.Section.TypeName
	case node.Code != nil:
		outRef := codeNodeRef(node.Code)
		res.Ref = &outRef
		res.Found = true
		res.Title = node.Code.Title
		res.Path = node.Code.Path
		if node.Code.Anchor != nil {
			res.ResolvedType = "CodeSymbol"
		} else {
			res.ResolvedType = "CodeFile"
		}
	}
}

func (e *executor) resolveSectionNodeField(ctx context.Context, node *sectionRecord, field *ast.Field, path []string) (any, bool) {
	if node == nil || node.Node == nil {
		return nil, true
	}
	resolveBuiltin := func() (any, bool, bool) {
		switch field.Name {
		case "ref":
			ref, err := e.publicSectionRef(ctx, node)
			if err != nil {
				e.addError(path, err.Error())
				return nil, true, true
			}
			return nodeRefGraphQLValue(ref), true, true
		case "nodeId":
			return "section:" + node.Node.NotePath + "#" + node.Node.ID, true, true
		case "nodeKind":
			if ref := e.sectionNodeRef(node); ref.Kind == ontology.NodeKindEmbedded {
				return "EMBEDDED", true, true
			}
			return "SECTION", true, true
		case "id":
			if node.Projection != nil {
				return node.Projection.Ref.NodeID, true, true
			}
			return node.Node.ID, true, true
		case "path":
			if node.Projection != nil {
				return node.Projection.Ref.String(), true, true
			}
			return node.Node.NotePath + "#" + queryFragmentFromSectionID(node.Node.NotePath, node.Node.ID), true, true
		case "notePath":
			return node.Node.NotePath, true, true
		case "title":
			if node.Projection != nil {
				return nodeCatalogTitleFromProjection(node.Projection), true, true
			}
			return node.Node.Title, true, true
		case "resolvedType":
			return node.TypeName, true, true
		case recordFieldUpdatedAt:
			return e.recordUpdatedAt(node.Node.NotePath), true, true
		case recordFieldIssueCount:
			return e.recordIssueCount(e.sectionNodeRef(node)), true, true
		case "level":
			if node.Projection != nil && node.Node.Level == "" {
				return "", true, true
			}
			return string(node.Node.Level), true, true
		case "content":
			if node.Projection != nil {
				return projectionRecordContent(node.Projection), true, true
			}
			return ontology.SectionBody(node.Node), true, true
		case "locator":
			return e.resolveLocator(ctx, e.sectionNodeRef(node)), true, true
		case "workspace":
			return e.resolveWorkspaceSelectionSet(ctx, e.sectionNodeRef(node), field.SelectionSet, path), true, true
		case "neighborhood":
			return e.resolveNeighborhoodSelectionSet(ctx, e.sectionNodeRef(node), field, path), true, true
		case "localGraph":
			return e.resolveLocalGraphSelectionSet(ctx, e.sectionNodeRef(node), field, path), true, true
		case "children":
			if node.Projection != nil {
				children := e.sectionRecordsFromProjectionField(node.Projection, node.Note, &ontology.Field{Name: "children"})
				first := 20
				if args := e.fieldArgs(field); args != nil {
					first = intValue(args["first"], 20)
				}
				if first > 0 && len(children) > first {
					children = children[:first]
				}
				out := make([]any, 0, len(children))
				for _, child := range children {
					out = append(out, e.resolveSectionSelectionSet(ctx, child, field.SelectionSet, path))
				}
				return out, true, true
			}
			first := 20
			if args := e.fieldArgs(field); args != nil {
				first = intValue(args["first"], 20)
			}
			children := node.Node.Children
			if first > 0 && len(children) > first {
				children = children[:first]
			}
			out := make([]any, 0, len(children))
			for _, child := range children {
				out = append(out, e.resolveSectionSelectionSet(ctx, &sectionRecord{
					Node:         child,
					Note:         node.Note,
					TypeName:     e.childSectionType(node, child),
					PropertyCase: node.PropertyCase,
				}, field.SelectionSet, path))
			}
			return out, true, true
		default:
			return nil, false, false
		}
	}

	noteType := e.schema.Types[node.TypeName]
	if noteType == nil {
		if value, ok, handled := resolveBuiltin(); handled {
			return value, ok
		}
		e.addError(path, fmt.Sprintf("unknown ontology section type %q", node.TypeName))
		return nil, false
	}
	schemaField := noteType.ByName[field.Name]
	if schemaField == nil {
		if count, ok := e.relationCount(e.sectionNodeRef(node), node.TypeName, field.Name); ok {
			return count, true
		}
		if value, ok, handled := resolveBuiltin(); handled {
			return value, ok
		}
		e.addError(path, fmt.Sprintf("field %q does not exist on %s", field.Name, node.TypeName))
		return nil, false
	}
	switch schemaField.Kind {
	case ontology.FieldKindSection:
		matches, err := e.matchNestedSections(ctx, node, schemaField)
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		if schemaField.SectionRequired && len(matches) == 0 {
			e.addError(path, fmt.Sprintf("required section %s.%s is missing", node.TypeName, field.Name))
			return nil, false
		}
		if !schemaField.List && len(matches) > 1 {
			e.addError(path, fmt.Sprintf("section %s.%s must match a single heading", node.TypeName, field.Name))
			return nil, false
		}
		if schemaField.List {
			out := make([]any, 0, len(matches))
			for _, match := range matches {
				out = append(out, e.resolveSectionSelectionSet(ctx, match, field.SelectionSet, path))
			}
			return out, true
		}
		if len(matches) == 0 {
			return nil, true
		}
		return e.resolveSectionSelectionSet(ctx, matches[0], field.SelectionSet, path), true
	case ontology.FieldKindLink:
		return e.resolveSectionStructuralField(ctx, node, schemaField, field.SelectionSet, path)
	case ontology.FieldKindScalar, ontology.FieldKindEnum:
		return e.resolveSectionScalarField(ctx, node, schemaField, path)
	case ontology.FieldKindNeighbor:
		return e.resolveSectionNeighborField(ctx, node, schemaField, field.SelectionSet, path, e.relationFirst(field))
	default:
		e.addError(path, fmt.Sprintf("unsupported field kind %q on section type", schemaField.Kind))
		return nil, false
	}
}

func (e *executor) resolveLocator(ctx context.Context, ref ontology.NodeRef) map[string]any {
	// Docs: [[ontology-browser-workspace#^spec-0014-us5-ac3]] requires GraphQL
	// locator payloads to expose requires-fix status instead of rendering
	// fragile embedded-node links. [[ontology-graphql-query-contract]]
	// keeps the query field aligned with NodeWorkspace/copy-link semantics.
	locator := ontology.NodeLocator{
		Ref:           ref,
		Kind:          ref.Kind,
		SourceLocator: queryLocatorKey(ref),
		Status:        ontology.NodeLocatorUnresolved,
	}
	if e.loaders != nil && e.loaders.scope != nil {
		result, err := e.loaders.scope.Locators(ctx, []ontology.NodeRef{ref})
		if err == nil {
			if resolved, ok := result[queryLocatorKey(ref)]; ok {
				locator = resolved
			}
		}
	}
	return locatorGraphQLValue(locator)
}

// Public refs retain the projected fingerprint. Internal scheduling refs stay
// stable so enriching an output cannot change keys in the warmed read plan.
func (e *executor) publicSectionRef(ctx context.Context, node *sectionRecord) (ontology.NodeRef, error) {
	ref := e.sectionNodeRef(node)
	if node == nil || node.Projection != nil || !node.catalogRef.IsZero() {
		return ref, nil
	}
	projection, err := e.projectSectionRecord(ctx, node)
	if err != nil {
		return ontology.NodeRef{}, fmt.Errorf("cannot project section ref: %w", err)
	}
	if projection == nil {
		return ontology.NodeRef{}, fmt.Errorf("section ref projection is unavailable")
	}
	return projection.Ref, nil
}

func (e *executor) sectionNodeRef(node *sectionRecord) ontology.NodeRef {
	if node == nil {
		return ontology.NodeRef{}
	}
	if node.Projection != nil {
		return node.Projection.Ref
	}
	if !node.catalogRef.IsZero() {
		return node.catalogRef
	}
	if node.Node == nil {
		return ontology.NodeRef{}
	}
	kind := ontology.NodeKindSection
	if noteType := e.schema.Types[node.TypeName]; noteType != nil && noteType.Role == ontology.TypeRoleEmbeddedNode {
		kind = ontology.NodeKindEmbedded
	}
	ref := ontology.NodeRef{
		NotePath: node.Node.NotePath,
		Kind:     kind,
		NodeID:   node.Node.ID,
		TypeName: node.TypeName,
	}
	if blockID := strings.TrimSpace(strings.TrimPrefix(node.Node.BlockID, "^")); blockID != "" {
		ref.Fragment = "^" + blockID
	} else {
		ref.Fragment = queryFragmentFromSectionID(node.Node.NotePath, node.Node.ID)
	}
	return ref
}

func nodeCatalogTitleFromProjection(projection *ontology.NodeProjection) string {
	if projection == nil {
		return ""
	}
	if projection.Ref.NodeID != "" {
		if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
			return span.Title
		}
	}
	return projection.Ref.String()
}

func projectionRecordContent(projection *ontology.NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
		return strings.TrimSpace(span.Content)
	}
	if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
		return ontology.SectionBody(section)
	}
	return ""
}

func codeNodeRef(node *codeRecord) ontology.NodeRef {
	ref := ontology.NodeRef{NotePath: node.Path, Kind: "CODE_FILE", TypeName: "CodeFile"}
	if node.Anchor != nil {
		ref.Kind = "CODE_SYMBOL"
		ref.TypeName = "CodeSymbol"
		ref.NodeID = node.Anchor.AnchorID
		ref.Fragment = "symbol:" + firstNonEmpty(node.Anchor.FQN, node.Anchor.Symbol, node.Anchor.AnchorID)
	}
	return ref
}

func isCodeNodeKind(kind ontology.NodeKind) bool {
	switch kind {
	case "CODE_FILE", "CODE_SYMBOL", "MODULE":
		return true
	default:
		return false
	}
}

func allowsCodeFileFallback(ref ontology.NodeRef) bool {
	if ref.Kind != "" && ref.Kind != ontology.NodeKindNote && ref.Kind != ontology.NodeKindSection {
		return false
	}
	if strings.TrimSpace(ref.NodeID) != "" || strings.TrimSpace(ref.Structural) != "" {
		return false
	}
	// A catalog miss may still name an indexed code symbol. Do not use the
	// filename extension to decide: rootCode confirms code ownership before it
	// returns a code record.
	return true
}

func locatorGraphQLValue(locator ontology.NodeLocator) map[string]any {
	kind := locator.Kind
	if kind == "" {
		kind = locator.Ref.Kind
	}
	out := map[string]any{
		"ref":           nodeRefGraphQLValue(locator.Ref),
		"kind":          graphQLNodeKind(kind),
		"sourceLocator": locator.SourceLocator,
		"status":        string(locator.Status),
		"diagnostics":   locatorDiagnosticsGraphQLValue(locator.Diagnostics),
		"fixActions":    locatorFixActionsGraphQLValue(locator.FixActions),
		"exists":        false,
		"requiresFix":   locator.Status == ontology.NodeLocatorRequiresFix,
	}
	if locator.LinkTarget != nil {
		out["linkTarget"] = linkTargetGraphQLValue(*locator.LinkTarget)
		out["wikilink"] = locator.LinkTarget.Wikilink
		out["markdown"] = locator.LinkTarget.Markdown
		out["exists"] = locator.LinkTarget.Exists
		out["requiresFix"] = locator.LinkTarget.RequiresFix
	}
	return out
}

func nodeRefGraphQLValue(ref ontology.NodeRef) map[string]any {
	kind := graphQLNodeKind(ref.Kind)
	pathValue := ref.NotePath
	return map[string]any{
		"ref":        queryLocatorKey(ref),
		"notePath":   notePathForNodeKind(kind, ref.NotePath),
		"path":       pathValue,
		"fragment":   emptyNil(ref.Fragment),
		"kind":       kind,
		"nodeId":     emptyNil(ref.NodeID),
		"structural": emptyNil(ref.Structural),
		"typeName":   emptyNil(ref.TypeName),
	}
}

func notePathForNodeKind(kind string, notePath string) any {
	switch kind {
	case "NOTE", "SECTION", "EMBEDDED":
		return notePath
	default:
		return nil
	}
}

func graphQLNodeKind(kind ontology.NodeKind) string {
	switch kind {
	case ontology.NodeKindNote:
		return "NOTE"
	case ontology.NodeKindSection:
		return "SECTION"
	case ontology.NodeKindEmbedded:
		return "EMBEDDED"
	case "CODE_SYMBOL":
		return "CODE_SYMBOL"
	case "MODULE":
		return "MODULE"
	default:
		return "CODE_FILE"
	}
}

func emptyNil(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func linkTargetGraphQLValue(target ontology.NodeLinkTarget) map[string]any {
	return map[string]any{
		"ref":          nodeRefGraphQLValue(target.Ref),
		"markdown":     target.Markdown,
		"wikilink":     target.Wikilink,
		"displayLabel": target.DisplayLabel,
		"exists":       target.Exists,
		"requiresFix":  target.RequiresFix,
		"blockId":      target.BlockID,
	}
}

func locatorDiagnosticsGraphQLValue(diagnostics []ontology.NodeLinkDiagnostic) []any {
	out := make([]any, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, map[string]any{
			"code":     diagnostic.Code,
			"notePath": emptyNil(diagnostic.NotePath),
			"ref":      optionalNodeRefGraphQLValue(diagnostic.Ref),
			"blockId":  emptyNil(diagnostic.BlockID),
			"message":  diagnostic.Message,
		})
	}
	return out
}

func locatorFixActionsGraphQLValue(actions []ontology.NodeLinkFixAction) []any {
	out := make([]any, 0, len(actions))
	for _, action := range actions {
		out = append(out, map[string]any{
			"ref":     nodeRefGraphQLValue(action.Ref),
			"blockId": action.BlockID,
		})
	}
	return out
}

func optionalNodeRefGraphQLValue(ref ontology.NodeRef) any {
	if ref.IsZero() {
		return nil
	}
	return nodeRefGraphQLValue(ref)
}

func queryLocatorKey(ref ontology.NodeRef) string {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	ref.Structural = strings.TrimSpace(ref.Structural)
	if ref.Kind == ontology.NodeKindNote {
		return ref.NotePath
	}
	if ref.Fragment != "" {
		return ref.String()
	}
	if ref.NodeID != "" {
		if fragment := queryFragmentFromSectionID(ref.NotePath, ref.NodeID); fragment != "" {
			return ref.NotePath + "#" + fragment
		}
		return ref.NotePath + "#node:" + ref.NodeID
	}
	if ref.Structural != "" {
		return ref.NotePath + "#struct:" + ref.Structural
	}
	return ref.String()
}

func queryFragmentFromSectionID(notePath, id string) string {
	id = strings.TrimSpace(id)
	prefix := strings.TrimSpace(notePath) + "#"
	if strings.HasPrefix(id, prefix) {
		return strings.TrimPrefix(id, prefix)
	}
	if idx := strings.LastIndex(id, "#"); idx >= 0 && idx+1 < len(id) {
		return id[idx+1:]
	}
	return ""
}

func (e *executor) resolveSectionStructuralField(ctx context.Context, node *sectionRecord, field *ontology.Field, set ast.SelectionSet, path []string) (any, bool) {
	if node == nil {
		return nil, true
	}
	values := node.fieldValues(field, node.PropertyCase)
	if field.Required && len(values) == 0 && (!field.List || !node.hasFieldValue(field, node.PropertyCase)) {
		e.addError(path, fmt.Sprintf("required relation %s.%s is missing", node.TypeName, field.Name))
		return nil, false
	}
	if !field.List && len(values) > 1 {
		e.addError(path, fmt.Sprintf("field %s.%s has multiple values but is declared as singular", node.TypeName, field.Name))
		return nil, false
	}
	cache, err := e.notePathCacheForSections(ctx)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	records := make([]*noteRecord, 0, len(values))
	for _, raw := range values {
		targetPath, ok := cache.ResolveNote(unwrapSectionLinkValue(raw))
		if !ok {
			e.addError(path, fmt.Sprintf("field %s.%s target %q does not resolve to a note", node.TypeName, field.Name, raw))
			return nil, false
		}
		record, err := e.rootNote(ctx, targetPath)
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		if record == nil {
			e.addError(path, fmt.Sprintf("field %s.%s target %q does not resolve to a typed note", node.TypeName, field.Name, raw))
			return nil, false
		}
		resolvedType := record.TypeName
		if strings.TrimSpace(resolvedType) == "" {
			resolvedType = "untyped note"
		}
		if !sectionLinkTypeMatchesOrImplements(e.schema, record.TypeName, field.TypeName) {
			e.addError(path, fmt.Sprintf("field %s.%s expects %s but %q resolves to %s", node.TypeName, field.Name, field.TypeName, raw, resolvedType))
			return nil, false
		}
		records = append(records, record)
	}
	if field.List {
		return e.resolveNodeList(ctx, records, set, path), true
	}
	if len(records) == 0 {
		return nil, true
	}
	return e.resolveNodeSelectionSet(ctx, records[0], set, path), true
}

// resolveSectionScalarField reads an inline-property scalar/enum declared on a
// section type. It honors enum membership against the schema and mirrors the
// "required when missing" error shape used by resolveScalarField for note
// fields, but it does not consult the assessment cache — section scalar
// issues already surface via assessNestedSectionIssues at index time, and
// reusing the assessment here would require a nested lookup path.
func (e *executor) resolveSectionScalarField(ctx context.Context, node *sectionRecord, field *ontology.Field, path []string) (any, bool) {
	_ = ctx
	if node == nil {
		return nil, true
	}
	values := node.fieldValues(field, node.PropertyCase)
	if field.Required && len(values) == 0 && (!field.List || !node.hasFieldValue(field, node.PropertyCase)) {
		e.addError(path, fmt.Sprintf("required field %s.%s is missing", node.TypeName, field.Name))
		return nil, false
	}
	if !field.List && len(values) > 1 {
		e.addError(path, fmt.Sprintf("field %s.%s has multiple values but is declared as singular", node.TypeName, field.Name))
		return nil, false
	}
	validValues, invalidValue, hasInvalid := validateSectionScalarValues(values, field, e.schema)
	if hasInvalid {
		if field.Required {
			e.addError(path, fmt.Sprintf("field %s value %q does not match %s", field.Name, invalidValue, field.TypeName))
			return nil, false
		}
		return nil, true
	}
	return fieldValue(field, validValues), true
}

// validateSectionScalarValues mirrors the note-field assessment rules for
// section inline properties: any invalid raw value suppresses the field for
// optional selections and becomes a GraphQL error for required selections.
func validateSectionScalarValues(values []string, field *ontology.Field, schema *ontology.Schema) ([]string, string, bool) {
	if len(values) == 0 || field == nil {
		return values, "", false
	}
	valid := make([]string, 0, len(values))
	for _, raw := range values {
		if !sectionScalarValueMatchesType(raw, field, schema) {
			return nil, raw, true
		}
		valid = append(valid, raw)
	}
	return valid, "", false
}

func sectionScalarValueMatchesType(raw string, field *ontology.Field, schema *ontology.Schema) bool {
	raw = strings.TrimSpace(raw)
	switch field.Kind {
	case ontology.FieldKindEnum:
		if schema == nil {
			return true
		}
		members := ontology.EnumValuesSet(schema, field.TypeName)
		if len(members) == 0 {
			return true
		}
		_, ok := members[raw]
		return ok
	case ontology.FieldKindScalar:
		switch field.TypeName {
		case "String", "ID":
			return raw != ""
		case "URL":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "url"
		case "Date":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "date"
		case "DateTime":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "datetime"
		case "Int":
			_, err := strconv.ParseInt(raw, 10, 64)
			return err == nil
		case "Float":
			_, err := strconv.ParseFloat(raw, 64)
			return err == nil
		case "Boolean":
			_, err := strconv.ParseBool(strings.ToLower(raw))
			return err == nil
		default:
			return raw != ""
		}
	default:
		return true
	}
}

func (e *executor) resolveScalarField(ctx context.Context, node *noteRecord, field *ontology.Field, path []string) (any, bool) {
	values := node.fieldValues(field)
	var fieldAssessment ontology.FieldAssessment
	var hasFieldAssessment bool
	if e.readOverlayTouchesPath(node.Path) {
		fieldAssessment = ontology.AssessScalarFieldValues(node.Path, node.TypeName, field, e.schema, values, node.hasField(field))
		hasFieldAssessment = true
	} else {
		assessment := e.noteAssessment(ctx, node.Path)
		fieldAssessment, hasFieldAssessment = assessment.Field(field.Name)
	}
	if hasFieldAssessment && len(fieldAssessment.ValidValues) > 0 {
		values = append([]string(nil), fieldAssessment.ValidValues...)
	}
	if hasFieldAssessment && len(fieldAssessment.Issues) > 0 {
		if field.Required {
			e.addError(path, fieldAssessment.Issues[0].Message)
			return nil, false
		}
		return nil, true
	}
	if field.Required && len(values) == 0 && (!field.List || !node.hasField(field)) {
		e.addError(path, fmt.Sprintf("required field %s.%s is missing", node.TypeName, field.Name))
		return nil, false
	}
	return fieldValue(field, values), true
}

func (e *executor) readOverlayTouchesPath(notePath string) bool {
	overlay := e.deps.ReadOverlay
	if overlay == nil {
		return false
	}
	path := strings.TrimSpace(notePath)
	if path == "" {
		return false
	}
	if _, ok := overlay.Notes[path]; ok {
		return true
	}
	if _, ok := overlay.UpdatedContentByPath[path]; ok {
		return true
	}
	for _, touched := range overlay.TouchedPaths {
		if strings.TrimSpace(touched) == path {
			return true
		}
	}
	return false
}

func (e *executor) resolveStructuralField(ctx context.Context, node *noteRecord, field *ontology.Field, set ast.SelectionSet, path []string) (any, bool) {
	// Docs: [[ontology-graphql-query-contract]] keeps
	// @link fields on structural ontology edges instead of ambient note links.
	grouped, err := e.loaders.structuralEdges(ctx, []string{node.Path}, field.Name)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	rows := dedupeOntologyEdgeRows(grouped[node.Path])
	records, err := e.edgeRecords(ctx, rows)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	records = e.filterRecordsByType(records, field.TypeName)
	assessment := e.noteAssessment(ctx, node.Path)
	relationAssessment, hasRelationAssessment := assessment.Relation(field.Name)
	if field.List {
		if field.Required && len(records) == 0 && relationAssessmentHasStrictIssue(hasRelationAssessment, relationAssessment) {
			e.addError(path, relationAssessment.Issues[0].Message)
			return nil, false
		}
		if field.Required && len(records) == 0 && !node.hasField(field) {
			e.addError(path, fmt.Sprintf("required relation %s.%s is missing", node.TypeName, field.Name))
			return nil, false
		}
		return e.resolveNodeList(ctx, records, set, path), true
	}
	if len(records) == 0 {
		if field.Required && relationAssessmentHasStrictIssue(hasRelationAssessment, relationAssessment) {
			e.addError(path, relationAssessment.Issues[0].Message)
			return nil, false
		}
		if field.Required {
			e.addError(path, fmt.Sprintf("required relation %s.%s is missing", node.TypeName, field.Name))
			return nil, false
		}
		return nil, true
	}
	return e.resolveNodeSelectionSet(ctx, records[0], set, path), true
}

func (e *executor) resolveNeighborField(ctx context.Context, node *noteRecord, field *ontology.Field, set ast.SelectionSet, path []string, first int) (any, bool) {
	typeFilter := field.TypeName
	if _, ok := e.schema.Interfaces[typeFilter]; ok {
		typeFilter = ""
	}
	rows, ok := e.resolveAmbientRows(ctx, node.Path, "", directionProvenance(field.Direction), typeFilter, 0, path)
	if !ok {
		return nil, false
	}
	records, err := e.edgeRecords(ctx, e.firstEdgeRows(rows, field.TypeName, first))
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	records = firstRecords(e.filterRecordsByType(records, field.TypeName), first)
	return e.resolveNodeList(ctx, records, set, path), true
}

const relationCacheSeparator = "\x00first="

// relationCacheName names a relation selection's batched read by field and
// first, so selecting one field with different limits (`all: children` and
// `children(first: 2)`) keeps each limit's targets apart.
func relationCacheName(field string, first int) string {
	if first <= 0 {
		return field
	}
	return field + relationCacheSeparator + strconv.Itoa(first)
}

// relationFirst is a list relation field's first argument, or 0 (every
// target) when it is absent or not positive.
func (e *executor) relationFirst(field *ast.Field) int {
	return max(intValue(e.fieldArgs(field)["first"], 0), 0)
}

// firstEdgeRows keeps the first rows whose target can be of the expected type
// (an untyped row may still resolve to it), so a capped relation hydrates no
// more than it returns; 0 keeps every row.
func (e *executor) firstEdgeRows(rows []semdb.OntologyEdgeRow, expected string, first int) []semdb.OntologyEdgeRow {
	if first <= 0 {
		return rows
	}
	out := make([]semdb.OntologyEdgeRow, 0, min(first, len(rows)))
	for _, row := range rows {
		if len(out) == first {
			break
		}
		if row.DstType == "" || expected == "" || e.typeMatchesOrImplements(row.DstType, expected) {
			out = append(out, row)
		}
	}
	return out
}

// firstRecords keeps a resolved relation's first records; 0 keeps all.
func firstRecords(records []*noteRecord, first int) []*noteRecord {
	if first > 0 && len(records) > first {
		return records[:first]
	}
	return records
}

func (e *executor) resolveReverseField(ctx context.Context, ref ontology.NodeRef, field *ontology.Field, set ast.SelectionSet, path []string, first int) (any, bool) {
	refs, prefetched := e.reverseRelations[noderead.RefIdentityKey(ref)][relationCacheName(field.Name, first)]
	if !prefetched {
		result, err := e.loaders.scope.Edges(ctx, noderead.NeighborhoodRequest{
			Sources: []ontology.NodeRef{ref}, Direction: noderead.TraversalDirectionInbound,
			RelationNames: []string{field.ReverseField}, IncludeStructural: true,
		})
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		refs = make([]ontology.NodeRef, 0, len(result.Edges))
		for _, edge := range result.Edges {
			refs = append(refs, edge.Target)
		}
	}
	// Keep the first targets of the field's type before hydrating any.
	refs = slices.DeleteFunc(slices.Clone(refs), func(target ontology.NodeRef) bool {
		return !e.typeMatchesOrImplements(target.TypeName, field.TypeName)
	})
	if first > 0 && len(refs) > first {
		refs = refs[:first]
	}
	paths := make([]string, 0, len(refs))
	for _, target := range refs {
		if target.Kind == ontology.NodeKindNote && e.typeMatchesOrImplements(target.TypeName, field.TypeName) {
			paths = append(paths, target.NotePath)
		}
	}
	records, err := e.loaders.recordsForPaths(ctx, paths)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	notesByPath := make(map[string]*noteRecord, len(records))
	for _, record := range records {
		notesByPath[record.Path] = record
	}
	// Keep full refs through this step. Several embedded nodes can share one
	// note path, but each is a separate reverse relation target.
	type targetIndex struct {
		note  bool
		index int
	}
	order := make([]targetIndex, 0, len(refs))
	notes := make([]*noteRecord, 0, len(records))
	sections := make([]*sectionRecord, 0)
	for _, target := range refs {
		if !e.typeMatchesOrImplements(target.TypeName, field.TypeName) {
			continue
		}
		if target.Kind == ontology.NodeKindEmbedded || target.Kind == ontology.NodeKindSection {
			section, err := e.rootSection(ctx, target)
			if err != nil {
				e.addError(path, err.Error())
				return nil, false
			}
			if section != nil && e.typeMatchesOrImplements(section.TypeName, field.TypeName) {
				order = append(order, targetIndex{index: len(sections)})
				sections = append(sections, section)
			}
			continue
		}
		if note := notesByPath[target.NotePath]; note != nil && e.typeMatchesOrImplements(note.TypeName, field.TypeName) {
			order = append(order, targetIndex{note: true, index: len(notes)})
			notes = append(notes, note)
		}
	}
	noteValues := e.resolveNodeList(ctx, notes, set, path)
	sectionValues := e.resolveSectionList(ctx, sections, set, path)
	out := make([]any, 0, len(order))
	for _, target := range order {
		if target.note {
			out = append(out, noteValues[target.index])
		} else {
			out = append(out, sectionValues[target.index])
		}
	}
	return out, true
}

func (e *executor) resolveAmbientBuiltin(ctx context.Context, node *noteRecord, field *ast.Field, path []string) (any, bool) {
	args := e.fieldArgs(field)
	typeFilter := strings.TrimSpace(stringValue(args["type"]))
	first := intValue(args["first"], 20)
	if first <= 0 {
		first = 20
	}
	loadType := typeFilter
	loadFirst := first
	if _, ok := e.schema.Interfaces[typeFilter]; ok {
		loadType = ""
		loadFirst = 0
	}
	rows, ok := e.resolveAmbientRows(ctx, node.Path, "", builtinProvenance(field.Name), loadType, loadFirst, path)
	if !ok {
		return nil, false
	}
	records, err := e.edgeRecords(ctx, rows)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	records = e.filterRecordsByType(records, typeFilter)
	if first > 0 && len(records) > first {
		records = records[:first]
	}
	return e.resolveNodeList(ctx, records, field.SelectionSet, path), true
}

func (e *executor) resolveAmbientRows(ctx context.Context, notePath, relation string, provenance map[string]struct{}, typeFilter string, first int, path []string) ([]semdb.OntologyEdgeRow, bool) {
	grouped, err := e.loaders.ambientEdges(ctx, []string{notePath}, relation, provenance, typeFilter, first)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	return dedupeOntologyEdgeRows(grouped[notePath]), true
}

func (e *executor) edgeRecords(ctx context.Context, rows []semdb.OntologyEdgeRow) ([]*noteRecord, error) {
	pathsList := make([]string, 0, len(rows))
	for _, row := range rows {
		pathsList = append(pathsList, row.DstPath)
	}
	records, err := e.loaders.recordsForPaths(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]string, len(rows))
	for _, row := range rows {
		byPath[row.DstPath] = row.DstType
	}
	for _, record := range records {
		if typeName := byPath[record.Path]; typeName != "" {
			record.TypeName = typeName
		}
	}
	return records, nil
}

func (e *executor) filterRecordsByType(records []*noteRecord, expected string) []*noteRecord {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return records
	}
	out := make([]*noteRecord, 0, len(records))
	for _, record := range records {
		if record == nil || !e.typeMatchesOrImplements(record.TypeName, expected) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (e *executor) addError(path []string, message string) {
	e.errors = append(e.errors, Error{Message: message, Path: cloneStrings(path)})
}

func (e *executor) addErrorWithExtensions(path []string, message string, extensions map[string]any) {
	e.errors = append(e.errors, Error{Message: message, Path: cloneStrings(path), Extensions: extensions})
}

func (e *executor) resolvedType(ctx context.Context, notePath string) (semdb.OntologyNoteTypeRow, bool, error) {
	if e.loaders != nil {
		rows, err := e.loaders.ontologyTypesByPaths(ctx, []string{notePath})
		if err != nil {
			return semdb.OntologyNoteTypeRow{}, false, err
		}
		if row, ok := rows[notePath]; ok {
			return row, true, nil
		}
	}
	if e.deps.Service != nil {
		return e.deps.Service.ResolvedType(ctx, notePath)
	}
	return e.deps.Store.GetOntologyTypeByPath(ctx, notePath)
}

func (e *executor) noteAssessment(ctx context.Context, notePath string) *ontology.NoteAssessment {
	e.assessmentMu.Lock()
	assessment := e.assessmentByPath[notePath]
	known := e.assessmentKnown[notePath]
	e.assessmentMu.Unlock()
	if known {
		return assessment
	}

	if e.deps.Service != nil {
		assessment, _, err := e.deps.Service.Assessment(ctx, notePath)
		if err == nil {
			e.assessmentMu.Lock()
			e.assessmentByPath[notePath] = assessment
			e.assessmentKnown[notePath] = true
			e.assessmentMu.Unlock()
			return assessment
		}
	}
	if e.deps.Store == nil {
		e.assessmentMu.Lock()
		e.assessmentKnown[notePath] = true
		e.assessmentMu.Unlock()
		return nil
	}
	row, ok, err := e.deps.Store.GetOntologyAssessmentByPath(ctx, notePath)
	if err != nil || !ok {
		e.assessmentMu.Lock()
		e.assessmentKnown[notePath] = true
		e.assessmentMu.Unlock()
		return nil
	}
	assessment, err = ontology.AssessmentFromJSON(row.AssessmentJSON)
	if err != nil {
		return nil
	}
	e.assessmentMu.Lock()
	e.assessmentByPath[notePath] = assessment
	e.assessmentKnown[notePath] = true
	e.assessmentMu.Unlock()
	return assessment
}

func (e *executor) prefetchAssessments(ctx context.Context, records []*noteRecord) {
	if e == nil || e.deps.Store == nil || len(records) == 0 {
		return
	}
	pathsList := make([]string, 0, len(records))
	e.assessmentMu.Lock()
	for _, record := range records {
		if record == nil || record.Path == "" || e.assessmentKnown[record.Path] {
			continue
		}
		e.assessmentKnown[record.Path] = true
		pathsList = append(pathsList, record.Path)
	}
	e.assessmentMu.Unlock()
	if len(pathsList) == 0 {
		return
	}
	assessments, err := e.loaders.assessmentsByPaths(ctx, pathsList)
	if err != nil {
		e.assessmentMu.Lock()
		for _, notePath := range pathsList {
			delete(e.assessmentKnown, notePath)
		}
		e.assessmentMu.Unlock()
		return
	}
	e.assessmentMu.Lock()
	defer e.assessmentMu.Unlock()
	for _, notePath := range pathsList {
		assessment := assessments[notePath]
		if assessment == nil {
			delete(e.assessmentKnown, notePath)
			continue
		}
		e.assessmentByPath[notePath] = assessment
	}
}

func dedupeOntologyEdgeRows(rows []semdb.OntologyEdgeRow) []semdb.OntologyEdgeRow {
	if len(rows) < 2 {
		return rows
	}
	seen := make(map[string]struct{}, len(rows))
	out := make([]semdb.OntologyEdgeRow, 0, len(rows))
	for _, row := range rows {
		key := row.DstPath
		if key == "" {
			key = row.SrcPath + "\x00" + row.RelationName + "\x00" + row.Provenance
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

func (e *executor) noteMatchesTypeCondition(node *noteRecord, typeCondition string) bool {
	typeCondition = strings.TrimSpace(typeCondition)
	switch typeCondition {
	case "", "Node", "NoteNode":
		return true
	case "Note":
		return true
	default:
		return e.typeMatchesOrImplements(node.TypeName, typeCondition)
	}
}

func (e *executor) sectionMatchesTypeCondition(node *sectionRecord, typeCondition string) bool {
	typeCondition = strings.TrimSpace(typeCondition)
	switch typeCondition {
	case "", "Node", "Section":
		return true
	case "EmbeddedNode":
		return e.sectionNodeRef(node).Kind == ontology.NodeKindEmbedded
	default:
		return e.typeMatchesOrImplements(node.TypeName, typeCondition)
	}
}

func codeMatchesTypeCondition(node *codeRecord, typeCondition string) bool {
	typeCondition = strings.TrimSpace(typeCondition)
	switch typeCondition {
	case "", "Node":
		return true
	case "CodeFile":
		return node != nil && node.Anchor == nil
	case "CodeSymbol":
		return node != nil && node.Anchor != nil
	default:
		return false
	}
}

func (e *executor) typeMatchesOrImplements(typeName, expected string) bool {
	typeName = strings.TrimSpace(typeName)
	expected = strings.TrimSpace(expected)
	if typeName == "" || expected == "" {
		return false
	}
	if expected == "Node" {
		return true
	}
	if expected == "Note" && typeName != "" {
		noteType := e.schema.Types[typeName]
		return noteType != nil && noteType.Role == ontology.TypeRoleNote
	}
	if typeName == expected {
		return true
	}
	noteType := e.schema.Types[typeName]
	if noteType != nil {
		for _, iface := range noteType.Implements {
			if iface == expected {
				return true
			}
			if e.interfaceImplements(iface, expected) {
				return true
			}
		}
	}
	return e.interfaceImplements(typeName, expected)
}

func (e *executor) interfaceImplements(typeName, expected string) bool {
	if typeName == expected {
		return true
	}
	if typeName == "Section" && expected == "Section" {
		return true
	}
	iface := e.schema.Interfaces[typeName]
	if iface == nil {
		return false
	}
	for _, name := range iface.Implements {
		if name == expected || e.interfaceImplements(name, expected) {
			return true
		}
	}
	return false
}

func (e *executor) sectionNodes(ctx context.Context, note *noteRecord) ([]*ontology.SectionNode, error) {
	if note == nil {
		return nil, nil
	}
	e.sectionsMu.Lock()
	nodes, ok := e.sectionsByPath[note.Path]
	e.sectionsMu.Unlock()
	if ok {
		return nodes, nil
	}
	if note.Content == "" {
		record, err := e.loaders.hydrateContentForPath(ctx, note.Path)
		if err != nil {
			return nil, err
		}
		if record != nil {
			note.Content = record.Content
			note.InlineProps = cloneInlineProps(record.InlineProps)
		}
	}
	nodes = ontology.ParseSections(note.Path, note.Content)
	e.sectionsMu.Lock()
	e.sectionsByPath[note.Path] = nodes
	e.sectionsMu.Unlock()
	return nodes, nil
}

func (e *executor) sectionRecordsForNote(ctx context.Context, note *noteRecord) ([]*sectionRecord, error) {
	out, err := e.headingSectionRecordsForNote(ctx, note)
	if err != nil {
		return nil, err
	}
	itemRecords, err := e.itemProjectionRecordsForNote(ctx, note)
	if err != nil {
		return nil, err
	}
	out = append(out, itemRecords...)
	return out, nil
}

func (e *executor) headingSectionRecordsForNote(ctx context.Context, note *noteRecord) ([]*sectionRecord, error) {
	nodes, err := e.sectionNodes(ctx, note)
	if err != nil {
		return nil, err
	}
	propertyCase := e.propertyCaseForType(note.TypeName)
	out := make([]*sectionRecord, 0)
	var walk func(parentTypeName string, children []*ontology.SectionNode, allowDocumentTitle bool)
	walk = func(parentTypeName string, children []*ontology.SectionNode, allowDocumentTitle bool) {
		for _, child := range children {
			if child == nil {
				continue
			}
			typeName := e.childSectionType(&sectionRecord{TypeName: parentTypeName}, child)
			record := &sectionRecord{
				Node:         child,
				Note:         note,
				TypeName:     typeName,
				PropertyCase: propertyCase,
			}
			out = append(out, record)
			nestedParentType := typeName
			if allowDocumentTitle && len(children) == 1 && child.Level == ontology.SectionLevelH1 && nestedParentType == "Section" {
				// WHY: A lone unmatched H1 is the Markdown document title, not an
				// ontology field. Other unmatched wrappers remain opaque so their
				// descendants cannot leak into level-only section lists.
				nestedParentType = parentTypeName
			}
			walk(nestedParentType, child.Children, false)
		}
	}
	walk(note.TypeName, nodes, true)
	return out, nil
}

func (e *executor) matchSectionsForField(ctx context.Context, node *noteRecord, field *ontology.Field) ([]*sectionRecord, error) {
	if field != nil && field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != ontology.EmbeddedSourceShapeSection {
		projection, err := e.projectNoteRecord(ctx, node)
		if err != nil {
			return nil, err
		}
		return e.sectionRecordsFromProjectionField(projection, node, field), nil
	}
	nodes, err := e.sectionNodes(ctx, node)
	if err != nil {
		return nil, err
	}
	matches := matchSectionNodesForField(nodes, field)
	propertyCase := e.propertyCaseForType(node.TypeName)
	out := make([]*sectionRecord, 0, len(matches))
	for _, match := range matches {
		out = append(out, &sectionRecord{
			Node:         match,
			Note:         node,
			TypeName:     field.TypeName,
			PropertyCase: propertyCase,
		})
	}
	return out, nil
}

func (e *executor) matchNestedSections(ctx context.Context, node *sectionRecord, field *ontology.Field) ([]*sectionRecord, error) {
	if node == nil {
		return nil, nil
	}
	if field != nil && field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != ontology.EmbeddedSourceShapeSection {
		projection := node.Projection
		if projection == nil {
			var err error
			projection, err = e.projectSectionRecord(ctx, node)
			if err != nil {
				return nil, err
			}
		}
		return e.sectionRecordsFromProjectionField(projection, node.Note, field), nil
	}
	if node.Node == nil {
		return nil, nil
	}
	matches := matchSectionNodesForField(node.Node.Children, field)
	out := make([]*sectionRecord, 0, len(matches))
	for _, match := range matches {
		out = append(out, &sectionRecord{
			Node:         match,
			Note:         node.Note,
			TypeName:     field.TypeName,
			PropertyCase: node.PropertyCase,
		})
	}
	return out, nil
}

func (e *executor) itemProjectionRecordsForNote(ctx context.Context, note *noteRecord) ([]*sectionRecord, error) {
	projection, err := e.projectNoteRecord(ctx, note)
	if err != nil {
		return nil, err
	}
	out := make([]*sectionRecord, 0)
	seen := map[string]struct{}{}
	appendItem := func(child *ontology.NodeProjection) {
		if child == nil || child.Snapshot == nil {
			return
		}
		sourceSpan := child.Snapshot.SourceSpansByID[child.Ref.NodeID]
		if sourceSpan == nil || sourceSpan.Shape == ontology.EmbeddedSourceShapeSection {
			return
		}
		key := child.Ref.String() + "\x00" + child.Ref.NodeID + "\x00" + child.Ref.TypeName
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, e.sectionRecordFromProjection(note, child))
	}
	var walk func(*ontology.NodeProjection) error
	walk = func(parent *ontology.NodeProjection) error {
		if parent == nil {
			return nil
		}
		names := make([]string, 0, len(parent.Fields))
		for name := range parent.Fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, ref := range parent.Fields[name].SectionNodes {
				child, err := ontology.ProjectBoundNodeFromSnapshot(parent.Snapshot, e.schema, ref)
				if err != nil {
					return err
				}
				appendItem(child)
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(projection); err != nil {
		return nil, err
	}
	globalRefs, err := ontology.GlobalSourceNodeRefsFromSnapshot(projection.Snapshot, e.schema)
	if err != nil {
		return nil, err
	}
	for _, ref := range globalRefs {
		child, err := ontology.ProjectBoundNodeFromSnapshot(projection.Snapshot, e.schema, ref)
		if err != nil {
			return nil, err
		}
		appendItem(child)
	}
	return out, nil
}

func (e *executor) projectNoteRecord(ctx context.Context, note *noteRecord) (*ontology.NodeProjection, error) {
	if note == nil {
		return nil, nil
	}
	if e.deps.Store == nil {
		return nil, nil
	}
	load := e.deps.Store.CurrentNoteMetadataRowsByPaths
	if e.deps.ExactNoteMetadataRows != nil {
		load = e.deps.ExactNoteMetadataRows
	}
	metadata, err := load(ctx, []string{note.Path})
	if err != nil {
		return nil, err
	}
	row, ok := metadata[note.Path]
	if !ok || row.FormatID != "markdown" || row.Projection.Status != semdb.NoteProjectionStatusCurrent {
		return nil, nil
	}
	if note.Content == "" {
		record, err := e.loaders.hydrateContentForPath(ctx, note.Path)
		if err != nil {
			return nil, err
		}
		if record != nil {
			note.Content = record.Content
			note.InlineProps = cloneInlineProps(record.InlineProps)
		}
	}
	snapshot, err := ontology.BuildDocumentSnapshot(note.Path, note.Content, time.Time{})
	if err != nil {
		return nil, err
	}
	return ontology.ProjectNodeFromSnapshot(snapshot, e.schema, ontology.NodeRef{NotePath: note.Path, Kind: ontology.NodeKindNote, TypeName: note.TypeName})
}

func (e *executor) projectSectionRecord(ctx context.Context, node *sectionRecord) (*ontology.NodeProjection, error) {
	if node == nil {
		return nil, nil
	}
	if node.Projection != nil {
		return node.Projection, nil
	}
	return e.loaders.scope.Projection(ctx, e.sectionNodeRef(node))
}

func (e *executor) sectionRecordsFromProjectionField(projection *ontology.NodeProjection, note *noteRecord, field *ontology.Field) []*sectionRecord {
	if projection == nil || field == nil {
		return nil
	}
	binding := projection.Fields[field.Name]
	out := make([]*sectionRecord, 0, len(binding.SectionNodes))
	for _, ref := range binding.SectionNodes {
		child, err := ontology.ProjectBoundNodeFromSnapshot(projection.Snapshot, e.schema, ref)
		if err != nil {
			continue
		}
		out = append(out, e.sectionRecordFromProjection(note, child))
	}
	return out
}

func (e *executor) sectionRecordFromProjection(note *noteRecord, projection *ontology.NodeProjection) *sectionRecord {
	if projection == nil {
		return nil
	}
	return &sectionRecord{
		Node:         sectionNodeFromProjection(projection),
		Projection:   projection,
		Note:         note,
		TypeName:     projection.ResolvedType,
		PropertyCase: projection.PropertyCase,
	}
}

func sectionNodeFromProjection(projection *ontology.NodeProjection) *ontology.SectionNode {
	if projection == nil || projection.Snapshot == nil {
		return nil
	}
	if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
		return section
	}
	if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
		return &ontology.SectionNode{
			ID:               span.ID,
			NotePath:         span.NotePath,
			Title:            span.Title,
			BlockID:          span.BlockID,
			MalformedBlockID: span.MalformedBlockID,
			StartByte:        span.Range.Start,
			EndByte:          span.Range.End,
			Content:          span.Content,
		}
	}
	return nil
}

// matchSectionNodesForField selects SectionNodes for a @contains field using
// the two supported semantics:
//
//   - `heading` set: find every section with that title at the given level
//     (anywhere in the subtree for note-scoped matching, or among direct
//     children for nested matching — callers decide the starting slice).
//   - list-typed with empty `heading`: take every direct child at the field's
//     level, in document order.
//
// Singular fields must set `heading`; schema compilation enforces that.
//
// Docs: [[ontology-graphql-query-contract#^spec-0042-sections-embedded]]
// freezes the section and embedded-node query semantics.
func matchSectionNodesForField(nodes []*ontology.SectionNode, field *ontology.Field) []*ontology.SectionNode {
	if field == nil {
		return nil
	}
	if strings.TrimSpace(field.SectionHeading) != "" {
		return ontology.FindMatchingSections(nodes, field.SectionLevel, field.SectionHeading)
	}
	if !field.List {
		return nil
	}
	nodes = ontology.UnwrapSingleH1SectionRoot(nodes)
	out := make([]*ontology.SectionNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.Level == field.SectionLevel {
			out = append(out, node)
		}
	}
	return out
}

// propertyCaseForType returns the PropertyCase for a note type, or the kebab
// default when the type is absent (e.g., ambient ImplicitNote).
func (e *executor) propertyCaseForType(typeName string) ontology.PropertyCase {
	if e == nil || e.schema == nil {
		return ontology.PropertyCaseKebab
	}
	if typeName == "" {
		return ontology.PropertyCaseKebab
	}
	if noteType := e.schema.Types[typeName]; noteType != nil && noteType.PropertyCase != "" {
		return noteType.PropertyCase
	}
	return ontology.PropertyCaseKebab
}

func (e *executor) resolveSectionNeighborField(ctx context.Context, node *sectionRecord, field *ontology.Field, set ast.SelectionSet, path []string, first int) (any, bool) {
	if node == nil || node.Note == nil || node.Node == nil {
		return []any{}, true
	}
	if field.Scope != ontology.NeighborScopeSubtree {
		rows, ok := e.resolveAmbientRows(ctx, node.Note.Path, "", directionProvenance(field.Direction), field.TypeName, 0, path)
		if !ok {
			return nil, false
		}
		records, err := e.edgeRecords(ctx, e.firstEdgeRows(rows, field.TypeName, first))
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		return e.resolveNodeList(ctx, firstRecords(records, first), set, path), true
	}
	records, err := e.subtreeNeighborRecords(ctx, node, field, first)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	return e.resolveNodeList(ctx, records, set, path), true
}

func (e *executor) subtreeNeighborRecords(ctx context.Context, node *sectionRecord, field *ontology.Field, first int) ([]*noteRecord, error) {
	cache, err := e.notePathCacheForSections(ctx)
	if err != nil {
		return nil, err
	}
	pathsSeen := make(map[string]struct{})
	pathsList := make([]string, 0)
	if field.Direction != ontology.NeighborDirectionInbound {
		for _, scanned := range ontology.ScanMarkdownBodyLinks(ontology.SectionBody(node.Node), obsidian.DefaultWikilinkOptions, obsidian.DefaultMdLinkOptions) {
			target := scanned.Detail
			var (
				resolved string
				ok       bool
			)
			switch target.LinkType {
			case "mdlink":
				resolved, ok = cache.ResolveMdLink(target.Target, node.Note.Path)
			default:
				resolved, ok = cache.ResolveNote(target.Target)
			}
			if !ok {
				continue
			}
			if _, seen := pathsSeen[resolved]; seen {
				continue
			}
			row, ok, err := e.resolvedType(ctx, resolved)
			if err != nil || !ok {
				continue
			}
			if !e.typeMatchesOrImplements(row.TypeName, field.TypeName) {
				continue
			}
			pathsSeen[resolved] = struct{}{}
			pathsList = append(pathsList, resolved)
		}
	}
	if field.Direction == ontology.NeighborDirectionInbound || field.Direction == ontology.NeighborDirectionBoth {
		inbound, err := e.inboundSectionNeighborIndex(ctx)
		if err != nil {
			return nil, err
		}
		targetID := inboundSectionNeighborKey(node.Note.Path, sectionAnchorMatchKey(node.Node))
		targetIDs := []string{targetID}
		nodeIDTarget := inboundSectionNeighborKey(node.Note.Path, node.Node.ID)
		if nodeIDTarget != targetID {
			targetIDs = append(targetIDs, nodeIDTarget)
		}
		for _, targetID := range targetIDs {
			for _, referrerPath := range inbound[targetID] {
				if _, seen := pathsSeen[referrerPath]; seen {
					continue
				}
				row, ok, err := e.resolvedType(ctx, referrerPath)
				if err != nil || !ok {
					continue
				}
				if !e.typeMatchesOrImplements(row.TypeName, field.TypeName) {
					continue
				}
				pathsSeen[referrerPath] = struct{}{}
				pathsList = append(pathsList, referrerPath)
			}
		}
	}
	return e.filterTypedRecords(ctx, pathsList, field.TypeName, first)
}

func (e *executor) inboundSectionNeighborIndex(ctx context.Context) (map[string][]string, error) {
	e.inboundSectionNeighborsOnce.Do(func() {
		cache, err := e.notePathCacheForSections(ctx)
		if err != nil {
			e.inboundSectionNeighborsErr = err
			return
		}
		byPath, _, err := e.loaders.snapshot(ctx)
		if err != nil {
			e.inboundSectionNeighborsErr = err
			return
		}
		seen := make(map[string]map[string]struct{})
		index := make(map[string][]string)
		addInbound := func(key, referrerPath string) {
			key = strings.TrimSpace(key)
			referrerPath = strings.TrimSpace(referrerPath)
			if key == "" || referrerPath == "" {
				return
			}
			if seen[key] == nil {
				seen[key] = make(map[string]struct{})
			}
			if _, exists := seen[key][referrerPath]; exists {
				return
			}
			seen[key][referrerPath] = struct{}{}
			index[key] = append(index[key], referrerPath)
		}
		for referrerPath, record := range byPath {
			if record == nil || strings.TrimSpace(record.Content) == "" {
				continue
			}
			for _, scanned := range ontology.ScanMarkdownBodyLinks(record.Content, obsidian.DefaultWikilinkOptions, obsidian.DefaultMdLinkOptions) {
				notePath, targetKey, ok := sectionTargetForLink(cache, record.Path, scanned.Detail)
				if !ok {
					continue
				}
				key := inboundSectionNeighborKey(notePath, targetKey)
				addInbound(key, referrerPath)
			}
		}
		if err := e.addProviderInboundSectionNeighbors(ctx, cache, byPath, addInbound); err != nil {
			e.inboundSectionNeighborsErr = err
			return
		}
		if e.loaders != nil && e.loaders.deps.Store != nil && len(byPath) > 0 {
			pathsList := make([]string, 0, len(byPath))
			for path := range byPath {
				pathsList = append(pathsList, path)
			}
			sort.Strings(pathsList)
			rows, err := e.loaders.deps.Store.OntologyEdgesForPaths(ctx, pathsList, false, "", 0)
			if err != nil {
				e.inboundSectionNeighborsErr = err
				return
			}
			nodeIDs := make([]string, 0, len(rows))
			for _, row := range rows {
				if row.Structural && strings.TrimSpace(row.DstNodeID) != "" {
					nodeIDs = append(nodeIDs, row.DstNodeID)
				}
			}
			nodesByID, err := e.loaders.deps.Store.OntologyNodesByIDs(ctx, nodeIDs)
			if err != nil {
				e.inboundSectionNeighborsErr = err
				return
			}
			for _, row := range rows {
				if !row.Structural || strings.TrimSpace(row.DstNodeID) == "" {
					continue
				}
				key := inboundSectionNeighborKey(row.DstPath, row.DstNodeID)
				addInbound(key, row.SrcPath)
				if node, ok := nodesByID[row.DstNodeID]; ok {
					if blockID := strings.TrimSpace(strings.TrimPrefix(node.BlockID, "^")); blockID != "" {
						addInbound(inboundSectionNeighborKey(row.DstPath, "block:"+blockID), row.SrcPath)
					}
					if fragment := strings.TrimSpace(strings.TrimPrefix(node.Fragment, "#")); fragment != "" {
						addInbound(inboundSectionNeighborKey(row.DstPath, "heading:"+slugifySectionHeading(fragment)), row.SrcPath)
					}
				}
			}
		}
		for key := range index {
			sort.Strings(index[key])
		}
		e.inboundSectionNeighbors = index
	})
	return e.inboundSectionNeighbors, e.inboundSectionNeighborsErr
}

func inboundSectionNeighborKey(notePath, targetKey string) string {
	return notePath + "\x00" + targetKey
}

func sectionTargetForLink(cache *obsidian.NotePathCache, fromPath string, target obsidian.LinkDetail) (string, string, bool) {
	if cache == nil {
		return "", "", false
	}
	var resolved obsidian.ResolvedNoteTarget
	var ok bool
	switch target.LinkType {
	case "mdlink":
		path, exists := cache.ResolveMdLink(target.Target, fromPath)
		if !exists {
			return "", "", false
		}
		resolved = obsidian.ResolvedNoteTarget{
			Path:     path,
			Fragment: fragmentFromLinkTarget(target.Target),
		}
		ok = true
	default:
		resolved, ok = cache.ResolveNoteTarget(target.Target)
	}
	if !ok || strings.TrimSpace(resolved.Path) == "" {
		return "", "", false
	}
	targetKey := resolvedTargetSectionKey(resolved)
	if targetKey == "" {
		return "", "", false
	}
	return resolved.Path, targetKey, true
}

func sectionAnchorMatchKey(node *ontology.SectionNode) string {
	if node == nil {
		return ""
	}
	fragment := strings.TrimSpace(node.BlockID)
	if fragment != "" {
		return "block:" + strings.TrimPrefix(fragment, "^")
	}
	if idx := strings.Index(node.ID, "#"); idx >= 0 && idx < len(node.ID)-1 {
		fragment = strings.TrimSpace(node.ID[idx+1:])
		if strings.HasPrefix(fragment, "^") {
			return "block:" + strings.TrimPrefix(fragment, "^")
		}
	}
	if title := strings.TrimSpace(node.Title); title != "" {
		return "heading:" + slugifySectionHeading(title)
	}
	return ""
}

func fragmentFromLinkTarget(target string) string {
	if idx := strings.Index(target, "#"); idx >= 0 && idx < len(target)-1 {
		return target[idx+1:]
	}
	return ""
}

func resolvedTargetSectionKey(target obsidian.ResolvedNoteTarget) string {
	fragment := strings.TrimSpace(target.Fragment)
	if fragment == "" {
		return ""
	}
	if strings.HasPrefix(fragment, "^") {
		return "block:" + strings.TrimPrefix(fragment, "^")
	}
	return "heading:" + slugifySectionHeading(fragment)
}

func slugifySectionHeading(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return "section"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "section"
	}
	return out
}

func (e *executor) childSectionType(parent *sectionRecord, child *ontology.SectionNode) string {
	if parent == nil || child == nil || strings.TrimSpace(parent.TypeName) == "" || parent.TypeName == "Section" {
		return "Section"
	}
	parentType := e.schema.Types[parent.TypeName]
	if parentType == nil {
		return "Section"
	}
	// Prefer heading-matched fields because they're strictly more specific
	// than list-no-heading catch-alls. Walk in two passes so a heading match
	// always wins over an overlapping level-only match.
	headingMatchCount := 0
	headingMatchType := ""
	levelMatchCount := 0
	levelMatchType := ""
	for _, field := range parentType.Fields {
		if field == nil || field.Kind != ontology.FieldKindSection {
			continue
		}
		if field.SectionLevel != child.Level {
			continue
		}
		if strings.TrimSpace(field.SectionHeading) != "" {
			if strings.TrimSpace(field.SectionHeading) == strings.TrimSpace(child.Title) {
				headingMatchCount++
				headingMatchType = field.TypeName
			}
			continue
		}
		if field.List {
			levelMatchCount++
			levelMatchType = field.TypeName
		}
	}
	switch {
	case headingMatchCount == 1 && strings.TrimSpace(headingMatchType) != "":
		return headingMatchType
	case headingMatchCount > 1:
		return "Section"
	case levelMatchCount == 1 && strings.TrimSpace(levelMatchType) != "":
		return levelMatchType
	}
	return "Section"
}

func (e *executor) notePathCacheForSections(ctx context.Context) (*obsidian.NotePathCache, error) {
	e.notePathCacheOnce.Do(func() {
		e.notePathCache, e.notePathCacheErr = buildCurrentNotePathCache(ctx, e.loaders.deps)
	})
	if e.notePathCacheErr != nil {
		return nil, e.notePathCacheErr
	}
	return e.notePathCache, nil
}

func relationAssessmentHasStrictIssue(ok bool, relation ontology.RelationAssessment) bool {
	return ok && len(relation.Issues) > 0
}

func unwrapSectionLinkValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[[") && strings.HasSuffix(raw, "]]") {
		raw = strings.TrimPrefix(strings.TrimSuffix(raw, "]]"), "[[")
		if idx := strings.Index(raw, "|"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

func sectionLinkTypeMatchesOrImplements(schema *ontology.Schema, actual, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if actual == "" || expected == "" {
		return false
	}
	if actual == expected || expected == "Note" {
		return true
	}
	if schema == nil {
		return false
	}
	if noteType := schema.Types[actual]; noteType != nil {
		for _, iface := range noteType.Implements {
			if iface == expected {
				return true
			}
		}
	}
	return false
}

func responseKey(field *ast.Field) string {
	if strings.TrimSpace(field.Alias) != "" {
		return field.Alias
	}
	return field.Name
}

func fieldValue(field *ontology.Field, values []string) any {
	if field.List {
		out := make([]any, 0, len(values))
		for _, value := range values {
			out = append(out, scalarValue(field.TypeName, value))
		}
		return out
	}
	if len(values) == 0 {
		return nil
	}
	return scalarValue(field.TypeName, values[0])
}

func scalarValue(typeName, raw string) any {
	switch typeName {
	case "Int":
		if value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
			return value
		}
	case "Float":
		if value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
			return value
		}
	case "Boolean":
		if value, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(raw))); err == nil {
			return value
		}
	}
	return raw
}

func builtinProvenance(name string) map[string]struct{} {
	switch name {
	case "linked":
		return map[string]struct{}{"body_link": {}}
	case "backlinked":
		return map[string]struct{}{"backlink": {}}
	default:
		return map[string]struct{}{"body_link": {}, "backlink": {}}
	}
}

func directionProvenance(direction ontology.NeighborDirection) map[string]struct{} {
	switch direction {
	case ontology.NeighborDirectionOutbound:
		return map[string]struct{}{"body_link": {}}
	case ontology.NeighborDirectionInbound:
		return map[string]struct{}{"backlink": {}}
	default:
		return map[string]struct{}{"body_link": {}, "backlink": {}}
	}
}

func normalizePropertyValue(v string) string {
	val := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(val, "[[") && strings.HasSuffix(val, "]]") {
		val = strings.TrimSuffix(strings.TrimPrefix(val, "[["), "]]")
	}
	if strings.Contains(val, "|") {
		parts := strings.SplitN(val, "|", 2)
		val = strings.TrimSpace(parts[0])
	}
	return val
}

func scoredPaths[T interface{ getPath() string }](matches []T) []string {
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.getPath())
	}
	return out
}

func intValue(v any, fallback int) int {
	switch current := v.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	default:
		return fallback
	}
}

func boolValue(v any) bool {
	current, _ := v.(bool)
	return current
}

func boundedInt(v any, fallback int, minValue int, maxValue int) int {
	value := intValue(v, fallback)
	if value < minValue {
		return fallback
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func traversalDirectionValue(raw any) noderead.TraversalDirection {
	switch strings.ToUpper(strings.TrimSpace(stringValue(raw))) {
	case "INBOUND":
		return noderead.TraversalDirectionInbound
	case "BOTH":
		return noderead.TraversalDirectionBoth
	default:
		return noderead.TraversalDirectionOutbound
	}
}

func traversalDirectionGraphQLValue(direction noderead.TraversalDirection) string {
	switch direction {
	case noderead.TraversalDirectionInbound:
		return "INBOUND"
	case noderead.TraversalDirectionBoth:
		return "BOTH"
	default:
		return "OUTBOUND"
	}
}

func graphProfileValue(raw any) noderead.GraphProfile {
	switch strings.ToUpper(strings.TrimSpace(stringValue(raw))) {
	case "NOTES_ONLY":
		return noderead.GraphProfileNotesOnly
	case "CODE_AWARE":
		return noderead.GraphProfileCodeAware
	default:
		return noderead.GraphProfileOntologyNative
	}
}

func normalizeNotePath(vaultPaths paths.VaultPaths, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if rel, _, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, raw); err == nil {
		return rel.String(), nil
	}
	return string(paths.NormalizeNotePath(raw)), nil
}

func stringListValue(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s := strings.TrimSpace(stringValue(value)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (e *executor) resolvedNodeRef(node *resolvedNode) ontology.NodeRef {
	switch {
	case node == nil:
		return ontology.NodeRef{}
	case node.Note != nil:
		return ontology.NodeRef{NotePath: node.Note.Path, Kind: ontology.NodeKindNote, TypeName: node.Note.TypeName}
	case node.Section != nil:
		kind := ontology.NodeKindSection
		if noteType := e.schema.Types[node.Section.TypeName]; noteType != nil && noteType.Role == ontology.TypeRoleEmbeddedNode {
			kind = ontology.NodeKindEmbedded
		}
		return ontology.NodeRef{NotePath: node.Section.Node.NotePath, Kind: kind, NodeID: node.Section.Node.ID, TypeName: node.Section.TypeName, Fragment: queryFragmentFromSectionID(node.Section.Node.NotePath, node.Section.Node.ID)}
	case node.Code != nil:
		return codeNodeRef(node.Code)
	default:
		return ontology.NodeRef{}
	}
}

func nodeRefFromString(raw string) ontology.NodeRef {
	raw = strings.TrimSpace(raw)
	ref := ontology.NodeRef{Kind: ontology.NodeKindNote}
	if strings.HasPrefix(raw, "code:") {
		ref.Kind = "CODE_FILE"
		raw = strings.TrimPrefix(raw, "code:")
	}
	if idx := strings.Index(raw, "#"); idx >= 0 {
		ref.NotePath = strings.TrimSpace(raw[:idx])
		ref.Fragment = strings.TrimPrefix(strings.TrimSpace(raw[idx+1:]), "#")
		if strings.HasPrefix(ref.Fragment, "struct:") {
			ref.Structural = strings.TrimSpace(strings.TrimPrefix(ref.Fragment, "struct:"))
			ref.Fragment = ""
			ref.Kind = ontology.NodeKindSection
		} else if strings.HasPrefix(ref.Fragment, "symbol:") {
			ref.Kind = "CODE_SYMBOL"
			ref.TypeName = "CodeSymbol"
		} else if strings.HasPrefix(ref.Fragment, "^") || strings.HasPrefix(ref.Fragment, "section-") || strings.HasPrefix(ref.Fragment, "item-") {
			ref.Kind = ontology.NodeKindEmbedded
		} else if ref.Kind == ontology.NodeKindNote {
			// Resolve a regular fragment through the catalog/projection first. If
			// it is not a note section, rootNode may still confirm an indexed code
			// symbol. A filename extension cannot choose between them.
			ref.Kind = ontology.NodeKindSection
		}
		return ref
	}
	ref.NotePath = raw
	return ref
}

func explicitNodePathRefFromString(raw string) (ontology.NodeRef, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "[[") || strings.Contains(raw, "](") || strings.Contains(raw, "://") {
		return ontology.NodeRef{}, false
	}
	ref := nodeRefFromString(raw)
	notePath := strings.TrimSpace(ref.NotePath)
	if notePath == "" {
		return ontology.NodeRef{}, false
	}
	if strings.HasPrefix(raw, "code:") || strings.Contains(notePath, "/") || filepath.IsAbs(notePath) {
		return ref, true
	}
	return ontology.NodeRef{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func queryRootTypeName(schema *ontology.Schema, root string) string {
	for typeName, noteType := range schema.Types {
		if noteType == nil || (noteType.Role != ontology.TypeRoleNote && noteType.Role != ontology.TypeRoleEmbeddedNode) {
			continue
		}
		if queryRootFieldName(typeName) == root {
			return typeName
		}
	}
	return ""
}
