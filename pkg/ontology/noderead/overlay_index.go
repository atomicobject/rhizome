package noderead

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// BuildReadOverlayIndex projects touched preview documents once into an
// immutable read index that can be reused across request scopes.
func BuildReadOverlayIndex(ctx context.Context, schema *ontology.Schema, overlay *ReadOverlay) (*ReadOverlayIndex, error) {
	if schema == nil || overlay == nil || overlay.Empty() {
		return nil, nil
	}
	index := newReadOverlayIndex()
	index.collectOverlayPaths(overlay)
	index.BuildTouchedPathCount = len(index.TouchedPaths)

	paths := make([]string, 0, len(index.TouchedPaths))
	for path := range index.TouchedPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := index.addOverlayPath(ctx, schema, overlay, path); err != nil {
			return nil, err
		}
	}
	for typeName := range index.ItemsByType {
		sortNodeListItems(index.ItemsByType[typeName])
	}
	index.BuildOverlayRecordKeys = make([]string, 0, len(index.RecordsByRef))
	for key := range index.RecordsByRef {
		index.BuildOverlayRecordKeys = append(index.BuildOverlayRecordKeys, key)
	}
	sort.Strings(index.BuildOverlayRecordKeys)
	return index, nil
}

// BuildReadOverlayIndexReplacingPaths reuses an existing index and reprojects
// only the requested touched paths. It is the edit-session v1 incremental path
// for simple property/link changes; callers fall back to BuildReadOverlayIndex
// when they cannot prove the changed paths.
func BuildReadOverlayIndexReplacingPaths(ctx context.Context, schema *ontology.Schema, overlay *ReadOverlay, previous *ReadOverlayIndex, replacePaths []string) (*ReadOverlayIndex, error) {
	if previous == nil || len(replacePaths) == 0 {
		return BuildReadOverlayIndex(ctx, schema, overlay)
	}
	replace := map[string]struct{}{}
	for _, path := range replacePaths {
		if path = strings.TrimSpace(path); path != "" {
			replace[path] = struct{}{}
		}
	}
	if len(replace) == 0 {
		return previous, nil
	}
	index := newReadOverlayIndex()
	index.collectOverlayPaths(overlay)
	index.BuildTouchedPathCount = len(index.TouchedPaths)
	for path, snapshot := range previous.SnapshotsByPath {
		if _, ok := replace[path]; !ok {
			index.SnapshotsByPath[path] = snapshot
		}
	}
	for key, record := range previous.RecordsByRef {
		if _, ok := replace[record.Ref.NotePath]; ok {
			continue
		}
		index.RecordsByRef[key] = cloneNodeRecord(record)
		if values := previous.FieldValuesByRef[key]; len(values) > 0 {
			index.FieldValuesByRef[key] = cloneStringSliceMap(values)
		}
		index.ProjectionCountByPath[record.Ref.NotePath] = previous.ProjectionCountByPath[record.Ref.NotePath]
	}
	for typeName, items := range previous.ItemsByType {
		for _, item := range items {
			if _, ok := replace[item.NotePath]; ok {
				continue
			}
			index.ItemsByType[typeName] = append(index.ItemsByType[typeName], item)
		}
	}
	for path, keys := range previous.ItemKeysByTouchedPath {
		if _, ok := replace[path]; !ok {
			index.ItemKeysByTouchedPath[path] = cloneStringSet(keys)
		}
	}
	paths := make([]string, 0, len(replace))
	for path := range replace {
		if _, touched := index.TouchedPaths[path]; !touched {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := index.addOverlayPath(ctx, schema, overlay, path); err != nil {
			return nil, err
		}
	}
	for typeName := range index.ItemsByType {
		sortNodeListItems(index.ItemsByType[typeName])
	}
	for key := range index.RecordsByRef {
		index.BuildOverlayRecordKeys = append(index.BuildOverlayRecordKeys, key)
	}
	sort.Strings(index.BuildOverlayRecordKeys)
	return index, nil
}

func newReadOverlayIndex() *ReadOverlayIndex {
	return &ReadOverlayIndex{
		TouchedPaths:          map[string]struct{}{},
		DeletedPaths:          map[string]struct{}{},
		SnapshotsByPath:       map[string]*ontology.DocumentSnapshot{},
		RecordsByRef:          map[string]NodeRecord{},
		ItemsByType:           map[string][]ontology.NodeListItem{},
		ItemKeysByTouchedPath: map[string]map[string]struct{}{},
		FieldValuesByRef:      map[string]map[string][]string{},
		ProjectionCountByPath: map[string]int{},
	}
}

func (index *ReadOverlayIndex) collectOverlayPaths(overlay *ReadOverlay) {
	for path := range overlay.UpdatedContentByPath {
		if path = strings.TrimSpace(path); path != "" {
			index.TouchedPaths[path] = struct{}{}
		}
	}
	for path, note := range overlay.Notes {
		if path = strings.TrimSpace(path); path != "" {
			index.TouchedPaths[path] = struct{}{}
			if note.Deleted {
				index.DeletedPaths[path] = struct{}{}
			}
		}
	}
	for _, path := range overlay.TouchedPaths {
		if path = strings.TrimSpace(path); path != "" {
			index.TouchedPaths[path] = struct{}{}
		}
	}
}

func (index *ReadOverlayIndex) addOverlayPath(ctx context.Context, schema *ontology.Schema, overlay *ReadOverlay, path string) error {
	_ = ctx
	if !overlay.IsMarkdownSource() {
		return nil
	}
	if _, deleted := index.DeletedPaths[path]; deleted {
		return nil
	}
	content, updatedAt, ok := overlayContentForIndex(overlay, path)
	if !ok {
		return nil
	}
	snapshot, err := ontology.BuildDocumentSnapshot(path, content, time.Unix(updatedAt, 0))
	if err != nil {
		return err
	}
	index.SnapshotsByPath[path] = snapshot
	root, err := ontology.ProjectNodeFromSnapshot(snapshot, schema, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
	if err != nil || root == nil {
		return nil
	}
	if fallback, ok := ontology.AsFallbackNoteProjection(root); ok {
		root = fallback
	}
	index.addProjection(schema, root, updatedAt)
	globalRefs, err := ontology.GlobalSourceNodeRefsFromSnapshot(snapshot, schema)
	if err != nil {
		return nil
	}
	for _, ref := range globalRefs {
		projection, err := ontology.ProjectBoundNodeFromSnapshot(snapshot, schema, ref)
		if err != nil || projection == nil {
			continue
		}
		index.addProjection(schema, projection, updatedAt)
	}
	return nil
}

func overlayContentForIndex(overlay *ReadOverlay, path string) (string, int64, bool) {
	if note, ok := overlay.Notes[path]; ok {
		if note.Deleted {
			return "", note.UpdatedAt, false
		}
		return note.Content, note.UpdatedAt, true
	}
	if content, ok := overlay.UpdatedContentByPath[path]; ok {
		return content, 0, true
	}
	return "", 0, false
}

func (index *ReadOverlayIndex) addProjection(schema *ontology.Schema, projection *ontology.NodeProjection, updatedAt int64) {
	if projection == nil {
		return
	}
	record := nodeRecordFromOverlayProjection(schema, projection, updatedAt)
	key := nodeRefIdentityKey(record.Ref)
	if key == "" {
		return
	}
	if _, seen := index.RecordsByRef[key]; seen {
		return
	}
	index.RecordsByRef[key] = record
	index.FieldValuesByRef[key] = overlayRecordFieldValues(record)
	index.BuildProjectionCount++
	index.ProjectionCountByPath[record.Ref.NotePath]++
	index.addItemForTypes(schema, record)

	names := make([]string, 0, len(projection.Fields))
	for name := range projection.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, ref := range projection.Fields[name].SectionNodes {
			child, err := ontology.ProjectBoundNodeFromSnapshot(projection.Snapshot, schema, ref)
			if err != nil || child == nil {
				continue
			}
			index.addProjection(schema, child, updatedAt)
		}
	}
}

func (index *ReadOverlayIndex) addItemForTypes(schema *ontology.Schema, record NodeRecord) {
	item := ontology.NodeListItem{
		Ref:          record.Ref,
		Title:        record.Title,
		ResolvedType: record.TypeName,
		NotePath:     record.Ref.NotePath,
		UpdatedAt:    record.UpdatedAt,
		HasIssues:    record.HasIssues,
	}
	types := []string{record.TypeName}
	if schema != nil {
		if noteType := schema.Types[record.TypeName]; noteType != nil {
			types = append(types, noteType.Implements...)
		}
	}
	for _, typeName := range types {
		typeName = strings.TrimSpace(typeName)
		if typeName == "" {
			continue
		}
		index.ItemsByType[typeName] = append(index.ItemsByType[typeName], item)
		if index.ItemKeysByTouchedPath[record.Ref.NotePath] == nil {
			index.ItemKeysByTouchedPath[record.Ref.NotePath] = map[string]struct{}{}
		}
		index.ItemKeysByTouchedPath[record.Ref.NotePath][nodeListItemKey(item)] = struct{}{}
	}
}

func nodeRecordFromOverlayProjection(schema *ontology.Schema, projection *ontology.NodeProjection, updatedAt int64) NodeRecord {
	title := projection.Ref.String()
	if workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(schema, projection); workspace != nil {
		title = workspace.Node.Title
	}
	content := ""
	if projection.Ref.StartByte >= 0 && projection.Ref.EndByte > projection.Ref.StartByte && projection.Ref.EndByte <= len(projection.Snapshot.Content) {
		content = strings.TrimSpace(projection.Snapshot.Content[projection.Ref.StartByte:projection.Ref.EndByte])
	}
	return NodeRecord{
		Ref:             projection.Ref,
		Path:            projection.Ref.NotePath,
		Title:           title,
		Content:         content,
		Frontmatter:     frontmatterWithTemporalText(projection.Snapshot.Frontmatter),
		InlineProps:     projectionInlineProps(projection),
		FieldValues:     nil,
		projectedFields: captureProjectedFieldValues(projection),
		TypeName:        projection.ResolvedType,
		UpdatedAt:       updatedAt,
	}
}

func overlayRecordFieldValues(record NodeRecord) map[string][]string {
	out := map[string][]string{}
	for key, values := range record.InlineProps {
		out[key] = append([]string(nil), values...)
		out["inline."+key] = append([]string(nil), values...)
	}
	for key, value := range record.Frontmatter {
		values := anyFieldValues(value)
		if len(values) == 0 {
			continue
		}
		out[key] = append([]string(nil), values...)
		out["frontmatter."+key] = append([]string(nil), values...)
	}
	if record.Title != "" {
		out["title"] = []string{record.Title}
	}
	if record.TypeName != "" {
		out["resolvedType"] = []string{record.TypeName}
		out["type"] = []string{record.TypeName}
	}
	if record.Ref.NotePath != "" {
		out["path"] = []string{record.Ref.NotePath}
		out["notePath"] = []string{record.Ref.NotePath}
	}
	return out
}

func cloneStringSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}

func cloneStringSliceMap(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}
