package query

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"

	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/vektah/gqlparser/v2/ast"
)

func Execute(ctx context.Context, deps Deps, schema *ontoschema.Schema, prepared *PreparedQuery) Result {
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
func ExecutePrepared(ctx context.Context, deps Deps, schema *ontoschema.Schema, execSchema *ExecutableSchema, prepared *PreparedQuery) Result {
	if prepared != nil && prepared.Introspection {
		return ExecuteIntrospection(execSchema, prepared)
	}
	return Execute(ctx, deps, schema, prepared)
}

type executor struct {
	schema         *ontoschema.Schema
	deps           Deps
	loaders        *loaders
	variables      map[string]any
	fragments      ast.FragmentDefinitionList
	errors         []Error
	sectionsByPath map[string][]*ontology.SectionNode
	notePathCache  *obsidian.NotePathCache

	inboundSectionNeighbors map[string]map[string][]string

	relationCounts     map[string]map[string]int
	reverseRelations   map[string]map[string][]ontology.NodeRef
	typedRootCoverage  map[string]typedRootCoverage
	linkTargetResolver *pushdown.CatalogLinkResolver

	// Record facts resolve after the selection tree (fillRecordFacts).
	recordFactPaths  map[string]struct{}
	recordFactScopes map[semdb.ValidationScope]struct{}
}

// Typed roots keep their historic array shape. Coverage is keyed by the
// response alias so independently bounded roots never share page state.
type rootPage[T any] struct {
	records        []T
	candidateBound bool
}

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
		records, err := e.rootNotesWithMax(ctx, argsWithFirst(args, requestedFirst+1), requestedFirst+1)
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
			if noteType := e.schema.Types[typeName]; noteType != nil && noteType.Role == ontoschema.TypeRoleEmbeddedNode {
				page, err := e.rootEmbeddedNodes(ctx, typeName, args, field.SelectionSet)
				if err != nil {
					e.addError(path, err.Error())
					return nil, false
				}
				records, hasMore := pageTypedRootRecords(page.records, first, offset, false)
				e.typedRootCoverage[path[len(path)-1]] = newTypedRootCoverage(first, offset, len(records), hasMore, fetchFirst <= first, page.candidateBound)
				return e.resolveSectionList(ctx, records, field.SelectionSet, path), true
			}
			page, err := e.rootTypedNotes(ctx, typeName, args)
			if err != nil {
				e.addError(path, err.Error())
				return nil, false
			}
			records, hasMore := pageTypedRootRecords(page.records, first, offset, selector)
			e.typedRootCoverage[path[len(path)-1]] = newTypedRootCoverage(first, offset, len(records), hasMore, fetchFirst <= first, page.candidateBound)
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
	Error *diag.Warning
}

func (e *executor) resolveNodeBatchSelectionSet(ctx context.Context, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	first := boundedInt(args["first"], 50, nestedFieldFirstMax)
	refs := stringListValue(args["refs"])
	if len(refs) > first {
		refs = refs[:first]
	}
	pathsList := make([]string, 0, len(refs))
	for _, input := range refs {
		if ref, ok := explicitNodePathRefFromString(input); ok && !isCodeNodeKind(ref.Kind) {
			pathsList = append(pathsList, ref.NotePath)
		}
	}
	if _, err := e.loaders.ontologyTypesByPaths(ctx, pathsList); err == nil {
		_, _ = e.loaders.recordsForPaths(ctx, pathsList)
	}
	items := make([]nodeBatchItem, 0, len(refs))
	for _, ref := range refs {
		label := strings.TrimSpace(ref)
		node, err := e.rootNode(ctx, ref)
		item := nodeBatchItem{Input: label}
		if err != nil {
			item.Error = &diag.Warning{Code: "unresolved_ref", Message: err.Error()}
			items = append(items, item)
			continue
		}
		if node == nil {
			item.Error = &diag.Warning{Code: "unresolved_ref", Message: "node ref did not resolve"}
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
				rows := e.resolveWarnings([]diag.Warning{*item.Error}, field.SelectionSet, append(path, key))
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
