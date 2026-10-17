package mcp

import (
	"context"
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"sort"
	"strings"
)

const (
	filesGraphMaxNodes         = 1500
	filesGraphMaxEdges         = 5000
	filesGraphPerNodeEdgeLimit = 200
)

func filesHasPathInputs(inputs []actions.ListInput) bool {
	for _, input := range inputs {
		switch input.Type {
		case actions.InputTypeFile, actions.InputTypeFind:
			return true
		}
	}
	return false
}

func indexedFilePathsForPrefixes(ctx context.Context, store *semdb.Store, prefixes []string) ([]string, error) {
	if store == nil || len(prefixes) == 0 {
		return nil, nil
	}
	seenPrefix := make(map[string]struct{}, len(prefixes))
	var uniquePrefixes []string
	for _, p := range prefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seenPrefix[p]; ok {
			continue
		}
		seenPrefix[p] = struct{}{}
		uniquePrefixes = append(uniquePrefixes, p)
	}
	if len(uniquePrefixes) == 0 {
		return nil, nil
	}
	seenPath := make(map[string]struct{})
	var out []string
	for _, prefix := range uniquePrefixes {
		paths, err := store.IndexedFilePathsByPrefix(ctx, prefix)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if _, ok := seenPath[path]; ok {
				continue
			}
			seenPath[path] = struct{}{}
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

func expandFilesWithGraph(ctx context.Context, store *semdb.Store, vaultPaths paths.VaultPaths, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, linkOptions markdownsyntax.WikilinkOptions, seeds []string, maxDepth int, unique map[string]bool, order *[]string, fileTypes map[string]string) error {
	if store == nil || maxDepth <= 0 || len(seeds) == 0 {
		return nil
	}

	seedSet := make(map[string]struct{}, len(seeds))
	frontier := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		seed = normalizeDocPath(vaultPaths, seed)
		if seed == "" {
			continue
		}
		if _, ok := seedSet[seed]; ok {
			continue
		}
		seedSet[seed] = struct{}{}
		frontier = append(frontier, seed)
	}
	if len(frontier) == 0 {
		return nil
	}
	sort.Strings(frontier)

	edgeCount := 0
	scope := noderead.NewService(vaultDef, note, store, nil).NewScope(noderead.ScopeOptions{})
	for depth := 0; depth < maxDepth; depth++ {
		if len(frontier) == 0 {
			break
		}
		if len(unique) >= filesGraphMaxNodes || edgeCount >= filesGraphMaxEdges {
			break
		}

		frontierSet := make(map[string]struct{}, len(frontier))
		for _, node := range frontier {
			frontierSet[node] = struct{}{}
		}

		next := make([]string, 0, len(frontier)*2)

		edgesLimit := filesGraphPerNodeEdgeLimit * len(frontier)
		if edgesLimit <= 0 {
			edgesLimit = 500
		}
		if remaining := filesGraphMaxEdges - edgeCount; remaining > 0 && edgesLimit > remaining {
			edgesLimit = remaining
		}

		facts, err := scope.GraphFacts(ctx, noderead.GraphFactsRequest{
			Paths:           frontier,
			NodeLimit:       filesGraphMaxNodes,
			EdgeLimit:       edgesLimit,
			IncludeOntology: true,
			IncludeDocLinks: true,
			IncludeCode:     true,
			IncludeEmbedded: true,
		})
		if err != nil {
			return fmt.Errorf("graph facts for files: %w", err)
		}

		perNodeCounts := make(map[string]int, len(frontier))

		for _, edge := range facts.Edges {
			if len(unique) >= filesGraphMaxNodes || edgeCount >= filesGraphMaxEdges {
				break
			}
			src, srcType, srcOK := filesGraphEndpointPath(vaultPaths, edge.SourceKind, edge.SourcePath)
			dst, dstType, dstOK := filesGraphEndpointPath(vaultPaths, edge.TargetKind, edge.TargetPath)
			if !srcOK || !dstOK {
				continue
			}
			if src == "" || dst == "" || src == dst {
				continue
			}

			if !filesGraphFactIncluded(edge) {
				continue
			}

			added := false
			if _, ok := frontierSet[src]; ok {
				if perNodeCounts[src] < filesGraphPerNodeEdgeLimit {
					perNodeCounts[src]++
					added = true
				}
			}
			if _, ok := frontierSet[dst]; ok {
				if perNodeCounts[dst] < filesGraphPerNodeEdgeLimit {
					perNodeCounts[dst]++
					added = true
				}
			}
			if !added {
				continue
			}
			edgeCount++

			for _, endpoint := range []struct {
				path     string
				fileType string
			}{{path: src, fileType: srcType}, {path: dst, fileType: dstType}} {
				node := endpoint.path
				if node == "" {
					continue
				}
				if _, ok := unique[node]; ok {
					continue
				}
				if len(unique) >= filesGraphMaxNodes {
					break
				}
				unique[node] = true
				*order = append(*order, node)
				if _, ok := fileTypes[node]; !ok {
					fileTypes[node] = endpoint.fileType
				}
				next = append(next, node)
			}
		}

		if len(next) == 0 {
			break
		}
		sort.Strings(next)
		frontier = next
	}

	return nil
}

func filesGraphEndpointPath(vaultPaths paths.VaultPaths, kind noderead.GraphEndpointKind, path string) (string, string, bool) {
	path = normalizeDocPath(vaultPaths, path)
	if path == "" {
		return "", "", false
	}
	switch kind {
	case noderead.GraphEndpointNote, noderead.GraphEndpointEmbedded, noderead.GraphEndpointSection:
		return path, fileTypeNote, true
	case noderead.GraphEndpointCode:
		return path, fileTypeCode, true
	default:
		return "", "", false
	}
}

func filesGraphFactIncluded(edge noderead.GraphFactEdge) bool {
	kind := strings.ToLower(strings.TrimSpace(edge.Kind))
	switch kind {
	case "wikilink", "mdlink", "mentions", "coderef", "ontology":
		return true
	case "embeds":
		return false
	default:
		return strings.HasPrefix(kind, "note_link:")
	}
}
