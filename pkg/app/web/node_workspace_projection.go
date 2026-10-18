package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"net/url"
	"strings"
)

func (s *Server) untypedNodeWorkspace(ctx context.Context, requestedRef ontology.NodeRef, includes nodeWorkspaceIncludes) (NodeWorkspaceResponse, error) {
	if requestedRef.IsZero() || strings.TrimSpace(requestedRef.NotePath) == "" {
		return NodeWorkspaceResponse{}, fmt.Errorf("node ref is required")
	}
	if requestedRef.Kind != "" && requestedRef.Kind != ontology.NodeKindNote {
		return NodeWorkspaceResponse{}, fmt.Errorf("ontology is unavailable for non-note refs")
	}

	rendered, err := s.readRenderedNote(ctx, requestedRef.NotePath)
	if err != nil {
		return NodeWorkspaceResponse{}, err
	}
	version := untypedWorkspaceVersion(rendered.Path, rendered.Rendered)
	resp := NodeWorkspaceResponse{
		RequestedRef: requestedRef.String(),
		Node: NodeDescriptorResponse{
			Ref: ontology.NodeRef{
				NotePath: requestedRef.NotePath,
				Kind:     ontology.NodeKindNote,
			},
			NotePath: requestedRef.NotePath,
			Title:    rendered.Title,
			Locator:  "FILE",
		},
		Content: NodeContentResponse{
			Path:     rendered.Path,
			Title:    rendered.Title,
			Markdown: rendered.Rendered,
		},
		Loaded: NodeWorkspaceLoadedResponse{
			Rendered: includes.Rendered,
		},
		Version: version,
	}
	if includes.Rendered {
		resp.Content.Rendered = &rendered
	}
	resp.FocusedNodeID, resp.Nodes, resp.Edges, resp.Views = buildWorkspaceGraph(&resp, nil, nil)
	return resp, nil
}

func untypedWorkspaceVersion(path, markdown string) string {
	sum := sha256.Sum256([]byte(path + "\n" + markdown))
	return hex.EncodeToString(sum[:8])
}

func (s *Server) nodeReadScope(ctx context.Context, defs *ontologyDefinitions) *noderead.Scope {
	return s.nodeReadScopeWithOverlay(ctx, defs, nil)
}

func (s *Server) nodeReadScopeWithOverlay(ctx context.Context, defs *ontologyDefinitions, overlay *ontologyquery.ReadOverlay) *noderead.Scope {
	if defs == nil || defs.schema == nil {
		return nil
	}
	var store noderead.ReadStore
	if s.runtime != nil {
		if intel := s.runtime.Intel(); intel != nil {
			store = intel
		}
	}
	noteReader := s.noteReader
	if noteReader == nil {
		noteReader = &obsidian.Note{}
	}
	return noderead.NewService(s.cfg.VaultDef, noteReader, store, defs.schema).
		WithNoteFormats(s.noteFormats).NewScope(noderead.ScopeOptions{ReadOverlay: overlay})
}

func (s *Server) resolveNodeProjection(ctx context.Context, defs *ontologyDefinitions, scope *noderead.Scope, ref ontology.NodeRef) (*ontology.NodeProjection, error) {
	if defs == nil || defs.schema == nil {
		return nil, fmt.Errorf("ontology is unavailable")
	}
	if s != nil && s.nodeProjectionCache != nil {
		noteReader := s.noteReader
		if noteReader == nil {
			noteReader = &obsidian.Note{}
		}
		projection, err := s.nodeProjectionCache.Projection(ctx, s.cfg.VaultDef, noteReader, defs.schema, ref)
		if err != nil {
			return nil, err
		}
		if projection != nil {
			s.nodeProjectionCache.StartWarmNote(context.Background(), s.cfg.VaultDef, noteReader, defs.schema, projection.Ref.NotePath)
		}
		return projection, nil
	}
	if scope == nil {
		scope = s.nodeReadScope(ctx, defs)
	}
	return scope.Projection(ctx, ref)
}

func nodeRefFromTarget(target string) (ontology.NodeRef, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return ontology.NodeRef{}, fmt.Errorf("node ref is required")
	}
	notePath, fragment, fragmentType := splitLinkFragment(target)
	if notePath == "" {
		notePath = target
	}
	ref := ontology.NodeRef{
		NotePath: notePath,
		Kind:     ontology.NodeKindNote,
	}
	if fragment != "" {
		ref.Fragment = joinAnchorFragment(fragment, fragmentType)
		ref.Kind = ontology.NodeKindSection
	}
	return ref, nil
}

func nodeRefFromRequest(values url.Values) (ontology.NodeRef, error) {
	ref, err := nodeRefFromTarget(values.Get("ref"))
	if err != nil {
		return ontology.NodeRef{}, err
	}
	if ref.IsZero() {
		return ref, nil
	}
	ref.NodeID = strings.TrimSpace(values.Get("nodeId"))
	ref.Structural = strings.TrimSpace(values.Get("structural"))
	if kind := strings.TrimSpace(values.Get("kind")); kind != "" {
		ref.Kind = ontology.NodeKind(kind)
	}
	return ref, nil
}

func (s *Server) buildNodeRenderedResponse(ctx context.Context, projection *ontology.NodeProjection, requestedRef string, resolvedType string, haveRenderedNote bool, renderedNote RenderedFileResponse, section *RenderedSection) *RenderedFileResponse {
	if projection == nil {
		return nil
	}
	notePath := projection.Ref.NotePath
	switch projection.Ref.Kind {
	case ontology.NodeKindNote:
		if haveRenderedNote {
			full := renderedNote
			full.Path = requestedRef
			if strings.TrimSpace(full.ResolvedType) == "" {
				full.ResolvedType = resolvedType
			}
			return &full
		}
		full, err := s.readRenderedNote(ctx, notePath)
		if err == nil {
			full.Path = requestedRef
			if strings.TrimSpace(full.ResolvedType) == "" {
				full.ResolvedType = resolvedType
			}
			return &full
		}
		return &RenderedFileResponse{
			Path:         requestedRef,
			Title:        firstNonEmptyString(ontologyProjectionTitle(projection), notePath),
			ResolvedType: resolvedType,
			Content:      projection.Snapshot.Content,
			Rendered:     projection.Snapshot.Content,
			Links:        s.resolveMarkdownLinksCompat(notePath, projection.Snapshot.Content),
			Embeds:       s.renderedEmbeds(ctx, notePath, projection.Snapshot.Content),
		}
	default:
		markdown := projectionMarkdown(projection)
		rendered := &RenderedFileResponse{
			Path:         requestedRef,
			Title:        ontologyProjectionTitle(projection),
			ResolvedType: resolvedType,
			Content:      markdown,
			Rendered:     markdown,
			Links:        s.resolveMarkdownLinksCompat(notePath, markdown),
			Embeds:       s.renderedEmbeds(ctx, notePath, markdown),
		}
		if section != nil {
			rendered.Title = firstNonEmptyString(section.Title, rendered.Title)
			rendered.ResolvedType = firstNonEmptyString(section.TypeName, rendered.ResolvedType)
			rendered.Sections = []RenderedSection{*section}
		}
		return rendered
	}
}

func nodeWorkspaceStatus(base ontology.NodeStatus, assessment *ontology.NoteAssessment) ontology.NodeStatus {
	status := base
	issueCount := assessmentIssueCount(assessment)
	status.Validation = ontology.NodeValidationStatus{IssueCount: issueCount}
	status.HasWarnings = issueCount > 0
	return status
}

func applyWorkspaceAssessmentStatus(resp *NodeWorkspaceResponse, assessment *ontology.NoteAssessment) {
	if resp == nil || assessment == nil {
		return
	}
	resp.Status = nodeWorkspaceStatus(resp.Status, assessment)
	for idx := range resp.Fields {
		fieldAssessment, ok := assessment.Field(resp.Fields[idx].Name)
		if !ok {
			continue
		}
		issueCount := len(fieldAssessment.Issues)
		resp.Fields[idx].Status.Validation = ontology.NodeValidationStatus{
			IssueCount: issueCount,
		}
		resp.Fields[idx].Status.HasWarnings = issueCount > 0
	}
	for idx := range resp.Collections {
		fieldAssessment, ok := assessment.Field(resp.Collections[idx].Name)
		if !ok {
			continue
		}
		issueCount := len(fieldAssessment.Issues)
		resp.Collections[idx].Status.Validation = ontology.NodeValidationStatus{
			IssueCount: issueCount,
		}
		resp.Collections[idx].Status.HasWarnings = issueCount > 0
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func assessmentIssueCount(assessment *ontology.NoteAssessment) int {
	if assessment == nil {
		return 0
	}
	count := len(assessment.Issues)
	for _, field := range assessment.Fields {
		count += len(field.Issues)
	}
	for _, relation := range assessment.Relations {
		count += len(relation.Issues)
	}
	return count
}

func projectionMarkdown(projection *ontology.NodeProjection) string {
	if projection == nil {
		return ""
	}
	if projection.Ref.Kind == ontology.NodeKindNote {
		return projection.Snapshot.Content
	}
	if node := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; node != nil {
		return strings.TrimSpace(node.Content)
	}
	return ""
}

func findRenderedSectionByNodeID(sections []RenderedSection, nodeID string) (RenderedSection, bool) {
	if strings.TrimSpace(nodeID) == "" {
		return RenderedSection{}, false
	}
	for _, section := range flattenRenderedSections(sections) {
		if strings.TrimSpace(section.ID) == strings.TrimSpace(nodeID) {
			return section, true
		}
	}
	return RenderedSection{}, false
}

func ontologyProjectionTitle(projection *ontology.NodeProjection) string {
	if projection == nil {
		return ""
	}
	if strings.TrimSpace(projection.ResolvedType) == "" && projection.Ref.Kind == ontology.NodeKindSection {
		if node := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; node != nil {
			return node.Title
		}
	}
	return ontology.BuildNodeWorkspaceFromProjection(projection).Node.Title
}
