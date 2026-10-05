package query

import (
	"context"
	"regexp"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/vektah/gqlparser/v2/ast"
)

const workspaceBodiesMax = 200

var workspaceBodyPreviewPlaceholder = regexp.MustCompile(`{{\s*([A-Za-z_][A-Za-z0-9_]*)\s*}}`)

// workspaceBodyNode is the source-preserving transport record from which a
// client can construct its own workspace graph. It deliberately contains no
// browser-pane or graph-view state.
type workspaceBodyNode struct {
	Projection *ontology.NodeProjection
	Workspace  *ontology.NodeWorkspace
	Binding    *workspaceBodyBinding
	Blocks     []ontology.NodeBodyBlock
	// Parent overrides the semantic parent for an undeclared heading, which
	// belongs to the body that contains it rather than to the note.
	Parent *ontology.NodeRef
}

type workspaceBodyBinding struct {
	TypeName        string
	FieldName       string
	FieldPath       string
	FieldList       bool
	SectionDisplay  ontology.SectionDisplay
	Properties      map[string]string
	IdentifierField string
	PreviewTemplate string
	Collapsed       bool
}

func (e *executor) resolveWorkspaceBodies(bodies []workspaceBodyNode, links fieldLinkTargets, field *ast.Field, path []string) []any {
	out := make([]any, 0, len(bodies))
	for _, body := range bodies {
		out = append(out, e.resolveWorkspaceBodyNode(body, links, field.SelectionSet, path))
	}
	return out
}

func workspaceBodiesLimit(args map[string]any) int {
	limit := workspaceBodiesMax
	if value, ok := intArg(args["first"]); ok {
		if value <= 0 {
			return 0
		}
		if value < limit {
			limit = value
		}
	}
	return limit
}

func (e *executor) workspaceBodyNodes(ctx context.Context, root *ontology.NodeProjection, limit int, path []string) []workspaceBodyNode {
	if e == nil || e.loaders == nil || e.loaders.scope == nil || root == nil || limit <= 0 {
		return nil
	}

	type pendingBody struct {
		projection    *ontology.NodeProjection
		binding       *workspaceBodyBinding
		includeBlocks bool
		parent        *ontology.NodeRef
	}
	// Scope.Projection shares one snapshot per note and memoizes canonical refs.
	// The explicit response cap keeps metadata-only collection descendants from
	// becoming an unbounded structural walk.
	queue := []pendingBody{{projection: root, includeBlocks: true}}
	seen := make(map[string]struct{}, limit)
	out := make([]workspaceBodyNode, 0, limit)

	for len(queue) > 0 && len(out) < limit {
		current := queue[0]
		queue = queue[1:]
		if current.projection == nil {
			continue
		}
		key := workspaceBodyRefKey(current.projection.Ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(e.schema, current.projection)
		if workspace == nil {
			continue
		}
		allBlocks := ontology.BuildNodeBody(current.projection, e.schema)
		blocks := []ontology.NodeBodyBlock(nil)
		if current.includeBlocks {
			blocks = allBlocks
		}
		out = append(out, workspaceBodyNode{
			Projection: current.projection,
			Workspace:  workspace,
			Binding:    current.binding,
			Blocks:     blocks,
			Parent:     current.parent,
		})

		for _, block := range allBlocks {
			for _, child := range workspaceBodyBlockRefs(block) {
				childProjection, err := e.loaders.scope.Projection(ctx, child)
				if err != nil {
					e.addError(path, err.Error())
					continue
				}
				if childProjection == nil {
					continue
				}
				pending := pendingBody{
					projection:    childProjection,
					binding:       workspaceBodyChildBinding(current.binding, block, childProjection),
					includeBlocks: current.includeBlocks && block.RendersInline(),
				}
				if block.Undeclared() {
					parent := current.projection.Ref
					pending.parent = &parent
				}
				queue = append(queue, pending)
			}
		}
	}
	return out
}

func workspaceBodyBlockRefs(block ontology.NodeBodyBlock) []ontology.NodeRef {
	if block.ChildRef != nil {
		return []ontology.NodeRef{*block.ChildRef}
	}
	return append([]ontology.NodeRef(nil), block.ChildRefs...)
}

func workspaceBodyRefKey(ref ontology.NodeRef) string {
	return strings.Join([]string{ref.NotePath, ref.NodeID, ref.Fragment, string(ref.Kind)}, "\x00")
}

func workspaceBodyChildBinding(parent *workspaceBodyBinding, block ontology.NodeBodyBlock, projection *ontology.NodeProjection) *workspaceBodyBinding {
	if projection == nil {
		return nil
	}
	fieldPath := strings.TrimSpace(block.FieldName)
	if parent != nil && parent.FieldPath != "" && fieldPath != "" {
		fieldPath = parent.FieldPath + "." + fieldPath
	}
	return &workspaceBodyBinding{
		TypeName:        workspaceBodyTypeName(projection),
		FieldName:       block.FieldName,
		FieldPath:       fieldPath,
		FieldList:       block.Kind == ontology.NodeBodyBlockKindCollection,
		SectionDisplay:  block.SectionDisplay,
		Properties:      workspaceBodyProperties(projection),
		IdentifierField: workspaceBodyIdentifierField(projection),
		PreviewTemplate: workspaceBodyPreviewTemplate(projection),
		Collapsed:       projection.Type != nil && projection.Type.Preview != nil && projection.Type.Preview.Collapsed,
	}
}

func workspaceBodyTypeName(projection *ontology.NodeProjection) string {
	if projection == nil {
		return ""
	}
	if projection.Type != nil {
		return projection.Type.Name
	}
	return firstNonEmpty(projection.ResolvedType, projection.Ref.TypeName)
}

func workspaceBodyProperties(projection *ontology.NodeProjection) map[string]string {
	if projection == nil {
		return nil
	}
	workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(nil, projection)
	if workspace == nil {
		return nil
	}
	properties := make(map[string]string)
	for _, field := range workspace.Fields {
		if len(field.SectionNodes) > 0 || len(field.Values) == 0 {
			continue
		}
		properties[field.Name] = strings.Join(field.Values, ", ")
	}
	if len(properties) == 0 {
		return nil
	}
	return properties
}

func workspaceBodyIdentifierField(projection *ontology.NodeProjection) string {
	if projection == nil || projection.Type == nil {
		return ""
	}
	for _, field := range projection.Type.Fields {
		if field != nil && field.IsPreferredIdentifier {
			return field.Name
		}
	}
	for _, field := range projection.Type.Fields {
		if field != nil && field.IsIdentifier {
			return field.Name
		}
	}
	return ""
}

func workspaceBodyPreviewTemplate(projection *ontology.NodeProjection) string {
	if projection == nil || projection.Type == nil || projection.Type.Preview == nil {
		return ""
	}
	preview := projection.Type.Preview.Template
	properties := workspaceBodyProperties(projection)
	title := ontology.BuildNodeWorkspaceFromProjection(projection).Node.Title
	return workspaceBodyPreviewPlaceholder.ReplaceAllStringFunc(preview, func(match string) string {
		parts := workspaceBodyPreviewPlaceholder.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		if parts[1] == "title" {
			return title
		}
		return properties[parts[1]]
	})
}

func (e *executor) resolveWorkspaceBodyNode(item workspaceBodyNode, links fieldLinkTargets, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	if item.Projection == nil || item.Workspace == nil {
		return out
	}
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "ref":
			out[key] = nodeRefGraphQLValue(item.Projection.Ref)
		case "title":
			out[key] = item.Workspace.Node.Title
		case "resolvedType":
			out[key] = emptyNil(item.Workspace.Node.ResolvedType)
		case "locator":
			out[key] = workspaceBodyLocator(item.Projection.Ref)
		case "parentRef":
			switch {
			case item.Parent != nil:
				out[key] = nodeRefGraphQLValue(*item.Parent)
			case item.Workspace.Node.ParentRef != nil:
				out[key] = nodeRefGraphQLValue(*item.Workspace.Node.ParentRef)
			default:
				out[key] = nil
			}
		case "markdown":
			out[key] = item.Workspace.Content.Markdown
		case "level":
			out[key] = emptyNil(workspaceBodyLevel(item.Projection))
		case "blockId":
			out[key] = emptyNil(workspaceBodyBlockID(item.Projection))
		case "fragment":
			out[key] = emptyNil(item.Projection.Ref.Fragment)
		case "binding":
			out[key] = e.resolveWorkspaceBodyBinding(item.Binding, field.SelectionSet, append(path, key))
		case "fields":
			fields := make([]any, 0, len(item.Workspace.Fields))
			for _, value := range item.Workspace.Fields {
				fields = append(fields, e.resolveWorkspaceField(value, links, field.SelectionSet, append(path, key)))
			}
			out[key] = fields
		case "collections":
			collections := make([]any, 0, len(item.Workspace.Collections))
			for _, value := range item.Workspace.Collections {
				collections = append(collections, e.resolveWorkspaceCollection(value, field.SelectionSet, append(path, key)))
			}
			out[key] = collections
		case "blocks":
			blocks := make([]any, 0, len(item.Blocks))
			for _, value := range item.Blocks {
				blocks = append(blocks, e.resolveWorkspaceBodyBlock(value, field.SelectionSet, append(path, key)))
			}
			out[key] = blocks
		default:
			e.addError(append(path, key), "field does not exist on NodeBodyProjection")
		}
	})
	return out
}

func workspaceBodyLocator(ref ontology.NodeRef) string {
	if ref.Kind == ontology.NodeKindNote {
		return "FILE"
	}
	return string(ref.Kind)
}

func workspaceBodyLevel(projection *ontology.NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
		return string(section.Level)
	}
	return ""
}

func workspaceBodyBlockID(projection *ontology.NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
		return strings.TrimPrefix(section.BlockID, "^")
	}
	if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
		return strings.TrimPrefix(span.BlockID, "^")
	}
	return ""
}

func (e *executor) resolveWorkspaceBodyBinding(item *workspaceBodyBinding, set ast.SelectionSet, path []string) any {
	if item == nil {
		return nil
	}
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "typeName":
			out[key] = emptyNil(item.TypeName)
		case "fieldName":
			out[key] = emptyNil(item.FieldName)
		case "fieldPath":
			out[key] = emptyNil(item.FieldPath)
		case "fieldList":
			out[key] = item.FieldList
		case "sectionDisplay":
			out[key] = emptyNil(string(item.SectionDisplay))
		case "properties":
			out[key] = item.Properties
		case "identifierField":
			out[key] = emptyNil(item.IdentifierField)
		case "previewTemplate":
			out[key] = emptyNil(item.PreviewTemplate)
		case "collapsed":
			out[key] = item.Collapsed
		default:
			e.addError(append(path, key), "field does not exist on NodeBodyBinding")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceBodyBlock(item ontology.NodeBodyBlock, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "kind":
			out[key] = string(item.Kind)
		case "range":
			out[key] = e.resolveWorkspaceRange(item.Range, field.SelectionSet, append(path, key))
		case "markdown":
			// Narrative edits reuse these bytes; trimming can join prose to a block locator.
			if strings.TrimSpace(item.Markdown) == "" {
				out[key] = nil
			} else {
				out[key] = item.Markdown
			}
		case "fieldName":
			out[key] = emptyNil(item.FieldName)
		case "rawKey":
			out[key] = emptyNil(item.RawKey)
		case "childRef":
			if item.ChildRef == nil {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(*item.ChildRef)
			}
		case "childRefs":
			refs := make([]any, 0, len(item.ChildRefs))
			for _, ref := range item.ChildRefs {
				refs = append(refs, nodeRefGraphQLValue(ref))
			}
			out[key] = refs
		case "sectionDisplay":
			out[key] = emptyNil(string(item.SectionDisplay))
		default:
			e.addError(append(path, key), "field does not exist on NodeBodyBlock")
		}
	})
	return out
}
