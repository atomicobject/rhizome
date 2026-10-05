package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	nodePreviewFieldLimit   = 24
	nodePreviewValueLimit   = 12
	nodePreviewSummaryRunes = 600
)

type NodePreviewValue struct {
	Text   string `json:"text"`
	Target string `json:"target,omitempty"`
}

type NodePreviewField struct {
	Name       string                          `json:"name"`
	Label      string                          `json:"label"`
	Kind       ontology.FieldKind              `json:"kind"`
	Importance ontology.FieldDisplayImportance `json:"importance"`
	Values     []NodePreviewValue              `json:"values"`
	Truncated  int                             `json:"truncated,omitempty"`
}

type NodePreviewFragment struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type NodePreview struct {
	Ref              string               `json:"ref"`
	Path             string               `json:"path"`
	Title            string               `json:"title"`
	TypeName         string               `json:"typeName,omitempty"`
	TypeLabel        string               `json:"typeLabel,omitempty"`
	Format           string               `json:"format"`
	Identifier       string               `json:"identifier,omitempty"`
	Summary          string               `json:"summary,omitempty"`
	Fragment         *NodePreviewFragment `json:"fragment,omitempty"`
	FragmentResolved bool                 `json:"fragmentResolved"`
	Fields           []NodePreviewField   `json:"fields"`
	Tags             []string             `json:"tags,omitempty"`
	UpdatedAt        int64                `json:"updatedAt,omitempty"`
	HasIssues        bool                 `json:"hasIssues"`
}

func (s *Server) handleNodePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errMissing("ref"))
		return
	}
	defs, err := s.ontologyDefinitions()
	if err != nil || defs == nil || defs.schema == nil {
		if err == nil {
			err = errors.New("ontology is unavailable")
		}
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorSchemaUnavailable, err)
		return
	}
	overlay, ok := s.readOverlayForStagedRead(w, r)
	if !ok {
		return
	}
	scope := s.nodeReadScopeWithOverlay(r.Context(), defs, overlay)
	if scope == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errors.New("node index is unavailable"))
		return
	}

	fromPath := strings.TrimSpace(r.URL.Query().Get("from"))
	resolveOne := func(input string) (noderead.ResolvedNode, bool, error) {
		result, resolveErr := scope.Resolve(r.Context(), noderead.ResolveRequest{
			OmitLocators: true,
			// With an edit session, fragments missing from the committed index
			// may be staged, so resolution may project the note (from its
			// staged source when touched); canonical paths are known only then.
			IndexOnly: overlay.Empty(),
			Targets:   []noderead.NodeTarget{{Input: input}},
			FromPath:  fromPath,
			Hydrate:   noderead.HydrateOptions{Profile: noderead.HydrateSummary},
		})
		if resolveErr != nil {
			return noderead.ResolvedNode{}, false, resolveErr
		}
		if len(result.Resolved) != 1 || result.Resolved[0].Record.Path == "" {
			return noderead.ResolvedNode{}, false, nil
		}
		return result.Resolved[0], true, nil
	}

	node, ok, err := resolveOne(ref)
	resolvedRequestedRef := ok
	baseRef, requestedFragment := splitPreviewRef(ref)
	reservedSelector := strings.HasPrefix(requestedFragment, "struct:") || strings.HasPrefix(requestedFragment, "node:")
	if err == nil && !ok && !reservedSelector {
		if baseRef != "" && requestedFragment != "" {
			node, ok, err = resolveOne(baseRef)
		}
	}
	if err != nil {
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	if !ok {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, fmt.Errorf("node %q did not resolve", ref))
		return
	}

	record := node.Record
	record.Ref = node.Ref
	fragment, err := s.previewFragment(r.Context(), overlay, firstNonEmpty(record.Ref.NotePath, record.Path), requestedFragment)
	if err != nil {
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	fragmentResolved := requestedFragment == "" || resolvedRequestedRef || fragment != nil
	noteType := defs.schema.Types[record.TypeName]
	linkInputs := previewLinkInputs(record, noteType)
	resolvedLinks := make(map[string]NodePreviewValue, len(linkInputs))
	if len(linkInputs) > 0 {
		targets := make([]noderead.NodeTarget, 0, len(linkInputs))
		for _, input := range linkInputs {
			targets = append(targets, noderead.NodeTarget{Input: input})
		}
		// Field values are authored in the previewed note, so they resolve
		// relative to it; `from` only locates the requested ref.
		linkFromPath := firstNonEmpty(record.Ref.NotePath, record.Path)
		result, resolveErr := scope.Resolve(r.Context(), noderead.ResolveRequest{
			OmitLocators: true,
			IndexOnly:    true,
			Targets:      targets,
			FromPath:     linkFromPath,
			Hydrate:      noderead.HydrateOptions{Profile: noderead.HydrateSummary},
		})
		if resolveErr != nil {
			writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, resolveErr)
			return
		}
		for _, resolved := range result.Resolved {
			if resolved.Record.Path == "" {
				continue
			}
			resolvedLinks[resolved.Input] = NodePreviewValue{
				Text:   firstNonEmpty(resolved.Record.Title, resolved.Input),
				Target: resolved.Ref.String(),
			}
		}
	}

	preview := buildNodePreview(record, noteType, resolvedLinks, fragmentResolved)
	preview.Fragment = fragment
	writeJSON(w, http.StatusOK, preview)
}

func splitPreviewRef(ref string) (string, string) {
	if index := strings.Index(ref, "#"); index >= 0 {
		return ref[:index], ref[index+1:]
	}
	return ref, ""
}

// previewFragment describes a heading or block target. A note the edit
// session touches is read from its staged source projection, the same facts
// the indexer stores for committed notes.
func (s *Server) previewFragment(ctx context.Context, overlay *ontologyquery.ReadOverlay, notePath, fragment string) (*NodePreviewFragment, error) {
	if strings.TrimSpace(fragment) != "" {
		if facts, ok := s.stagedNoteFacts(overlay, notePath); ok {
			return matchIndexedPreviewFragment(fragment, fragmentTargetRows(notePath, facts.FragmentTargets)), nil
		}
	}
	return s.indexedPreviewFragment(ctx, notePath, fragment)
}

func fragmentTargetRows(notePath string, targets []noteformat.FragmentTargetFact) []semdb.NoteFragmentTargetRow {
	rows := make([]semdb.NoteFragmentTargetRow, 0, len(targets))
	for _, target := range targets {
		kind := semdb.NoteFragmentTargetKind(target.Kind)
		if kind != semdb.NoteFragmentTargetHeading && kind != semdb.NoteFragmentTargetBlock {
			continue
		}
		rows = append(rows, semdb.NoteFragmentTargetRow{
			NotePath: notePath, Kind: kind, Target: target.Text, TargetNorm: target.NormalizedText, Ordinal: target.Ordinal,
		})
	}
	return rows
}

func (s *Server) indexedPreviewFragment(ctx context.Context, notePath, fragment string) (*NodePreviewFragment, error) {
	if s == nil || s.runtime == nil || strings.TrimSpace(notePath) == "" || strings.TrimSpace(fragment) == "" {
		return nil, nil
	}
	store := s.runtime.Intel()
	if store == nil {
		return nil, nil
	}
	rows, err := store.CurrentNoteFragmentTargets(ctx, []string{notePath}, "", "")
	if err != nil {
		return nil, err
	}
	return matchIndexedPreviewFragment(fragment, rows), nil
}

func matchIndexedPreviewFragment(fragment string, rows []semdb.NoteFragmentTargetRow) *NodePreviewFragment {
	if blockID, block := strings.CutPrefix(fragment, "^"); block {
		for _, row := range rows {
			if row.Kind == semdb.NoteFragmentTargetBlock && row.TargetNorm == blockID {
				return &NodePreviewFragment{Kind: "block", Text: row.Target}
			}
		}
		return nil
	}

	headingNorm := obsidian.NormalizeMarkdownHeadingFragment(fragment)
	for _, row := range rows {
		if row.Kind == semdb.NoteFragmentTargetHeading && row.TargetNorm == headingNorm {
			return &NodePreviewFragment{Kind: "heading", Text: row.Target}
		}
	}

	elementNorm := strings.ToLower(fragment)
	for _, kind := range []semdb.NoteFragmentTargetKind{semdb.NoteFragmentTargetElementID, semdb.NoteFragmentTargetLegacyName} {
		for _, row := range rows {
			if row.Kind == kind && row.TargetNorm == elementNorm {
				return &NodePreviewFragment{Kind: "element_id", Text: row.Target}
			}
		}
	}
	return nil
}

func buildNodePreview(record noderead.NodeRecord, noteType *ontology.NoteType, resolvedLinks map[string]NodePreviewValue, fragmentResolved bool) NodePreview {
	preview := NodePreview{
		Ref: record.Ref.String(), Path: firstNonEmpty(record.Ref.NotePath, record.Path), Title: record.Title,
		TypeName: record.TypeName, Format: string(record.Format), FragmentResolved: fragmentResolved,
		Fields: []NodePreviewField{}, Tags: record.Tags, UpdatedAt: record.UpdatedAt, HasIssues: record.HasIssues,
	}
	if noteType == nil {
		buildUntypedNodePreview(&preview, record)
		return preview
	}
	// NoteType.Label falls back to the type name when no display label is authored.
	preview.TypeLabel = noteType.Label
	if preview.TypeLabel == "" || !noteType.LabelAuthored {
		preview.TypeLabel = ontology.HumanizeFieldName(noteType.Name)
	}
	summaryField := ontology.SummaryField(noteType.Fields)
	if summaryField != nil && !summaryField.Display.HideHover {
		preview.Summary = capPreviewSummary(strings.Join(previewFieldValues(record, summaryField), " "))
	}
	for _, field := range noteType.Fields {
		if field == nil || !field.IsIdentifier {
			continue
		}
		values := previewFieldValues(record, field)
		if len(values) == 0 {
			continue
		}
		if preview.Identifier == "" || field.IsPreferredIdentifier {
			preview.Identifier = values[0]
		}
		if field.IsPreferredIdentifier {
			break
		}
	}

	for _, field := range noteType.Fields {
		if field == nil || field == summaryField || field.IsIdentifier || field.Kind == ontology.FieldKindSection || field.Kind == ontology.FieldKindNeighbor || field.Kind == ontology.FieldKindReverse {
			continue
		}
		if field.Display.HideHover || field.Display.EffectiveImportance() == ontology.FieldImportanceDetail {
			continue
		}
		values := previewFieldValues(record, field)
		if len(values) == 0 || len(values) == 1 && values[0] == record.Title {
			continue
		}
		preview.Fields = append(preview.Fields, makePreviewField(field.Name, field.Kind, field.Display.EffectiveImportance(), values, resolvedLinks))
	}
	sort.SliceStable(preview.Fields, func(i, j int) bool {
		return preview.Fields[i].Importance == ontology.FieldImportanceKey && preview.Fields[j].Importance != ontology.FieldImportanceKey
	})
	if len(preview.Fields) > nodePreviewFieldLimit {
		preview.Fields = preview.Fields[:nodePreviewFieldLimit]
	}
	return preview
}

func buildUntypedNodePreview(preview *NodePreview, record noderead.NodeRecord) {
	keys := make([]string, 0, len(record.Frontmatter))
	for key := range record.Frontmatter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := previewValues(record.Frontmatter[key], "")
		if len(values) == 0 {
			continue
		}
		if key == "summary" {
			preview.Summary = capPreviewSummary(strings.Join(values, " "))
			continue
		}
		preview.Fields = append(preview.Fields, makePreviewField(key, ontology.FieldKindScalar, ontology.FieldImportanceNormal, values, nil))
		if len(preview.Fields) == nodePreviewFieldLimit {
			break
		}
	}
}

func makePreviewField(name string, kind ontology.FieldKind, importance ontology.FieldDisplayImportance, values []string, resolvedLinks map[string]NodePreviewValue) NodePreviewField {
	field := NodePreviewField{Name: name, Label: ontology.HumanizeFieldName(name), Kind: kind, Importance: importance, Values: []NodePreviewValue{}}
	limit := len(values)
	if limit > nodePreviewValueLimit {
		field.Truncated = limit - nodePreviewValueLimit
		limit = nodePreviewValueLimit
	}
	for _, value := range values[:limit] {
		item := NodePreviewValue{Text: value}
		if kind == ontology.FieldKindLink {
			if resolved, ok := resolvedLinks[value]; ok {
				item = resolved
			} else {
				item.Text = appviews.LinkDisplayText(value)
			}
		}
		field.Values = append(field.Values, item)
	}
	return field
}

func previewLinkInputs(record noderead.NodeRecord, noteType *ontology.NoteType) []string {
	if noteType == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, field := range noteType.Fields {
		if field == nil || field.Kind != ontology.FieldKindLink {
			continue
		}
		for _, value := range previewFieldValues(record, field) {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func previewFieldValues(record noderead.NodeRecord, field *ontology.Field) []string {
	if field == nil {
		return nil
	}
	if rows := record.FieldValues[field.Name]; len(rows) > 0 {
		values := make([]string, 0, len(rows))
		for _, row := range rows {
			if value := strings.TrimSpace(row.ValueText); value != "" {
				values = append(values, value)
			}
		}
		return values
	}
	inline := ontology.IsSectionSummary(field) || field.SourceKind == ontology.FieldSourceInline || field.SourceKind == ontology.FieldSourceCheckbox || field.SourceKind == ontology.FieldSourceItemTitle || field.SourceKind == ontology.FieldSourceItemSummary || field.SourceKind == ontology.FieldSourceItemDetail
	names := ontology.FieldSourceNames(field)
	if len(names) == 0 {
		names = []string{field.Name}
	}
	for _, name := range names {
		var values []string
		if inline {
			values = previewValues(lookupPreviewProperty(record.InlineProps, name), field.TypeName)
		} else {
			values = previewValues(lookupPreviewProperty(record.Frontmatter, name), field.TypeName)
		}
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

// lookupPreviewProperty matches an authored property name exactly, then
// case-insensitively, the way ontology projection accepts case variations.
func lookupPreviewProperty[V any](props map[string]V, name string) any {
	if value, ok := props[name]; ok {
		return value
	}
	for key, value := range props {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return nil
}

func previewValues(raw any, typeName string) []string {
	switch value := raw.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := previewScalar(item, typeName); text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		if text := previewScalar(value, typeName); text != "" {
			return []string{text}
		}
		return nil
	}
}

func previewScalar(value any, typeName string) string {
	switch value := value.(type) {
	case time.Time:
		if typeName == "Date" {
			return value.Format(time.DateOnly)
		}
		return value.Format(time.RFC3339)
	case bool:
		return strconv.FormatBool(value)
	case float32:
		return strconv.FormatFloat(float64(value), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	case json.Number:
		return value.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func capPreviewSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if utf8.RuneCountInString(summary) <= nodePreviewSummaryRunes {
		return summary
	}
	runes := []rune(summary)
	return string(runes[:nodePreviewSummaryRunes-1]) + "…"
}
