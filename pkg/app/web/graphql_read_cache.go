package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/queryengine"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/paths"
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
// arguments, a bypassed root, or a caller-dependent field go through the read
// cache after readiness admission (SPEC-0136 R4).
func (s *Server) publicGraphQLBody(ctx context.Context, generation string, req OntologyQueryRequest) ([]byte, error) {
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
	run := func(ctx context.Context) ([]byte, bool, error) {
		deps, err := s.ontologyQueryDepsForExactPaths(ontology.NewService(store, defs.schema), nil, exactSnapshot)
		if err != nil {
			return nil, false, err
		}
		engine, err := queryengine.New(defs.schema, defs.exec, deps)
		if err != nil {
			return nil, false, err
		}
		result, err := engine.Run(ctx, prepared)
		if err != nil {
			return nil, false, err
		}
		body, err := graphQLResponseBody(result, generation)
		if err != nil {
			return nil, false, err
		}
		return body, graphQLResultStorable(result) && s.readStillCurrent(ctx, generation), nil
	}
	if graphQLReadBypassed(defs.exec, prepared) {
		s.reads.bypass(queryHash)
		body, _, err := run(ctx)
		return body, err
	}
	variables, err := json.Marshal(prepared.Variables)
	if err != nil {
		return nil, err
	}
	key := generation + "|" + s.namedSourceWitness(prepared) + "|" + readQueryHash("graphql", req.Query, strings.TrimSpace(req.OperationName), string(variables))
	return s.reads.do(ctx, key, queryHash, run)
}

// namedSourceWitness stamps the source file of every note a root names by
// path or ref. Exact reads check their source on disk, which changes before
// the watcher's store write advances the generation; with the stamp in the
// key, a deleted or edited note misses at once. Roots that select by type or
// filter read only the index and rely on the generation alone.
func (s *Server) namedSourceWitness(prepared *ontologyquery.PreparedQuery) string {
	if prepared.Introspection {
		return ""
	}
	var notePaths []string
	addRef := func(value any) {
		raw, _ := value.(string)
		if hash := strings.IndexByte(raw, '#'); hash >= 0 {
			raw = raw[:hash]
		}
		if clean, err := paths.CleanNotePath(strings.TrimSpace(raw)); err == nil && clean.String() != "" {
			notePaths = append(notePaths, clean.String())
		}
	}
	visitGraphQLFields(prepared.Document, prepared.Operation.SelectionSet, func(root *ast.Field) {
		args := root.ArgumentMap(prepared.Variables)
		switch root.Name {
		case "node", "resolve":
			addRef(args["ref"])
		case "note":
			addRef(args["path"])
			addRef(args["ref"])
		case "nodes":
			refs, _ := args["refs"].([]any)
			for _, ref := range refs {
				addRef(ref)
			}
		}
	})
	if len(notePaths) == 0 {
		return ""
	}
	sort.Strings(notePaths)
	hash := fnv.New64a()
	for _, notePath := range notePaths {
		info, err := os.Stat(filepath.Join(s.cfg.VaultPath, filepath.FromSlash(notePath)))
		if err != nil {
			fmt.Fprintf(hash, "%s|missing\n", notePath)
			continue
		}
		fmt.Fprintf(hash, "%s|%d|%d\n", notePath, info.ModTime().UnixNano(), info.Size())
	}
	return fmt.Sprintf("%x", hash.Sum64())
}

// readStillCurrent reports whether a body read at generation may be stored:
// the generation did not change during execution and the index is ready, so
// a startup read cannot outlive the readiness transition.
func (s *Server) readStillCurrent(ctx context.Context, generation string) bool {
	return s.runtime.IndexReady() && s.IndexGeneration(ctx) == generation
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
func graphQLReadBypassed(exec *ontologyquery.ExecutableSchema, prepared *ontologyquery.PreparedQuery) bool {
	if prepared.UsesSemantic || prepared.UsesSearch {
		return true
	}
	if prepared.Introspection {
		return false
	}
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
		case !graphQLCoveredRoots[root.Name] && exec.RootTypes[root.Name] == "" && root.Name != "__typename":
			// A root this file does not classify is never cached.
			bypassed = true
		}
	})
	return bypassed
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
