package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// OntologyEditKind is one serializable, source-preserving EditSession action.
// Whole-file rewrites and function transforms are intentionally excluded.
type OntologyEditKind string

const (
	OntologyEditSetScalar             OntologyEditKind = "set_scalar"
	OntologyEditSetScalarList         OntologyEditKind = "set_scalar_list"
	OntologyEditSetRawFrontmatterList OntologyEditKind = "set_raw_frontmatter_list"
	OntologyEditSetInline             OntologyEditKind = "set_inline"
	OntologyEditSetLink               OntologyEditKind = "set_link"
	OntologyEditAddEmbedded           OntologyEditKind = "add_embedded"
	OntologyEditDeleteNode            OntologyEditKind = "delete_node"
	OntologyEditReorder               OntologyEditKind = "reorder_collection"
	OntologyEditSetNarrative          OntologyEditKind = "set_narrative"
	OntologyEditEnsureBlockID         OntologyEditKind = "ensure_block_id"
	OntologyEditSetBlockID            OntologyEditKind = "set_block_id"
	OntologyEditRemoveBlockID         OntologyEditKind = "remove_block_id"
	OntologyEditAddSectionField       OntologyEditKind = "add_section_field"
)

// OntologyEdit is the stable repair representation for ontology replay.
type OntologyEdit struct {
	Kind             OntologyEditKind `json:"kind"`
	Ref              ontology.NodeRef `json:"ref,omitempty"`
	ParentRef        ontology.NodeRef `json:"parentRef,omitempty"`
	Field            string           `json:"field,omitempty"`
	Value            string           `json:"value,omitempty"`
	Values           []string         `json:"values,omitempty"`
	Heading          string           `json:"heading,omitempty"`
	Body             string           `json:"body,omitempty"`
	BlockID          string           `json:"blockId,omitempty"`
	RangeStart       int              `json:"rangeStart,omitempty"`
	RangeEnd         int              `json:"rangeEnd,omitempty"`
	PreviousMarkdown string           `json:"previousMarkdown,omitempty"`
	Markdown         string           `json:"markdown,omitempty"`
}

// OntologyPreviewRequest adapts one semantic action through PreviewCurrent.
type OntologyPreviewRequest struct {
	OperationID           string            `json:"operationId"`
	ActionIDs             []string          `json:"actionIds"`
	IssueKeys             []string          `json:"issueKeys"`
	Edits                 []OntologyEdit    `json:"edits"`
	BaseFingerprints      map[string]string `json:"baseFingerprints,omitempty"`
	AllowSelectorRecovery bool              `json:"allowSelectorRecovery,omitempty"`
}

// OntologyPreviewConflict preserves EditSession conflict evidence losslessly.
type OntologyPreviewConflict struct {
	Kind     ontology.ConflictKind `json:"kind"`
	NotePath string                `json:"notePath"`
	NodeRef  string                `json:"nodeRef,omitempty"`
	Field    string                `json:"field,omitempty"`
	Message  string                `json:"message"`
}

// OntologyPreviewResult is pure prospective output; conflicts imply no writes.
type OntologyPreviewResult struct {
	Operations   []RepairOperation         `json:"operations,omitempty"`
	Conflicts    []OntologyPreviewConflict `json:"conflicts,omitempty"`
	RebasedPaths []string                  `json:"rebasedPaths,omitempty"`
}

// OntologyOperationPreviewer is the consumer-side preview contract.
type OntologyOperationPreviewer interface {
	Preview(
		context.Context,
		RunContext,
		*ontology.Schema,
		OntologyPreviewRequest,
	) (OntologyPreviewResult, error)
}

type ontologyPreviewSession interface {
	RestoreBaseFingerprints(context.Context, map[string]string) error
	LoadNode(context.Context, ontology.NodeRef) (*ontology.NodeProjection, error)
	SetScalarField(ontology.NodeRef, string, string) error
	SetScalarListField(ontology.NodeRef, string, []string) error
	SetRawFrontmatterList(ontology.NodeRef, string, []string) error
	SetInlineField(ontology.NodeRef, string, string) error
	SetLinkField(ontology.NodeRef, string, []string) error
	AddEmbeddedNode(ontology.NodeRef, string, string, string, string) error
	DeleteNode(ontology.NodeRef) error
	ReorderCollection(ontology.NodeRef, string, []string) error
	SetNarrative(ontology.NodeRef, int, int, string, string) error
	EnsureBlockID(ontology.NodeRef, string) error
	SetBlockID(ontology.NodeRef, string) error
	RemoveBlockID(ontology.NodeRef) error
	AddSectionField(ontology.NodeRef, string) error
	PreviewCurrent(context.Context) (ontology.CommitPlan, []ontology.ConflictReport, error)
}

// OntologyOperationAdapter cannot commit by construction: its dependency
// exposes PreviewCurrent but omits Commit and CommitWithOptions.
type OntologyOperationAdapter struct {
	newSession func(RunContext, *ontology.Schema, bool) ontologyPreviewSession
}

// NewOntologyOperationAdapter returns the production preview-only adapter.
func NewOntologyOperationAdapter() *OntologyOperationAdapter {
	return &OntologyOperationAdapter{
		newSession: func(runCtx RunContext, schema *ontology.Schema, allowSelectorRecovery bool) ontologyPreviewSession {
			if allowSelectorRecovery {
				return ontology.NewIdentifierRepairEditSession(runCtx.VaultDef, runCtx.NoteReader, schema)
			}
			if formats, err := runCtx.NoteMetadata.FormatRuntime(); err == nil {
				return ontology.NewProviderAwareEditSession(runCtx.VaultDef, runCtx.NoteReader, schema, formats)
			}
			return ontology.NewEditSession(runCtx.VaultDef, runCtx.NoteReader, schema)
		},
	}
}

var _ OntologyOperationPreviewer = (*OntologyOperationAdapter)(nil)

// Preview canonicalizes refs, stages semantic edits, and calls PreviewCurrent.
// It never invokes an EditSession write path.
func (a *OntologyOperationAdapter) Preview(
	ctx context.Context,
	runCtx RunContext,
	schema *ontology.Schema,
	request OntologyPreviewRequest,
) (OntologyPreviewResult, error) {
	if a == nil || a.newSession == nil {
		return OntologyPreviewResult{}, fmt.Errorf("ontology operation adapter is not initialized")
	}
	if schema == nil {
		return OntologyPreviewResult{}, fmt.Errorf("ontology schema is required")
	}
	if strings.TrimSpace(request.OperationID) == "" {
		return OntologyPreviewResult{}, fmt.Errorf("ontology operation id is required")
	}
	actionIDs := sortedUnique(request.ActionIDs)
	if len(actionIDs) == 0 {
		return OntologyPreviewResult{}, fmt.Errorf("ontology operation action ids are required")
	}
	issueKeys := sortedUnique(request.IssueKeys)
	if len(issueKeys) == 0 {
		return OntologyPreviewResult{}, fmt.Errorf("ontology operation issue keys are required")
	}
	session := a.newSession(runCtx, schema, request.AllowSelectorRecovery)
	if err := session.RestoreBaseFingerprints(ctx, request.BaseFingerprints); err != nil {
		return OntologyPreviewResult{}, err
	}
	identities := make([]string, 0, len(request.Edits))
	for _, edit := range request.Edits {
		identity, err := stageOntologyEdit(ctx, session, edit)
		if err != nil {
			return OntologyPreviewResult{}, err
		}
		if identity != "" {
			identities = append(identities, identity)
		}
	}
	plan, conflicts, err := session.PreviewCurrent(ctx)
	if err != nil {
		return OntologyPreviewResult{}, err
	}
	if len(conflicts) > 0 {
		result := OntologyPreviewResult{
			Conflicts: make([]OntologyPreviewConflict, 0, len(conflicts)),
		}
		for _, conflict := range conflicts {
			result.Conflicts = append(result.Conflicts, OntologyPreviewConflict{
				Kind: conflict.Kind, NotePath: conflict.NotePath,
				NodeRef: conflict.NodeRef, Field: conflict.Field, Message: conflict.Message,
			})
		}
		return result, nil
	}

	result := OntologyPreviewResult{}
	materialFiles := make([]ontology.FileCommitPlan, 0, len(plan.Files))
	for _, file := range plan.Files {
		if file.Rebased {
			result.RebasedPaths = append(result.RebasedPaths, file.NotePath)
		}
		if file.HasMaterialChange {
			materialFiles = append(materialFiles, file)
		}
	}
	sort.Slice(materialFiles, func(i, j int) bool { return materialFiles[i].NotePath < materialFiles[j].NotePath })
	for _, file := range materialFiles {
		current, err := runCtx.NoteReader.GetContents(runCtx.VaultDef, file.NotePath)
		if err != nil {
			return OntologyPreviewResult{}, err
		}
		currentHash := SourceHash([]byte(current))
		if strings.TrimPrefix(currentHash, "sha256:") != file.CurrentFingerprint {
			return OntologyPreviewResult{}, fmt.Errorf(
				"ontology preview became stale for %s; replan before applying",
				file.NotePath,
			)
		}
		operationID := request.OperationID
		if len(materialFiles) > 1 {
			pathHash := SourceHash([]byte(file.NotePath))
			operationID += ":" + pathHash[len("sha256:"):len("sha256:")+12]
		}
		updated := file.UpdatedContentPreview
		result.Operations = append(result.Operations, RepairOperation{
			ID:         operationID,
			ActionID:   actionIDs[0],
			ActionIDs:  append([]string(nil), actionIDs...),
			IssueKey:   issueKeys[0],
			IssueKeys:  append([]string(nil), issueKeys...),
			Kind:       RepairOperationWrite,
			Path:       file.NotePath,
			SourceHash: currentHash,
			Expected: []ExpectedText{{
				StartByte:   0,
				EndByte:     len(current),
				Text:        current,
				Replacement: updated,
			}},
			Identities: sortedUnique(identities),
			Content:    []byte(updated),
			Lifecycle: ClassifyLifecycleEdit(LifecycleEdit{
				Before: []byte(current),
				After:  []byte(updated),
			}),
		})
	}
	result.RebasedPaths = sortedUnique(result.RebasedPaths)
	return result, nil
}

func stageOntologyEdit(
	ctx context.Context,
	session ontologyPreviewSession,
	edit OntologyEdit,
) (string, error) {
	ref, parentRef := edit.Ref, edit.ParentRef
	var err error
	if edit.Kind == OntologyEditAddEmbedded || edit.Kind == OntologyEditReorder {
		parentRef, err = canonicalOntologyRef(ctx, session, parentRef)
	} else {
		ref, err = canonicalOntologyRef(ctx, session, ref)
	}
	if err != nil {
		return "", err
	}
	switch edit.Kind {
	case OntologyEditSetScalar:
		err = session.SetScalarField(ref, edit.Field, edit.Value)
	case OntologyEditSetScalarList:
		err = session.SetScalarListField(ref, edit.Field, edit.Values)
	case OntologyEditSetRawFrontmatterList:
		err = session.SetRawFrontmatterList(ref, edit.Field, edit.Values)
	case OntologyEditSetInline:
		err = session.SetInlineField(ref, edit.Field, edit.Value)
	case OntologyEditSetLink:
		err = session.SetLinkField(ref, edit.Field, edit.Values)
	case OntologyEditAddEmbedded:
		err = session.AddEmbeddedNode(parentRef, edit.Field, edit.Heading, edit.Body, edit.BlockID)
	case OntologyEditDeleteNode:
		err = session.DeleteNode(ref)
	case OntologyEditReorder:
		err = session.ReorderCollection(parentRef, edit.Field, edit.Values)
	case OntologyEditSetNarrative:
		err = session.SetNarrative(ref, edit.RangeStart, edit.RangeEnd, edit.PreviousMarkdown, edit.Markdown)
	case OntologyEditEnsureBlockID:
		err = session.EnsureBlockID(ref, edit.BlockID)
	case OntologyEditSetBlockID:
		err = session.SetBlockID(ref, edit.BlockID)
	case OntologyEditRemoveBlockID:
		err = session.RemoveBlockID(ref)
	case OntologyEditAddSectionField:
		err = session.AddSectionField(ref, edit.Field)
	default:
		return "", fmt.Errorf("unsupported ontology edit kind %q", edit.Kind)
	}
	if err != nil {
		return "", err
	}
	if edit.Kind == OntologyEditAddEmbedded || edit.Kind == OntologyEditReorder {
		return ontologyRefIdentity(parentRef), nil
	}
	return ontologyRefIdentity(ref), nil
}

func canonicalOntologyRef(
	ctx context.Context,
	session ontologyPreviewSession,
	ref ontology.NodeRef,
) (ontology.NodeRef, error) {
	projection, err := session.LoadNode(ctx, ref)
	if err != nil {
		return ontology.NodeRef{}, err
	}
	if projection == nil {
		return ontology.NodeRef{}, fmt.Errorf("ontology ref %q did not resolve", ref.String())
	}
	canonical := projection.Ref
	if canonical.Kind == ontology.NodeKindNote {
		canonical.NodeID = ""
		canonical.StartByte = 0
		canonical.EndByte = 0
		canonical.ParentID = ""
		canonical.Structural = ""
	}
	return canonical, nil
}

func ontologyRefIdentity(ref ontology.NodeRef) string {
	return strings.Join([]string{
		ref.NotePath,
		string(ref.Kind),
		ref.NodeID,
		ref.Structural,
	}, "\x00")
}
