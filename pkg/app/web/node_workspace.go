package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type nodeWorkspaceIncludes struct {
	Rendered   bool
	Assessment bool
	Structure  bool
	Relations  bool
}

func defaultNodeWorkspaceIncludes() nodeWorkspaceIncludes {
	return nodeWorkspaceIncludes{
		Rendered:   true,
		Assessment: true,
		Structure:  true,
		Relations:  true,
	}
}

func (i nodeWorkspaceIncludes) loaded() NodeWorkspaceLoadedResponse {
	return NodeWorkspaceLoadedResponse{
		Rendered:   i.Rendered,
		Assessment: i.Assessment,
		Structure:  i.Structure,
		Relations:  i.Relations,
	}
}

func (s *Server) nodeWorkspace(ctx context.Context, requestedRef ontology.NodeRef, includes nodeWorkspaceIncludes) (NodeWorkspaceResponse, error) {
	// Docs: [[ontology-browser-workspace#^spec-0014-us2-ac1]]: this HTTP path
	// adapts the canonical ontology node workspace for whole-note, section, and
	// embedded-node panes.
	trace := os.Getenv("RZM_TRACE_NODE_WORKSPACE") != ""
	start := time.Now()
	last := start
	traceStep := func(label string) {
		if !trace {
			return
		}
		now := time.Now()
		log.Printf("node-workspace trace %s step=%s delta=%s total=%s", requestedRef.String(), label, now.Sub(last), now.Sub(start))
		last = now
	}
	defs, err := s.ontologyDefinitions()
	traceStep("defs")
	if err != nil {
		return NodeWorkspaceResponse{}, err
	}
	if defs == nil || defs.schema == nil {
		return s.untypedNodeWorkspace(ctx, requestedRef, includes)
	}
	nodeScope := s.nodeReadScope(ctx, defs)
	traceStep("scope")
	projection, err := s.resolveNodeProjection(ctx, defs, nodeScope, requestedRef)
	traceStep("projection")
	if err != nil {
		log.Printf(
			"node-workspace resolve failed note=%q fragment=%q nodeId=%q structural=%q kind=%q err=%v",
			requestedRef.NotePath,
			requestedRef.Fragment,
			requestedRef.NodeID,
			requestedRef.Structural,
			requestedRef.Kind,
			err,
		)
		return NodeWorkspaceResponse{}, fmt.Errorf(
			"node-workspace resolve failed: note=%q fragment=%q nodeId=%q structural=%q kind=%q: %w",
			requestedRef.NotePath,
			requestedRef.Fragment,
			requestedRef.NodeID,
			requestedRef.Structural,
			requestedRef.Kind,
			err,
		)
	}
	core := ontology.BuildNodeWorkspaceFromProjectionWithSchema(defs.schema, projection)
	traceStep("build-core")
	if core == nil {
		return NodeWorkspaceResponse{}, fmt.Errorf("failed to build node workspace")
	}
	node := core.Node
	workspacePath := node.Ref.String()
	if strings.TrimSpace(workspacePath) == "" {
		workspacePath = projection.Ref.String()
	}

	var (
		renderedNote         RenderedFileResponse
		haveRenderedNote     bool
		sectionRendered      *RenderedSection
		assessment           *ontology.NoteAssessment
		typeDoc              *ontology.TypeDoc
		structural           *StructuralViewResponse
		relations            []NoteWorkspaceGroup
		sectionRelationGroup map[string][]NoteWorkspaceGroup
	)

	needRenderedNote := includes.Rendered || includes.Structure || includes.Relations
	needInspect := includes.Assessment || (includes.Structure && projection.Ref.Kind == ontology.NodeKindNote)
	var (
		inspected     ontology.InspectNote
		haveInspected bool
		fetchWG       sync.WaitGroup
		fetchMu       sync.Mutex
		fetchErr      error
	)
	setFetchErr := func(err error) {
		if err == nil {
			return
		}
		fetchMu.Lock()
		if fetchErr == nil {
			fetchErr = err
		}
		fetchMu.Unlock()
	}
	if needRenderedNote {
		fetchWG.Add(1)
		go func() {
			defer fetchWG.Done()
			rendered, err := s.readRenderedNote(ctx, projection.Ref.NotePath)
			if err != nil {
				setFetchErr(err)
				return
			}
			fetchMu.Lock()
			renderedNote = rendered
			haveRenderedNote = true
			fetchMu.Unlock()
		}()
	}

	if needInspect {
		fetchWG.Add(1)
		go func() {
			defer fetchWG.Done()
			service, _, err := s.ontologyContext()
			if err != nil {
				setFetchErr(err)
				return
			}
			if service == nil {
				return
			}
			result, err := service.Inspect(ctx, projection.Ref.NotePath)
			if err != nil {
				setFetchErr(err)
				return
			}
			fetchMu.Lock()
			inspected = result
			haveInspected = true
			fetchMu.Unlock()
		}()
	}
	fetchWG.Wait()
	traceStep("fetch-optional")
	if fetchErr != nil {
		return NodeWorkspaceResponse{}, fetchErr
	}
	if projection.Ref.Kind != ontology.NodeKindNote && haveRenderedNote {
		if section, ok := findRenderedSectionByNodeID(renderedNote.Sections, node.Ref.NodeID); ok {
			sectionRendered = &section
		}
	}
	if haveInspected {
		if projection.Ref.Kind == ontology.NodeKindNote && strings.TrimSpace(node.ResolvedType) == "" {
			node.ResolvedType = inspected.ResolvedType
		}
		if includes.Assessment && projection.Ref.Kind == ontology.NodeKindNote {
			assessment = inspected.Assessment
			typeDoc = inspected.TypeDoc
		}
		if includes.Structure && projection.Ref.Kind == ontology.NodeKindNote {
			typeDoc = inspected.TypeDoc
		}
		if includes.Relations && projection.Ref.Kind == ontology.NodeKindNote && haveRenderedNote {
			var relationWG sync.WaitGroup
			var relationMu sync.Mutex
			var relationErr error
			setRelationErr := func(err error) {
				if err != nil {
					relationMu.Lock()
					if relationErr == nil {
						relationErr = err
					}
					relationMu.Unlock()
				}
			}
			relationWG.Add(2)
			go func() {
				defer relationWG.Done()
				groups, err := s.sectionRelationGroups(ctx, projection.Ref.NotePath, renderedNote)
				if err != nil {
					setRelationErr(err)
					return
				}
				relationMu.Lock()
				sectionRelationGroup = groups
				relationMu.Unlock()
			}()
			go func() {
				defer relationWG.Done()
				groups, err := s.workspaceGroups(ctx, nodeScope, projection.Ref.NotePath, renderedNote, inspected)
				if err != nil {
					setRelationErr(err)
					return
				}
				relationMu.Lock()
				relations = groups
				relationMu.Unlock()
			}()
			relationWG.Wait()
			if relationErr != nil {
				return NodeWorkspaceResponse{}, relationErr
			}
		}
	}

	if typeDoc == nil && (includes.Assessment || includes.Structure) && strings.TrimSpace(node.ResolvedType) != "" {
		typeDoc = schemaTypeDoc(defs, node.ResolvedType)
	}

	var rendered *RenderedFileResponse
	if includes.Rendered {
		rendered = s.buildNodeRenderedResponse(ctx, projection, workspacePath, node.ResolvedType, haveRenderedNote, renderedNote, sectionRendered)
	}
	if includes.Structure {
		switch projection.Ref.Kind {
		case ontology.NodeKindNote:
			if haveRenderedNote {
				structural = buildStructuralView(renderedNote, typeDoc)
			}
		default:
			if sectionRendered != nil {
				structural = buildSectionStructuralView(*sectionRendered, typeDoc)
			}
		}
	}
	if includes.Relations && projection.Ref.Kind != ontology.NodeKindNote && haveRenderedNote {
		groups, err := s.sectionRelationGroups(ctx, projection.Ref.NotePath, renderedNote)
		if err != nil {
			return NodeWorkspaceResponse{}, err
		}
		if sectionRendered != nil {
			if items := groups[sectionRendered.ID]; len(items) > 0 {
				sectionRelationGroup = map[string][]NoteWorkspaceGroup{
					sectionRendered.ID: slices.Clone(items),
				}
				relations = slices.Clone(items)
			}
		}
	}
	if includes.Relations && projection.Ref.Kind != ontology.NodeKindNote {
		if parentGroup, ok := parentWorkspaceGroup(node.ParentRef, renderedNote); ok {
			relations = append([]NoteWorkspaceGroup{parentGroup}, relations...)
		}
	}

	nodeLocator, ok := s.fastNodeWorkspaceLocator(ctx, projection.Ref, node.Title)
	var linkFixOps []OntologyEditOp
	if !ok {
		nodeLocator, linkFixOps, err = s.nodeWorkspaceLocator(ctx, defs, nodeScope, projection.Ref, ontology.EnsureLinkTargetPlan)
		traceStep("locator")
		if err != nil {
			return NodeWorkspaceResponse{}, err
		}
	} else {
		traceStep("locator-fast")
	}
	var linkTarget *ontology.NodeLinkTarget
	if nodeLocator != nil {
		linkTarget = nodeLocator.LinkTarget
	}

	status := nodeWorkspaceStatus(core.Status, assessment)
	var format noteformat.FormatID
	var sourceRepresentation ontology.SourceRepresentation
	var evidenceRepresentation ontology.EvidenceRepresentation
	var sourceCapabilities []noteformat.Capability
	if projection.RootSnapshot != nil {
		format = projection.RootSnapshot.Format
		sourceRepresentation = projection.RootSnapshot.SourceRepresentation
		evidenceRepresentation = projection.RootSnapshot.EvidenceRepresentation
		sourceCapabilities = projection.RootSnapshot.Projection.Capabilities.Values()
	} else if projection.Ref.Kind == ontology.NodeKindNote && s.catalog != nil {
		if selectedFormat, note := s.catalog.noteFormat(projection.Ref.NotePath); note {
			if provider, ok := s.catalog.formats.Provider(selectedFormat); ok {
				format = selectedFormat
				sourceRepresentation = ontology.SourceRepresentationUTF8
				evidenceRepresentation = ontology.EvidenceRepresentationProviderProjection
				sourceCapabilities = provider.Descriptor().Capabilities.Values()
			}
		}
	}
	contentTitle := node.Title
	if projection.Ref.Kind == ontology.NodeKindNote && format != "" && format != markdownCompatibilityFormatID && haveRenderedNote && strings.TrimSpace(renderedNote.Title) != "" {
		contentTitle = renderedNote.Title
	}
	resp := NodeWorkspaceResponse{
		RequestedRef: requestedRef.String(),
		Node: NodeDescriptorResponse{
			Ref:          node.Ref,
			ResolvedType: node.ResolvedType,
			NotePath:     node.NotePath,
			Title:        node.Title,
			Locator:      node.Locator,
			NodeLocator:  nodeLocator,
			ParentRef:    node.ParentRef,
		},
		Content: NodeContentResponse{
			Path:                   workspacePath,
			Title:                  contentTitle,
			ResolvedType:           node.ResolvedType,
			Markdown:               core.Content.Markdown,
			Format:                 format,
			SourceRepresentation:   sourceRepresentation,
			EvidenceRepresentation: evidenceRepresentation,
			SourceCapabilities:     sourceCapabilities,
			Rendered:               rendered,
			Assessment:             assessment,
			TypeDoc:                typeDoc,
			Structural:             structural,
		},
		Fields:                core.Fields,
		Collections:           core.Collections,
		Relations:             relations,
		SectionRelationGroups: sectionRelationGroup,
		Loaded:                includes.loaded(),
		Capabilities:          core.Capabilities,
		Status:                status,
		Version:               core.Version,
		SourceRevision:        core.SourceRevision,
		NodeLocator:           nodeLocator,
		LinkTarget:            linkTarget,
		LinkFixOps:            linkFixOps,
	}
	applyWorkspaceAssessmentStatus(&resp, assessment)
	resp.FocusedNodeID, resp.Nodes, resp.Edges, resp.Views = buildWorkspaceGraph(&resp, projection.Snapshot, defs.schema)
	traceStep("build-graph")
	s.applyFastWorkspaceNodeLocators(ctx, &resp)
	traceStep("fast-locators")
	traceStep("done")
	return resp, nil
}

func parentWorkspaceGroup(parentRef *ontology.NodeRef, renderedNote RenderedFileResponse) (NoteWorkspaceGroup, bool) {
	if parentRef == nil || parentRef.IsZero() || strings.TrimSpace(parentRef.NotePath) == "" {
		return NoteWorkspaceGroup{}, false
	}
	path := parentRef.String()
	if strings.TrimSpace(path) == "" {
		path = parentRef.NotePath
	}
	title := titleFromPath(parentRef.NotePath)
	if parentRef.Kind != ontology.NodeKindNote {
		if section, ok := findRenderedSectionByNodeID(renderedNote.Sections, parentRef.NodeID); ok && strings.TrimSpace(section.Title) != "" {
			title = section.Title
		} else if strings.TrimSpace(parentRef.Fragment) != "" {
			title = strings.TrimPrefix(strings.TrimSpace(parentRef.Fragment), "^")
		}
	}
	return NoteWorkspaceGroup{
		Key:   "parent",
		Label: "Parent",
		Items: []NoteWorkspaceLink{{
			Path:       path,
			Title:      title,
			Kind:       "note",
			Anchor:     parentRef.Fragment,
			Provenance: "parent",
			Structural: true,
		}},
	}, true
}

func (s *Server) applyFastWorkspaceNodeLocators(ctx context.Context, resp *NodeWorkspaceResponse) {
	if resp == nil {
		return
	}
	for i := range resp.Nodes {
		isFocused := resp.FocusedNodeID != "" && resp.Nodes[i].ID == resp.FocusedNodeID
		if isFocused && resp.NodeLocator != nil {
			resp.Nodes[i].NodeLocator = resp.NodeLocator
			continue
		}
		label := ""
		if resp.Nodes[i].Data != nil {
			label = resp.Nodes[i].Data.Title
		}
		locator, ok := s.fastNodeWorkspaceLocator(ctx, resp.Nodes[i].Ref, label)
		if !ok {
			continue
		}
		resp.Nodes[i].NodeLocator = locator
		if isFocused {
			resp.Node.NodeLocator = locator
			resp.NodeLocator = locator
			resp.LinkTarget = locator.LinkTarget
		}
	}
}

func (s *Server) fastNodeWorkspaceLocator(ctx context.Context, ref ontology.NodeRef, label string) (*ontology.NodeLocator, bool) {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	if ref.NotePath == "" {
		return nil, false
	}
	switch ref.Kind {
	case "", ontology.NodeKindNote:
		target := ontology.NodeLinkTarget{
			Ref:          ref,
			Markdown:     ref.NotePath,
			Wikilink:     "[[" + s.fastWikilinkTarget(ctx, ref.NotePath) + "]]",
			DisplayLabel: firstNonEmptyString(label, titleFromPath(ref.NotePath)),
			Exists:       true,
		}
		return &ontology.NodeLocator{
			Ref:           ref,
			Kind:          ontology.NodeKindNote,
			SourceLocator: ref.NotePath,
			LinkTarget:    &target,
			Status:        ontology.NodeLocatorLinkable,
		}, true
	case ontology.NodeKindEmbedded:
		if !strings.HasPrefix(ref.Fragment, "^") {
			return nil, false
		}
		blockID := strings.TrimPrefix(ref.Fragment, "^")
		target := ontology.NodeLinkTarget{
			Ref:          ref,
			Markdown:     ref.NotePath + "#^" + blockID,
			Wikilink:     "[[" + s.fastWikilinkTarget(ctx, ref.NotePath) + "#^" + blockID + "]]",
			DisplayLabel: firstNonEmptyString(label, ref.TypeName, blockID),
			Exists:       true,
			BlockID:      blockID,
		}
		return &ontology.NodeLocator{
			Ref:           ref,
			Kind:          ontology.NodeKindEmbedded,
			SourceLocator: ref.NotePath + "#^" + blockID,
			LinkTarget:    &target,
			Status:        ontology.NodeLocatorLinkable,
		}, true
	case ontology.NodeKindSection:
		sourceLocator := ref.NotePath
		if ref.Fragment != "" {
			sourceLocator += "#" + ref.Fragment
		} else if ref.NodeID != "" {
			sourceLocator = ref.NodeID
		} else if ref.Structural != "" {
			sourceLocator += "#struct:" + ref.Structural
		}
		return &ontology.NodeLocator{
			Ref:           ref,
			Kind:          ontology.NodeKindSection,
			SourceLocator: sourceLocator,
			Status:        ontology.NodeLocatorUnsupported,
			Diagnostics: []ontology.NodeLinkDiagnostic{{
				Code:     "unsupported_node_kind",
				NotePath: ref.NotePath,
				Ref:      ref,
				Message:  "only embedded ontology nodes are linkable with block identifiers",
			}},
		}, true
	default:
		return nil, false
	}
}

func (s *Server) fastWikilinkTarget(ctx context.Context, notePath string) string {
	stem := strings.TrimSuffix(filepath.Base(notePath), filepath.Ext(notePath))
	if stem != "" {
		if resolved, ok := s.notePathCache(ctx).ResolveNote(stem); ok && resolved == notePath {
			return stem
		}
	}
	return strings.TrimSuffix(notePath, filepath.Ext(notePath))
}

func (s *Server) nodeWorkspaceLocator(ctx context.Context, defs *ontologyDefinitions, scope *noderead.Scope, ref ontology.NodeRef, ensure ontology.EnsureLinkTargetMode) (*ontology.NodeLocator, []OntologyEditOp, error) {
	if defs == nil || defs.schema == nil {
		return nil, nil, nil
	}
	if scope == nil {
		scope = s.nodeReadScope(ctx, defs)
	}
	locators, err := scope.LocatorsWithEnsure(ctx, []ontology.NodeRef{ref}, ensure)
	if err != nil {
		return nil, nil, err
	}
	locator, ok := locators[nodeWorkspaceLocatorKey(ref)]
	if !ok {
		return nil, nil, nil
	}
	return &locator, nodeLinkFixActionsToEditOps(locator.FixActions), nil
}

func nodeWorkspaceLocatorKey(ref ontology.NodeRef) string {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	ref.Structural = strings.TrimSpace(ref.Structural)
	if ref.Kind == ontology.NodeKindNote {
		return ref.NotePath
	}
	if ref.Fragment != "" {
		return ref.String()
	}
	if ref.NodeID != "" {
		if strings.HasPrefix(ref.NodeID, ref.NotePath+"#") {
			return ref.NodeID
		}
		return ref.NotePath + "#node:" + ref.NodeID
	}
	if ref.Structural != "" {
		return ref.NotePath + "#struct:" + ref.Structural
	}
	return ref.String()
}

func nodeLinkFixActionsToEditOps(actions []ontology.NodeLinkFixAction) []OntologyEditOp {
	if len(actions) == 0 {
		return nil
	}
	fixOps := make([]OntologyEditOp, 0, len(actions))
	for _, action := range actions {
		fixOps = append(fixOps, OntologyEditOp{
			Kind:       "ensureBlockID",
			Path:       action.Ref.NotePath,
			NodeID:     action.Ref.NodeID,
			Structural: action.Ref.Structural,
			BlockID:    action.BlockID,
		})
	}
	return fixOps
}

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
	var store noderead.Store
	if s.runtime != nil {
		if intel := s.runtime.Intel(); intel != nil {
			store = intel
		}
	}
	noteReader := s.noteReader
	if noteReader == nil {
		noteReader = &obsidian.Note{}
	}
	return noderead.NewService(s.cfg.VaultDef, noteReader, store, defs.schema).NewScope(ctx, noderead.ScopeOptions{ReadOverlay: overlay})
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
