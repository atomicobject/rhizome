package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
// the generation. Both are keyed by graphQLFieldKey, so a field on a new type
// is never cached because an index field elsewhere shares its name. A request
// selecting any field outside graphQLIndexFields at any depth bypasses the
// cache; TestGraphQLReadCacheClassifiesEveryReachableField fails until a new
// field is classified here.
var graphQLIndexFields = graphQLFieldKeys(map[string][]string{
	"Node":              {"nodeId", "nodeKind", "path", "ref", "resolvedType", "title"},
	"Note":              {"backlinked", "connected", "evidenceRepresentation", "format", "frontmatter", "issueCount", "linked", "nodeId", "nodeKind", "path", "ref", "resolvedType", "score", "sourceCapabilities", "sourceRepresentation", "tags", "title", "updatedAt"},
	"NoteNode":          {"backlinked", "connected", "evidenceRepresentation", "format", "frontmatter", "issueCount", "linked", "nodeId", "nodeKind", "path", "ref", "resolvedType", "score", "sourceCapabilities", "sourceRepresentation", "tags", "title", "updatedAt"},
	"Section":           {"id", "issueCount", "nodeId", "nodeKind", "notePath", "path", "ref", "resolvedType", "title", "updatedAt"},
	"CodeFile":          {"nodeId", "nodeKind", "path", "ref", "resolvedType", "title"},
	"CodeSymbol":        {"nodeId", "nodeKind", "path", "ref", "resolvedType", "title"},
	"NodeRef":           {"fragment", "kind", "nodeId", "notePath", "path", "ref", "structural", "typeName"},
	"NodeRefResolution": {"candidates", "found", "input", "kind", "path", "reason", "ref", "resolvedType", "title"},
	"NodeBatchResult":   {"items", "pageInfo"},
	"NodeBatchItem":     {"error", "node", "ref", "requestedRef"},
	"NoteConnection":    {"nodes", "pageInfo", "warnings"},
	"PageInfo":          {"maxFirst", "queryShapeVersion", "requestedFirst", "returnedCount", "truncated"},
	"RuntimeWarning":    {"code", "message", "path"},
})

var graphQLNoteFileFields = graphQLFieldKeys(map[string][]string{
	// Note and section bodies and the structure projected from them, the
	// locators, workspace, and graphs that scan section bodies for links.
	"Node":     {"localGraph", "locator", "neighborhood", "workspace"},
	"Note":     {"content", "localGraph", "locator", "neighborhood", "workspace"},
	"NoteNode": {"content", "localGraph", "locator", "neighborhood", "workspace"},
	"Section":  {"children", "content", "level", "localGraph", "locator", "neighborhood", "workspace"},
	// Code nodes, which read code files.
	"CodeFile":   {"language", "localGraph", "locator", "neighborhood", "symbols", "workspace"},
	"CodeSymbol": {"docComment", "fqn", "language", "localGraph", "locator", "neighborhood", "signature", "symbol", "workspace"},
})

func graphQLFieldKeys(fields map[string][]string) map[string]bool {
	keys := map[string]bool{}
	for owner, names := range fields {
		for _, name := range names {
			keys[owner+"."+name] = true
		}
	}
	return keys
}

// graphQLFieldKey keys a built-in field by its parent type, or, on a type that
// implements NoteNode or Section (every authored type does), by that
// interface, since authored type names come from each vault's schema.
func graphQLFieldKey(parent *ast.Definition, name string) string {
	if parent == nil {
		return ""
	}
	owner := parent.Name
	for _, iface := range parent.Interfaces {
		if iface == "NoteNode" || iface == "Section" {
			owner = iface
			break
		}
	}
	return owner + "." + name
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
		} else if field.Name != "__typename" && !graphQLIndexFields[graphQLFieldKey(field.ObjectDefinition, field.Name)] {
			reads = true
			return
		}
		reads = selectionReadsNoteFiles(schema, doc, field.SelectionSet)
	})
	return reads
}

// authoredGraphQLField returns the ontology field a selected field reads,
// when its parent is an authored type or interface and the executor resolves
// the authored field: a note node resolves some built-in names first, even
// when its type declares a field of the same name.
func authoredGraphQLField(schema *ontoschema.Schema, field *ast.Field) *ontoschema.Field {
	if schema == nil || field.ObjectDefinition == nil {
		return nil
	}
	if isNoteDefinition(field.ObjectDefinition) && ontologyquery.NoteBuiltinFieldPrecedes(field.Name) {
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

// isNoteDefinition reports whether a field's parent may be a note node, so a
// note's built-in fields can take precedence over authored ones. The executor
// resolves by the concrete node, and an interface or union's possible types
// can include notes even when it declares neither note interface, so any
// abstract parent counts: classifying it as a note only ever bypasses the
// cache, never caches a disk read.
func isNoteDefinition(def *ast.Definition) bool {
	if def.Kind == ast.Interface || def.Kind == ast.Union {
		return true
	}
	return slices.Contains(def.Interfaces, "NoteNode") || slices.Contains(def.Interfaces, "Note")
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
