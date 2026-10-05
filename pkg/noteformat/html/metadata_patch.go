package html

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

const (
	canonicalMetadataOpen  = `<script id="rhizome-metadata" type="application/json">`
	canonicalMetadataClose = `</script>`
)

// MetadataOperationKind identifies one operation against the canonical
// metadata object. The planner applies operations in the order supplied.
type MetadataOperationKind string

const (
	MetadataSet    MetadataOperationKind = "set"
	MetadataAdd    MetadataOperationKind = "add"
	MetadataDelete MetadataOperationKind = "delete"
	MetadataRename MetadataOperationKind = "rename"
)

// MetadataOperation is a provider-local semantic edit. Value accepts the
// same JSON-like values accepted by noteformat.NewMetadataValue, including a
// noteformat.MetadataValue when the caller already has a canonical value.
type MetadataOperation struct {
	Kind   MetadataOperationKind
	Key    string
	NewKey string
	Value  any
}

// MetadataPatchRequest supplies the source precondition and semantic edits.
// An empty ExpectedSourceHash skips the caller-side precondition; the plan
// still records the actual source hash for the transaction layer.
type MetadataPatchRequest struct {
	ExpectedSourceHash string
	Operations         []MetadataOperation
}

// MetadataByteEdit is an exact source edit suitable for adapting to a
// journaled repair operation. Expected is the original byte slice at the
// half-open range [StartByte, EndByte).
type MetadataByteEdit struct {
	StartByte   int
	EndByte     int
	Expected    string
	Replacement string
}

// MetadataPatchPreview describes the semantic and source effect without
// granting the planner any write authority.
type MetadataPatchPreview struct {
	MetadataPresent bool
	Inserted        bool
	OldJSON         []byte
	NewJSON         []byte
	ChangedStart    int
	ChangedEnd      int
}

// MetadataPatchPlan is a pure, source-bound preview. UpdatedSource and all
// byte slices are detached copies and may be passed to a later transaction
// adapter without rereading or rewriting the source here.
type MetadataPatchPlan struct {
	SourceHash         string
	ExpectedSourceHash string
	Edits              []MetadataByteEdit
	UpdatedSource      []byte
	HasMaterialChange  bool
	Preview            MetadataPatchPreview
}

// MetadataPatchError reports a fail-closed provider decision. Code is stable
// enough for callers to select a useful UI message; Message carries the local
// evidence needed for a manual repair.
type MetadataPatchError struct {
	Code    string
	Message string
	Range   noteformat.OptionalSourceRange
}

func (e *MetadataPatchError) Error() string {
	if e == nil {
		return "HTML metadata patch blocked"
	}
	return e.Code + ": " + e.Message
}

var ErrMetadataPatchBlocked = errors.New("HTML metadata patch blocked")

var _ noteformat.RootMetadataPatchPlanner = Provider{}

// PlanRootMetadataPatch adapts the format-neutral ontology edit contract to
// the HTML byte-preserving planner. A missing field is an add, while an
// existing field is a set. The provider reparses the sealed source to prove
// metadata ownership and turns the resulting source edit into the shared
// patch representation.
func (Provider) PlanRootMetadataPatch(source noteformat.AuthoredSource, projection noteformat.Projection, changes []noteformat.RootMetadataChange) (noteformat.MetadataPatchPlan, error) {
	var result noteformat.MetadataPatchPlan
	descriptor := Provider{}.Descriptor()
	if source.Format() != descriptor.ID {
		return result, fmt.Errorf("%w: source format %q is not HTML", ErrMetadataPatchBlocked, source.Format())
	}
	if projection.ProviderVersion != descriptor.ProviderVersion || projection.ProjectionVersion != descriptor.ProjectionVersion {
		return result, fmt.Errorf("%w: projection/provider generation does not match HTML metadata planner", ErrMetadataPatchBlocked)
	}
	if projection.Status != noteformat.ProjectionStatusCurrent {
		return result, fmt.Errorf("%w: HTML projection is not current", ErrMetadataPatchBlocked)
	}
	if err := noteformat.ValidateRootMetadataChanges(changes); err != nil {
		return result, fmt.Errorf("%w: %v", ErrMetadataPatchBlocked, err)
	}
	request := MetadataPatchRequest{ExpectedSourceHash: SourceHash(source.Bytes())}
	request.Operations = make([]MetadataOperation, 0, len(changes))
	for _, change := range changes {
		if change.Delete {
			request.Operations = append(request.Operations, MetadataOperation{Kind: MetadataDelete, Key: change.Key})
		} else {
			request.Operations = append(request.Operations, MetadataOperation{Kind: MetadataSet, Key: change.Key, Value: change.Value})
		}
	}
	planned, err := PlanMetadataPatch(source.Bytes(), request)
	if err != nil {
		return result, err
	}
	result.Patches = make([]noteformat.MetadataSourcePatch, len(planned.Edits))
	for index, edit := range planned.Edits {
		result.Patches[index] = noteformat.MetadataSourcePatch{
			Range:       noteformat.SourceRange{StartByte: edit.StartByte, EndByte: edit.EndByte},
			Expected:    []byte(edit.Expected),
			Replacement: []byte(edit.Replacement),
		}
	}
	if err := noteformat.ValidateMetadataPatchPlan(source, result); err != nil {
		return noteformat.MetadataPatchPlan{}, fmt.Errorf("%w: %v", ErrMetadataPatchBlocked, err)
	}
	return result, nil
}

// SourceHash returns the transaction-compatible identity of raw authored
// bytes. It deliberately hashes bytes before any UTF-8 or newline handling.
func SourceHash(source []byte) string {
	digest := sha256.Sum256(source)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// PlanMetadataPatch validates and previews a root metadata edit. It changes
// only the canonical JSON body, or one proven insertion span when the block is
// absent. It never parses or reserializes the surrounding HTML as a repair.
func PlanMetadataPatch(source []byte, request MetadataPatchRequest) (MetadataPatchPlan, error) {
	plan := MetadataPatchPlan{
		SourceHash:         SourceHash(source),
		ExpectedSourceHash: request.ExpectedSourceHash,
	}
	if !utf8.Valid(source) {
		return plan, patchError("html_metadata_source_invalid_utf8", "HTML source is not valid UTF-8", noteformat.OptionalSourceRange{})
	}
	if expected := strings.TrimSpace(request.ExpectedSourceHash); expected != "" && expected != plan.SourceHash {
		return plan, patchError("html_metadata_source_stale", "HTML source hash no longer matches the edit session", noteformat.OptionalSourceRange{})
	}
	if len(request.Operations) == 0 {
		plan.UpdatedSource = append([]byte(nil), source...)
		return plan, nil
	}

	document := parse(source)
	if len(document.metadata) > 1 {
		return plan, patchError("html_metadata_duplicate", "multiple canonical metadata blocks are owned by the same note", noteformat.OptionalSourceRange{})
	}
	if reserved := reservedMetadataScript(document); reserved != nil && len(document.metadata) == 0 {
		return plan, patchError("html_metadata_ambiguous", "a direct-head script reserves the canonical metadata id but does not have unambiguous canonical ownership", exactRange(reserved.start, reserved.end))
	}

	values := map[string]noteformat.MetadataValue{}
	metadataPresent := len(document.metadata) == 1
	var bodyStart, bodyEnd int
	if metadataPresent {
		candidate := document.metadata[0]
		start, end, err := metadataBodySpan(candidate, len(source))
		if err != nil {
			return plan, err
		}
		bodyStart, bodyEnd = start, end
		parsed, err := parseJSONMetadata(source[start:end])
		if err != nil {
			return plan, patchError("html_metadata_invalid_json", err.Error(), exactRange(start, end))
		}
		for _, member := range parsed.members {
			value, err := noteformat.NewMetadataValue(normalizeJSONValue(member.value))
			if err != nil {
				return plan, patchError("html_metadata_unsupported_value", fmt.Sprintf("metadata field %q: %v", member.key, err), exactRange(start+member.valueStart, start+member.valueEnd))
			}
			values[member.key] = value
		}
	}

	for index, operation := range request.Operations {
		if err := applyMetadataOperation(values, operation); err != nil {
			return plan, fmt.Errorf("metadata operation %d: %w", index, err)
		}
	}
	newJSON, err := marshalMetadata(values)
	if err != nil {
		return plan, err
	}
	plan.Preview.MetadataPresent = metadataPresent
	plan.Preview.NewJSON = append([]byte(nil), newJSON...)
	if metadataPresent {
		oldJSON := append([]byte(nil), source[bodyStart:bodyEnd]...)
		plan.Preview.OldJSON = oldJSON
		if bytes.Equal(oldJSON, newJSON) {
			plan.UpdatedSource = append([]byte(nil), source...)
			return plan, nil
		}
		plan.Edits = []MetadataByteEdit{{
			StartByte: bodyStart, EndByte: bodyEnd,
			Expected: string(oldJSON), Replacement: string(newJSON),
		}}
		plan.Preview.ChangedStart, plan.Preview.ChangedEnd = bodyStart, bodyEnd
	} else {
		insertion, err := metadataInsertion(source, document)
		if err != nil {
			return plan, err
		}
		replacement := renderMetadataInsertion(source, insertion, newJSON)
		plan.Preview.Inserted = true
		plan.Preview.ChangedStart, plan.Preview.ChangedEnd = insertion.offset, insertion.offset
		plan.Edits = []MetadataByteEdit{{StartByte: insertion.offset, EndByte: insertion.offset, Replacement: replacement}}
	}
	updated, err := applyByteEdits(source, plan.Edits)
	if err != nil {
		return plan, err
	}
	plan.UpdatedSource = updated
	plan.HasMaterialChange = !bytes.Equal(source, updated)
	return plan, nil
}

func applyMetadataOperation(values map[string]noteformat.MetadataValue, operation MetadataOperation) error {
	key := strings.TrimSpace(operation.Key)
	if key == "" {
		return patchError("html_metadata_key_missing", "metadata operation requires a non-empty key", noteformat.OptionalSourceRange{})
	}
	switch operation.Kind {
	case MetadataSet:
		value, err := noteformat.NewMetadataValue(operation.Value)
		if err != nil {
			return patchError("html_metadata_value_invalid", err.Error(), noteformat.OptionalSourceRange{})
		}
		// Set is intentionally an upsert at the provider boundary. Ontology
		// property editors expose one set operation for both an existing field
		// and a field in a newly inserted metadata object; callers that need a
		// strict insertion can use MetadataAdd.
		values[key] = value
	case MetadataAdd:
		value, err := noteformat.NewMetadataValue(operation.Value)
		if err != nil {
			return patchError("html_metadata_value_invalid", err.Error(), noteformat.OptionalSourceRange{})
		}
		if _, ok := values[key]; ok {
			return patchError("html_metadata_field_exists", fmt.Sprintf("cannot add existing metadata field %q; use set", key), noteformat.OptionalSourceRange{})
		}
		values[key] = value
	case MetadataDelete:
		if _, ok := values[key]; !ok {
			return patchError("html_metadata_field_missing", fmt.Sprintf("cannot delete missing metadata field %q", key), noteformat.OptionalSourceRange{})
		}
		delete(values, key)
	case MetadataRename:
		newKey := strings.TrimSpace(operation.NewKey)
		if newKey == "" {
			return patchError("html_metadata_key_missing", "metadata rename requires a non-empty destination key", noteformat.OptionalSourceRange{})
		}
		value, ok := values[key]
		if !ok {
			return patchError("html_metadata_field_missing", fmt.Sprintf("cannot rename missing metadata field %q", key), noteformat.OptionalSourceRange{})
		}
		if _, exists := values[newKey]; exists {
			return patchError("html_metadata_field_exists", fmt.Sprintf("cannot rename metadata field to existing key %q", newKey), noteformat.OptionalSourceRange{})
		}
		delete(values, key)
		values[newKey] = value
	default:
		return patchError("html_metadata_operation_unknown", fmt.Sprintf("unknown metadata operation %q", operation.Kind), noteformat.OptionalSourceRange{})
	}
	return nil
}

func marshalMetadata(values map[string]noteformat.MetadataValue) ([]byte, error) {
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode HTML metadata: %w", err)
	}
	// encoding/json escapes '<', '>', and '&' by default. Keep this assertion
	// next to the writer so a future encoder change cannot re-enable a literal
	// case-insensitive </script sequence inside the raw-text element.
	if bytes.Contains(bytes.ToLower(encoded), []byte("</script")) {
		return nil, patchError("html_metadata_script_terminator", "metadata value would terminate the canonical script element", noteformat.OptionalSourceRange{})
	}
	return encoded, nil
}

func metadataBodySpan(candidate *node, sourceLength int) (int, int, error) {
	if candidate == nil || candidate.kind != elementNode || candidate.start < 0 || candidate.end <= candidate.start || candidate.end > sourceLength {
		return 0, 0, patchError("html_metadata_span_invalid", "canonical metadata element has an invalid source span", noteformat.OptionalSourceRange{})
	}
	if candidate.duplicateAttrs {
		return 0, 0, patchError("html_metadata_span_ambiguous", "canonical metadata element has duplicate attributes", exactRange(candidate.start, candidate.end))
	}
	if !candidate.rawTextExact {
		return 0, 0, patchError("html_metadata_span_ambiguous", "canonical metadata body has no provable tokenizer correlation", exactRange(candidate.start, candidate.end))
	}
	if len(candidate.children) != 1 || candidate.children[0].kind != textNode {
		return 0, 0, patchError("html_metadata_span_ambiguous", "canonical metadata body is not one provable raw-text span", exactRange(candidate.start, candidate.end))
	}
	body := candidate.children[0]
	if body.start != candidate.rawTextStart || body.end != candidate.rawTextEnd || body.start < 0 || body.end < body.start || body.end > sourceLength || body.start < candidate.start || body.end > candidate.end {
		return 0, 0, patchError("html_metadata_span_invalid", "canonical metadata body span is outside its script element", exactRange(candidate.start, candidate.end))
	}
	return body.start, body.end, nil
}

// reservedMetadataScript finds a direct-head script using the reserved id,
// even when its type marker is wrong or duplicate attributes made recognition
// impossible. Treating it as absent would allow Save to create a second
// competing metadata block.
func reservedMetadataScript(document parsedDocument) *node {
	var found *node
	var visit func(*node)
	visit = func(n *node) {
		if found != nil || n == nil || n.kind != elementNode {
			return
		}
		if n.tag == "head" {
			for _, child := range n.children {
				if child.kind != elementNode || child.tag != "script" {
					continue
				}
				if id, ok := firstAttr(child, "id"); ok && id.value == "rhizome-metadata" {
					found = child
					return
				}
			}
			return
		}
		for _, child := range n.children {
			visit(child)
		}
	}
	visit(document.root)
	return found
}

type metadataInsertionPoint struct {
	offset  int
	hasHead bool
}

func metadataInsertion(source []byte, document parsedDocument) (metadataInsertionPoint, error) {
	if count := authoredElementCount(document.root, "head"); count > 1 {
		return metadataInsertionPoint{}, patchError("html_metadata_insertion_ambiguous", "HTML has multiple head elements", noteformat.OptionalSourceRange{})
	}
	if count := authoredElementCount(document.root, "body"); count > 1 {
		return metadataInsertionPoint{}, patchError("html_metadata_insertion_ambiguous", "HTML has multiple body elements", noteformat.OptionalSourceRange{})
	}
	if head := firstAuthoredElement(document.root, "head"); head != nil {
		if head.parent != document.root && (head.parent == nil || head.parent.tag != "html") {
			return metadataInsertionPoint{}, patchError("html_metadata_insertion_ambiguous", "head is not a document-level element", exactRange(head.start, head.end))
		}
		closeStart, ok := explicitCloseStart(source, head, "head")
		if !ok {
			return metadataInsertionPoint{}, patchError("html_metadata_insertion_ambiguous", "existing head has no provable closing tag for metadata insertion", exactRange(head.start, head.end))
		}
		return metadataInsertionPoint{offset: closeStart, hasHead: true}, nil
	}
	if body := firstAuthoredElement(document.root, "body"); body != nil && body.start >= 0 && body.start <= len(source) {
		if body.parent != document.root && (body.parent == nil || body.parent.tag != "html") {
			return metadataInsertionPoint{}, patchError("html_metadata_insertion_ambiguous", "body is not a document-level element", exactRange(body.start, body.end))
		}
		return metadataInsertionPoint{offset: body.start}, nil
	}
	return metadataInsertionPoint{}, patchError("html_metadata_insertion_ambiguous", "HTML has no explicit head or body insertion point", noteformat.OptionalSourceRange{})
}

func firstAuthoredElement(root *node, tag string) *node {
	if root == nil {
		return nil
	}
	if root.kind == elementNode && root.authored && root.tag == tag {
		return root
	}
	for _, child := range root.children {
		if found := firstAuthoredElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func explicitCloseStart(source []byte, element *node, tag string) (int, bool) {
	if element == nil || !element.authored || element.tag != tag || element.closing == nil || element.start < 0 || element.end <= element.start || element.end > len(source) {
		return 0, false
	}
	return element.closing.start, element.closing.start >= element.start && element.closing.end == element.end
}

func onlyHTMLSpace(value []byte) bool {
	for _, char := range value {
		if !isSpace(char) {
			return false
		}
	}
	return true
}

func renderMetadataInsertion(source []byte, insertion metadataInsertionPoint, metadata []byte) string {
	newline := "\n"
	if bytes.Contains(source, []byte("\r\n")) {
		newline = "\r\n"
	}
	lineStart := bytes.LastIndexByte(source[:insertion.offset], '\n') + 1
	linePrefix := source[lineStart:insertion.offset]
	lineIndent := lineStart > 0 && onlyHTMLSpace(linePrefix)
	indent := string(linePrefix)
	if !lineIndent {
		indent = ""
	}
	return renderInsertionWithJSON(newline, indent, lineIndent, insertion.hasHead, metadata)
}

func renderInsertionWithJSON(newline, indent string, lineIndent, hasHead bool, metadata []byte) string {
	if metadata == nil {
		metadata = []byte("{}")
	}
	scriptIndent := indent
	if !hasHead {
		scriptIndent += "  "
	}
	pretty := indentJSON(metadata, scriptIndent+"  ", newline)
	block := canonicalMetadataOpen + newline + pretty + newline + scriptIndent + canonicalMetadataClose
	if !hasHead {
		block = "<head>" + newline + scriptIndent + block + newline + indent + "</head>"
	}
	block += newline + indent
	if !lineIndent {
		block = newline + block
	}
	return block
}

func indentJSON(metadata []byte, prefix, newline string) string {
	var out strings.Builder
	for index, line := range strings.Split(string(metadata), "\n") {
		if index > 0 {
			out.WriteString(newline)
		}
		out.WriteString(prefix)
		out.WriteString(line)
	}
	return out.String()
}

func applyByteEdits(source []byte, edits []MetadataByteEdit) ([]byte, error) {
	updated := append([]byte(nil), source...)
	for index := len(edits) - 1; index >= 0; index-- {
		edit := edits[index]
		if edit.StartByte < 0 || edit.EndByte < edit.StartByte || edit.EndByte > len(updated) {
			return nil, patchError("html_metadata_span_invalid", "metadata edit span is outside source", noteformat.OptionalSourceRange{})
		}
		if string(updated[edit.StartByte:edit.EndByte]) != edit.Expected {
			return nil, patchError("html_metadata_source_stale", "metadata edit span no longer matches the planned source", exactRange(edit.StartByte, edit.EndByte))
		}
		updated = append(append(append([]byte(nil), updated[:edit.StartByte]...), edit.Replacement...), updated[edit.EndByte:]...)
	}
	return updated, nil
}

func patchError(code, message string, sourceRange noteformat.OptionalSourceRange) error {
	return fmt.Errorf("%w: %w", ErrMetadataPatchBlocked, &MetadataPatchError{Code: code, Message: message, Range: sourceRange})
}
