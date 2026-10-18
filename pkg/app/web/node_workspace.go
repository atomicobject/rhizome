package web

import (
	"context"

	"fmt"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"

	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/paths"

	"log"

	"os"

	"slices"
	"strings"
	"sync"
	"time"
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
		typeDoc              *schemadoc.TypeDoc
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
	title := paths.NoteTitle(parentRef.NotePath)
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
