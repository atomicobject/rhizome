package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/queryengine"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/vektah/gqlparser/v2/ast"
)

// GraphQL roots the index generation covers: they read the store, note files
// the watcher indexes through the store, the schema, or validation state.
// Typed roots (ExecutableSchema.RootTypes) are covered too.
var graphQLCoveredRoots = map[string]bool{
	"node": true, "nodes": true, "note": true, "notes": true,
	"resolve": true, "validation": true, "ontology": true,
}

// GraphQL roots that read state outside the generation and are never cached:
// search ranks with embeddings, which write through their own runtime, and
// code context packs read code files and code embeddings.
var graphQLBypassRoots = map[string]bool{"search": true, "code": true}

// Fields under `ontology` whose answer depends on the caller or the clock:
// currentUser is the configured person, and nextId allocates identifiers that
// may embed the current time.
var graphQLCallerDependentFields = map[string]bool{"currentUser": true, "nextId": true}

// Fields under `ontology` that read only the schema and the index.
var graphQLCoveredOntologyFields = map[string]bool{
	"schemaHash": true, "type": true, "types": true, "authoringGuide": true, "queryPlan": true, "__typename": true,
}

// publicGraphQLBody answers one public GraphQL request as a response body
// carrying extensions.generation. Reads without an edit session, semantic
// arguments, a bypassed root, a caller-dependent field, or a field read from
// note files go through the read cache after readiness admission, when the
// generation is cacheable (SPEC-0136 R4).
func (s *Server) publicGraphQLBody(ctx context.Context, generation string, cacheable bool, req OntologyQueryRequest) ([]byte, error) {
	queryHash := readQueryHash("graphql", req.Query, strings.TrimSpace(req.OperationName))
	if req.EditSession != nil {
		s.reads.bypass(queryHash)
		overlay, err := s.readOverlayForEditSession(ctx, req.EditSession)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errInvalidEditSessionRead, err)
		}
		resp, err := s.executeOntologyQueryOperation(ctx, req.Query, req.Variables, req.OperationName, overlay)
		if err != nil {
			return nil, err
		}
		return graphQLResponseBody(resp, generation)
	}

	defs, err := s.ontologyDefinitions()
	if err != nil {
		return nil, err
	}
	if defs == nil {
		return nil, fmt.Errorf("ontology is unavailable")
	}
	if defs.exec == nil {
		if defs.execErr != nil {
			return nil, fmt.Errorf("ontology query schema is unavailable: %w", defs.execErr)
		}
		return nil, fmt.Errorf("ontology query schema is unavailable")
	}
	prepared, errs := ontologyquery.PrepareWithOptions(defs.exec, req.Query, ontologyquery.PrepareOptions{
		Variables:     req.Variables,
		OperationName: strings.TrimSpace(req.OperationName),
	})
	if len(errs) > 0 {
		return graphQLResponseBody(ontologyquery.Result{Errors: errs}, generation)
	}
	// Readiness admission runs for every request, hit or miss.
	store, exactSnapshot, err := s.waitForPreparedQuery(ctx, prepared)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("ontology is unavailable")
	}
	execute := func(ctx context.Context, prepared *ontologyquery.PreparedQuery, exactSnapshot *exactNoteReadSnapshot) (ontologyquery.Result, error) {
		deps, err := s.ontologyQueryDepsForExactPaths(ontology.NewService(store, defs.schema), nil, exactSnapshot)
		if err != nil {
			return ontologyquery.Result{}, err
		}
		engine, err := queryengine.New(defs.schema, defs.exec, deps)
		if err != nil {
			return ontologyquery.Result{}, err
		}
		return engine.Run(ctx, prepared)
	}
	run := func(ctx context.Context) ([]byte, bool, error) {
		result, err := execute(ctx, prepared, exactSnapshot)
		if err != nil {
			return nil, false, err
		}
		body, err := graphQLResponseBody(result, generation)
		if err != nil {
			return nil, false, err
		}
		return body, graphQLResultStorable(result) && s.readStillCurrent(ctx, generation), nil
	}
	direct := func() ([]byte, error) {
		s.reads.bypass(queryHash)
		body, _, err := run(ctx)
		return body, err
	}
	if !cacheable || graphQLReadBypassed(defs, prepared) {
		return direct()
	}
	resolve := resolveRefsWith(func(ctx context.Context, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
		return execute(ctx, prepared, nil)
	}, defs.exec)
	witness, err := s.namedSourceWitness(ctx, generation, prepared, resolve)
	if err != nil {
		return direct()
	}
	variables, err := json.Marshal(prepared.Variables)
	if err != nil {
		return nil, err
	}
	key := readCacheKey(generation, witness, "graphql", req.Query, strings.TrimSpace(req.OperationName), string(variables))
	return s.reads.do(ctx, key, queryHash, run)
}

// readStillCurrent reports whether a body read at generation may be stored:
// the generation did not change during execution, no write was open when it
// was read, and the index is ready, so a startup read cannot outlive the
// readiness transition.
func (s *Server) readStillCurrent(ctx context.Context, generation string) bool {
	current, cacheable := s.indexGeneration(ctx)
	return cacheable && current == generation && s.runtime.IndexReady()
}

func graphQLResponseBody(resp ontologyquery.Result, generation string) ([]byte, error) {
	for i := range resp.Errors {
		if resp.Errors[i].Extensions == nil {
			resp.Errors[i].Extensions = map[string]any{"code": PublicErrorGraphQLValidation}
		}
	}
	if resp.Extensions == nil {
		resp.Extensions = map[string]any{}
	}
	resp.Extensions["generation"] = generation
	return encodeJSONBody(resp)
}

// encodeJSONBody encodes like writeJSON, so cached and direct bodies match.
func encodeJSONBody(payload any) ([]byte, error) {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

// graphQLResultStorable allows field errors with a path; any other error makes
// the response uncacheable.
func graphQLResultStorable(resp ontologyquery.Result) bool {
	for _, err := range resp.Errors {
		if len(err.Path) == 0 {
			return false
		}
	}
	return true
}

// graphQLReadBypassed reports whether a prepared read depends on state the
// generation does not cover.
func graphQLReadBypassed(defs *ontologyDefinitions, prepared *ontologyquery.PreparedQuery) bool {
	if prepared.UsesSemantic || prepared.UsesSearch {
		return true
	}
	if prepared.Introspection {
		return false
	}
	exec := defs.exec
	bypassed := false
	visitGraphQLFields(prepared.Document, prepared.Operation.SelectionSet, func(root *ast.Field) {
		switch {
		case graphQLBypassRoots[root.Name]:
			bypassed = true
		case root.Name == "ontology":
			visitGraphQLFields(prepared.Document, root.SelectionSet, func(field *ast.Field) {
				if !graphQLCoveredOntologyFields[field.Name] {
					bypassed = true
				}
			})
		case root.Name == "validation" || root.Name == "__typename":
		case graphQLCoveredRoots[root.Name] || exec.RootTypes[root.Name] != "":
			if selectionReadsNoteFiles(defs.schema, prepared.Document, root.SelectionSet) {
				bypassed = true
			}
		default:
			// A root this file does not classify is never cached.
			bypassed = true
		}
	})
	return bypassed
}

// graphQLIndexFields are the built-in fields, on every type the node and note
// roots reach, that read only the index. graphQLNoteFileFields are the ones
// that read note files on disk, which change before the watcher's write moves
// the generation. A request selecting any field outside graphQLIndexFields at
// any depth bypasses the cache, so a new field is never cached until someone
// classifies it here; TestGraphQLReadCacheClassifiesEveryReachableField fails
// until they do.
var graphQLIndexFields = map[string]bool{
	"__typename": true, "ref": true, "nodeId": true, "nodeKind": true, "path": true, "title": true,
	"resolvedType": true, "format": true, "sourceRepresentation": true, "evidenceRepresentation": true,
	"sourceCapabilities": true, "frontmatter": true, "tags": true, "score": true, "linked": true,
	"backlinked": true, "connected": true, "updatedAt": true, "issueCount": true, "id": true,
	"notePath": true, "fragment": true, "kind": true, "structural": true, "typeName": true,
	"items": true, "requestedRef": true, "node": true, "error": true, "pageInfo": true, "nodes": true,
	"warnings": true, "code": true, "message": true, "queryShapeVersion": true, "requestedFirst": true,
	"returnedCount": true, "truncated": true, "maxFirst": true, "input": true, "found": true,
	"candidates": true, "reason": true,
}

var graphQLNoteFileFields = map[string]bool{
	// Note and section bodies and the structure projected from them.
	"content": true, "children": true, "level": true,
	// Projections of the document: locators, workspace, and graphs that
	// scan section bodies for links.
	"locator": true, "workspace": true, "neighborhood": true, "localGraph": true,
	// Code nodes, which read code files.
	"language": true, "symbols": true, "symbol": true, "fqn": true, "signature": true, "docComment": true,
}

// selectionReadsNoteFiles reports whether set selects, at any depth, a field
// read from note files: a built-in field outside graphQLIndexFields, or an
// authored field the index does not hold (sections, ambient and reverse
// relations, and fields without an indexed query capability), mirroring the
// query layer's projection rule.
func selectionReadsNoteFiles(schema *ontoschema.Schema, doc *ast.QueryDocument, set ast.SelectionSet) bool {
	reads := false
	visitGraphQLFields(doc, set, func(field *ast.Field) {
		if reads {
			return
		}
		if authored := authoredGraphQLField(schema, field); authored != nil {
			if !authoredFieldIndexed(schema, authored) {
				reads = true
				return
			}
		} else if !graphQLIndexFields[field.Name] {
			reads = true
			return
		}
		reads = selectionReadsNoteFiles(schema, doc, field.SelectionSet)
	})
	return reads
}

// authoredGraphQLField returns the ontology field a selected field reads,
// when its parent is an authored type or interface.
func authoredGraphQLField(schema *ontoschema.Schema, field *ast.Field) *ontoschema.Field {
	if schema == nil || field.ObjectDefinition == nil {
		return nil
	}
	parent := field.ObjectDefinition.Name
	if noteType := schema.Types[parent]; noteType != nil {
		return noteType.ByName[field.Name]
	}
	if iface := schema.Interfaces[parent]; iface != nil {
		return iface.ByName[field.Name]
	}
	return nil
}

func authoredFieldIndexed(schema *ontoschema.Schema, field *ontoschema.Field) bool {
	switch field.Kind {
	case ontoschema.FieldKindScalar, ontoschema.FieldKindEnum, ontoschema.FieldKindLink:
		_, ok := ontoschema.FieldQueryCapabilityForField(schema, field)
		return ok
	}
	return false
}

// visitGraphQLFields calls visit for each field directly in set, looking
// through inline fragments and fragment spreads.
func visitGraphQLFields(doc *ast.QueryDocument, set ast.SelectionSet, visit func(*ast.Field)) {
	seen := map[string]bool{}
	var walk func(ast.SelectionSet)
	walk = func(set ast.SelectionSet) {
		for _, selection := range set {
			switch current := selection.(type) {
			case *ast.Field:
				visit(current)
			case *ast.InlineFragment:
				walk(current.SelectionSet)
			case *ast.FragmentSpread:
				if seen[current.Name] {
					continue
				}
				seen[current.Name] = true
				if fragment := doc.Fragments.ForName(current.Name); fragment != nil {
					walk(fragment.SelectionSet)
				}
			}
		}
	}
	walk(set)
}
