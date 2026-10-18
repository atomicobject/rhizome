package operations

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func renderNoteSummaryBlock(noteMgr obsidian.NoteReader, analysis *vaultgraph.GraphAnalysis, reverseNeighbors map[string][]string, notePath string, onto *ontologyTextContext) string {
	return renderNoteSummaryBlockWithContext(context.Background(), noteMgr, analysis, reverseNeighbors, notePath, onto)
}

func renderNoteSummaryBlockWithContext(ctx context.Context, noteMgr obsidian.NoteReader, analysis *vaultgraph.GraphAnalysis, reverseNeighbors map[string][]string, notePath string, onto *ontologyTextContext) string {
	if analysis == nil || len(analysis.Nodes) == 0 || notePath == "" {
		return ""
	}
	node, ok := analysis.Nodes[notePath]
	if !ok {
		return ""
	}

	var b strings.Builder
	title := strings.TrimSpace(node.Title)
	if title == "" {
		title = paths.NoteTitle(notePath)
	}
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString(" (note)\n\n")
	b.WriteString(fmt.Sprintf("- path: %s\n", notePath))
	summary := blessedSummary(noteMgr, notePath)
	if summary != "" {
		b.WriteString(fmt.Sprintf("- summary: %s\n", summary))
	}
	ontoBlock := renderNoteOntologyBlockWithContext(ctx, notePath, onto)
	if ontoBlock != "" {
		b.WriteString(ontoBlock)
	}
	b.WriteString(fmt.Sprintf("- hub: %.3f\n", node.Hub))
	b.WriteString(fmt.Sprintf("- authority: %.3f\n", node.Authority))
	b.WriteString(fmt.Sprintf("- inbound: %d\n", node.Inbound))
	b.WriteString(fmt.Sprintf("- outbound: %d\n", node.Outbound))

	// Top inbound/outbound neighbors by authority (very small, no backlink parsing).
	inTop := topNeighborsByAuthority(reverseNeighbors[notePath], analysis.Nodes, 5)
	outTop := topNeighborsByAuthority(node.Neighbors, analysis.Nodes, 5)
	if len(inTop) > 0 {
		b.WriteString("\n### Top inbound\n")
		for _, p := range inTop {
			b.WriteString("\n- ")
			b.WriteString(p)
		}
	}
	if len(outTop) > 0 {
		b.WriteString("\n\n### Top outbound\n")
		for _, p := range outTop {
			b.WriteString("\n- ")
			b.WriteString(p)
		}
	}
	if isThinNoteContext(node, ontoBlock) {
		b.WriteString(renderThinContextWarning(notePath))
	}

	return strings.TrimSpace(b.String())
}

func renderNoteSummaryBlockNoGraph(noteMgr obsidian.NoteReader, notePath string, onto *ontologyTextContext) string {
	return renderNoteSummaryBlockNoGraphWithContext(context.Background(), noteMgr, notePath, onto)
}

func renderNoteSummaryBlockNoGraphWithContext(ctx context.Context, noteMgr obsidian.NoteReader, notePath string, onto *ontologyTextContext) string {
	if notePath == "" {
		return ""
	}
	title := paths.NoteTitle(notePath)

	var b strings.Builder
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString(" (note)\n\n")
	b.WriteString(fmt.Sprintf("- path: %s\n", notePath))
	summary := blessedSummary(noteMgr, notePath)
	if summary != "" {
		b.WriteString(fmt.Sprintf("- summary: %s\n", summary))
	}
	ontoBlock := renderNoteOntologyBlockWithContext(ctx, notePath, onto)
	if ontoBlock != "" {
		b.WriteString(ontoBlock)
	}
	if ontoBlock == "" && summary == "" {
		b.WriteString(renderThinContextWarning(notePath))
	}
	return strings.TrimSpace(b.String())
}

func isThinNoteContext(node vaultgraph.GraphNode, ontologyBlock string) bool {
	return node.Inbound == 0 && node.Outbound == 0 && strings.TrimSpace(ontologyBlock) == ""
}

func isThinCodeContext(ctx FileContext) bool {
	return len(ctx.AncestorDocs) == 0 &&
		len(ctx.SubmoduleDocs) == 0 &&
		len(ctx.Rationale) == 0 &&
		len(ctx.OntologyNodes) == 0 &&
		len(ctx.NodeDiagnostics) == 0 &&
		len(ctx.LinkedNotes) == 0 &&
		len(ctx.ReturnedNotePaths) == 0 &&
		len(ctx.ReturnedDocPaths) == 0 &&
		len(ctx.AnchorMatches) == 0
}

func renderThinContextWarning(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "<path>"
	}
	quotedPath := shellQuoteArg(path)
	return fmt.Sprintf("\n\n#### Warnings\n- %s: insufficient contextual bindings for `%s`; this is not absence of docs. Next: `rzm agent files --include-content true --input %s`; for discovery use `rzm agent semantic-query --path %s --query \"<topic>\"`.", thinContextWarningCode, path, quotedPath, quotedPath)
}

func shellQuoteArg(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func renderNoteOntologyBlockWithContext(ctx context.Context, notePath string, onto *ontologyTextContext) string {
	if onto == nil || onto.Store == nil || onto.Schema == nil || onto.Scope == nil || notePath == "" {
		return ""
	}
	rows, err := onto.Scope.TypesByPaths(ctx, []string{notePath})
	if err != nil {
		return ""
	}
	row, ok := rows[notePath]
	if !ok {
		return ""
	}
	noteType := onto.Schema.Types[row.TypeName]
	if noteType == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n### Ontology\n")
	b.WriteString(fmt.Sprintf("\n- type: %s", row.TypeName))
	if noteType.Label != "" && noteType.Label != row.TypeName {
		b.WriteString(fmt.Sprintf("\n- label: %s", noteType.Label))
	}
	if noteType.KeyField != "" {
		b.WriteString(fmt.Sprintf("\n- keyField: %s", noteType.KeyField))
	}
	fields := make([]string, 0, len(noteType.Fields))
	for _, field := range noteType.Fields {
		name := field.Name
		if field.Required {
			name += "!"
		}
		if field.ContextInclude {
			name += "(ctx)"
		}
		fields = append(fields, name)
	}
	if len(fields) > 0 {
		b.WriteString(fmt.Sprintf("\n- fields: %s", strings.Join(fields, ", ")))
	}
	if desc := strings.TrimSpace(noteType.Description); desc != "" {
		b.WriteByte('\n')
		for _, line := range strings.Split(desc, "\n") {
			b.WriteString("\n> ")
			b.WriteString(line)
		}
	}

	edges, err := noteOntologyPresentationEdges(ctx, onto, notePath, nil, 24)
	if err != nil {
		b.WriteString(renderOntologyReadWarning(err))
		return b.String()
	}
	if len(edges) == 0 {
		return b.String()
	}

	type groupKey struct {
		Relation   string
		Provenance string
	}
	grouped := make(map[groupKey][]string)
	keys := make([]groupKey, 0)
	for _, edge := range edges {
		key := groupKey{Relation: edge.RelationName, Provenance: edge.Provenance}
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], edge.Target.NotePath)
	}
	if len(keys) == 0 {
		return b.String()
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Relation != keys[j].Relation {
			return keys[i].Relation < keys[j].Relation
		}
		return keys[i].Provenance < keys[j].Provenance
	})
	b.WriteString("\n\n### Ontology neighbors\n")
	for _, key := range keys {
		neighbors := grouped[key]
		sort.Strings(neighbors)
		marker := ""
		if field := noteType.ByName[key.Relation]; field != nil && field.ContextInclude {
			marker = " (ctx)"
		}
		b.WriteString(fmt.Sprintf("\n- %s%s [%s]: %s", key.Relation, marker, key.Provenance, strings.Join(neighbors, ", ")))
	}
	return b.String()
}

func stringBoolMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key, ok := range values {
		if ok && strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func renderNoteContextIncludesWithContext(ctx context.Context, noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, notePath string, onto *ontologyTextContext, budget int, tracker DedupeTracker) (string, []DedupeItem) {
	if onto == nil || onto.Scope == nil || onto.Schema == nil || noteMgr == nil || notePath == "" || budget < 400 {
		return "", nil
	}
	rows, err := onto.Scope.TypesByPaths(ctx, []string{notePath})
	if err != nil {
		return "", nil
	}
	row, ok := rows[notePath]
	if !ok {
		return "", nil
	}
	noteType := onto.Schema.Types[row.TypeName]
	if noteType == nil {
		return "", nil
	}
	ctxFields := make(map[string]bool)
	for _, field := range noteType.Fields {
		if field.ContextInclude {
			ctxFields[field.Name] = true
		}
	}
	if len(ctxFields) == 0 {
		return "", nil
	}
	edges, err := noteOntologyPresentationEdges(ctx, onto, notePath, stringBoolMapKeys(ctxFields), 64)
	if err != nil {
		return renderOntologyReadWarning(err), nil
	}
	if len(edges) == 0 {
		return "", nil
	}

	type target struct {
		path     string
		relation string
	}
	seen := make(map[string]bool)
	var targets []target
	for _, edge := range edges {
		if !ctxFields[edge.RelationName] {
			continue
		}
		targetPath := edge.Target.NotePath
		if targetPath == "" || targetPath == notePath || seen[targetPath] {
			continue
		}
		seen[targetPath] = true
		targets = append(targets, target{path: targetPath, relation: edge.RelationName})
	}
	if len(targets) == 0 {
		return "", nil
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultDef.BasePath())
	const perNoteCap = 8000
	remaining := budget
	blocks := make([]string, 0, len(targets))
	dedupe := renderedNoteDedupe{vaultPaths: vaultPaths, tracker: tracker, items: make([]DedupeItem, 0, len(targets))}
	for _, tgt := range targets {
		if remaining < 400 {
			break
		}
		raw, err := noteMgr.GetContents(vaultDef, tgt.path)
		if err != nil || strings.TrimSpace(raw) == "" {
			continue
		}
		content, truncated := contextpack.TrimMarkdown(raw, min(perNoteCap, remaining))
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		if !dedupe.include(tgt.path, content) {
			continue
		}
		source := "ontology:" + tgt.relation
		blocks = append(blocks, renderEmbeddedFileXMLWithMeta("note", tgt.path, "", source, content, truncated))
		remaining -= len(content) + 120
	}
	if len(blocks) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("\n\n#### Ontology context (ctx)\n")
	for _, block := range blocks {
		b.WriteString("\n\n")
		b.WriteString(block)
	}
	return b.String(), dedupe.items
}

func noteOntologyPresentationEdges(ctx context.Context, onto *ontologyTextContext, notePath string, relationNames []string, limit int) ([]noderead.NeighborhoodEdge, error) {
	if onto == nil || onto.Scope == nil || strings.TrimSpace(notePath) == "" {
		return nil, nil
	}
	result, err := onto.Scope.Edges(ctx, noderead.NeighborhoodRequest{
		Sources:           []ontology.NodeRef{{NotePath: notePath, Kind: ontology.NodeKindNote}},
		Direction:         noderead.TraversalDirectionOutbound,
		IncludeStructural: true, IncludeAmbient: true, RelationNames: relationNames, FirstPerSource: limit,
	})
	return result.Edges, err
}

func renderOntologyReadWarning(err error) string {
	return fmt.Sprintf("\n\n#### Warnings\n- ontology_read_failed: %s.\n", err)
}

func buildReverseNeighborsForText(nodes map[string]vaultgraph.GraphNode) map[string][]string {
	if len(nodes) == 0 {
		return nil
	}
	reverse := make(map[string][]string, len(nodes))
	for path := range nodes {
		reverse[path] = nil
	}
	for src, node := range nodes {
		for _, dst := range node.Neighbors {
			reverse[dst] = append(reverse[dst], src)
		}
	}
	for p := range reverse {
		sort.Strings(reverse[p])
	}
	return reverse
}

func topNeighborsByAuthority(paths []string, nodes map[string]vaultgraph.GraphNode, limit int) []string {
	if len(paths) == 0 || limit == 0 {
		return nil
	}
	type pr struct {
		path string
		val  float64
	}
	list := make([]pr, 0, len(paths))
	for _, p := range paths {
		if n, ok := nodes[p]; ok {
			list = append(list, pr{path: p, val: n.Authority})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].val == list[j].val {
			return list[i].path < list[j].path
		}
		return list[i].val > list[j].val
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.path)
	}
	return out
}

func normalizeAbsNotePath(absPath string, vaultPaths paths.VaultPaths) string {
	if vaultPaths.Root() == "" {
		return string(paths.EnsureNoteExtension(absPath, ".md"))
	}
	if rel, err := vaultPaths.RelNotePathStrict(paths.EnsureNoteExtension(absPath, ".md").String()); err == nil && rel.String() != "" {
		return rel.String()
	}
	return string(paths.EnsureNoteExtension(absPath, ".md"))
}

func noteDedupeKey(path string, vaultPaths paths.VaultPaths) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	normalized := path
	if filepath.IsAbs(path) {
		normalized = normalizeAbsNotePath(path, vaultPaths)
	} else {
		normalized = string(paths.EnsureNoteExtension(path, ".md"))
	}
	return "note:" + normalized
}
