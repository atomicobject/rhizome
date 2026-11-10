package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/vektah/gqlparser/v2/ast"
)

// namedSourceWitness stamps the source file of every note a root names by
// path or ref. Exact reads check their source on disk, which changes before
// the watcher's store write advances the generation; with the stamp in the
// key, a deleted or edited note misses at once. Each ref is resolved to its
// canonical note path the way execution resolves it (a basename, an alias, or
// an identifier names some other file), and the raw path is stamped as well,
// so a note created at that path also misses. Roots that select by type or
// filter read only the index and rely on the generation alone.
func (s *Server) namedSourceWitness(ctx context.Context, generation string, prepared *ontologyquery.PreparedQuery, resolve refResolver) (string, error) {
	refs := namedRefs(prepared)
	if len(refs) == 0 {
		return "", nil
	}
	canonical, err := s.refPaths.resolve(ctx, generation, refs, resolve)
	if err != nil {
		return "", err
	}
	notePaths := map[string]bool{}
	for _, ref := range refs {
		if clean, err := paths.CleanNotePath(ref); err == nil && clean.String() != "" {
			notePaths[clean.String()] = true
		}
		for _, notePath := range canonical[ref] {
			notePaths[notePath] = true
		}
	}
	sorted := make([]string, 0, len(notePaths))
	for notePath := range notePaths {
		sorted = append(sorted, notePath)
	}
	sort.Strings(sorted)
	var witness strings.Builder
	for _, notePath := range sorted {
		info, err := os.Stat(filepath.Join(s.cfg.VaultPath, filepath.FromSlash(notePath)))
		if err != nil {
			fmt.Fprintf(&witness, "%s|missing\n", notePath)
			continue
		}
		fmt.Fprintf(&witness, "%s|%d|%d\n", notePath, info.ModTime().UnixNano(), info.Size())
	}
	return witness.String(), nil
}

// namedRefs lists, without fragments, the refs and paths the operation's
// node, resolve, note, and nodes roots name.
func namedRefs(prepared *ontologyquery.PreparedQuery) []string {
	if prepared.Introspection {
		return nil
	}
	seen := map[string]bool{}
	var refs []string
	add := func(value any) {
		raw, _ := value.(string)
		if hash := strings.IndexByte(raw, '#'); hash >= 0 {
			raw = raw[:hash]
		}
		raw = strings.TrimSpace(raw)
		if raw != "" && !seen[raw] {
			seen[raw] = true
			refs = append(refs, raw)
		}
	}
	visitGraphQLFields(prepared.Document, prepared.Operation.SelectionSet, func(root *ast.Field) {
		args := root.ArgumentMap(prepared.Variables)
		switch root.Name {
		case "node", "resolve":
			add(args["ref"])
		case "note":
			add(args["path"])
			add(args["ref"])
		case "nodes":
			list, _ := args["refs"].([]any)
			for _, ref := range list {
				add(ref)
			}
		}
	})
	sort.Strings(refs)
	return refs
}

// refResolver returns the note paths each ref resolves to: one for a unique
// match, every candidate for an ambiguous one, none when nothing matches.
type refResolver func(ctx context.Context, refs []string) (map[string][]string, error)

// refPathMemo remembers, for the current generation, the note paths refs
// resolved to. Resolution reads only the index, which the generation covers;
// the witness stats the resolved files on every request. A ref that resolved
// to nothing is never remembered, because a note created on disk can resolve
// it before the generation moves.
type refPathMemo struct {
	mu         sync.Mutex
	generation string
	paths      map[string][]string
}

// refPathMemoLimit bounds the memo; it starts over when full.
const refPathMemoLimit = 4096

func (m *refPathMemo) resolve(ctx context.Context, generation string, refs []string, resolve refResolver) (map[string][]string, error) {
	out := make(map[string][]string, len(refs))
	var missing []string
	m.mu.Lock()
	if m.generation != generation {
		m.generation, m.paths = generation, map[string][]string{}
	}
	for _, ref := range refs {
		if notePaths, ok := m.paths[ref]; ok {
			out[ref] = notePaths
		} else {
			missing = append(missing, ref)
		}
	}
	m.mu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	resolved, err := resolve(ctx, missing)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generation == generation && len(m.paths)+len(missing) > refPathMemoLimit {
		m.paths = map[string][]string{}
	}
	for _, ref := range missing {
		notePaths := resolved[ref]
		out[ref] = notePaths
		if m.generation == generation && len(notePaths) > 0 {
			m.paths[ref] = notePaths
		}
	}
	return out, nil
}

// resolveRefsWith resolves refs through the query engine's own resolve root,
// so the witness stamps the files execution reads.
func resolveRefsWith(run func(ctx context.Context, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error), exec *ontologyquery.ExecutableSchema) refResolver {
	return func(ctx context.Context, refs []string) (map[string][]string, error) {
		var query strings.Builder
		variables := make(map[string]any, len(refs))
		query.WriteString("query(")
		for i := range refs {
			fmt.Fprintf(&query, "$r%d: String!,", i)
		}
		query.WriteString(") {")
		for i, ref := range refs {
			fmt.Fprintf(&query, " r%d: resolve(ref: $r%d) { ref { notePath } candidates { notePath } }", i, i)
			variables[fmt.Sprintf("r%d", i)] = ref
		}
		query.WriteString(" }")
		prepared, errs := ontologyquery.PrepareWithOptions(exec, query.String(), ontologyquery.PrepareOptions{Variables: variables})
		if len(errs) > 0 {
			return nil, fmt.Errorf("resolve named refs: %s", errs[0].Message)
		}
		result, err := run(ctx, prepared)
		if err != nil {
			return nil, err
		}
		if len(result.Errors) > 0 {
			return nil, fmt.Errorf("resolve named refs: %s", result.Errors[0].Message)
		}
		out := make(map[string][]string, len(refs))
		for i, ref := range refs {
			resolution, _ := result.Data[fmt.Sprintf("r%d", i)].(map[string]any)
			var notePaths []string
			addPath := func(value any) {
				node, _ := value.(map[string]any)
				if notePath, _ := node["notePath"].(string); notePath != "" {
					notePaths = append(notePaths, notePath)
				}
			}
			addPath(resolution["ref"])
			candidates, _ := resolution["candidates"].([]any)
			for _, candidate := range candidates {
				addPath(candidate)
			}
			out[ref] = notePaths
		}
		return out, nil
	}
}
