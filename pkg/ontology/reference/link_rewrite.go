package reference

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// StructuredLinkComponent identifies the independently editable portion of a
// structured Markdown link. Delimiters remain source-owned and are never part
// of an edit.
type StructuredLinkComponent string

const (
	LinkComponentPath     StructuredLinkComponent = "PATH"
	LinkComponentFragment StructuredLinkComponent = "FRAGMENT"
	LinkComponentDisplay  StructuredLinkComponent = "DISPLAY"
)

// StructuredLinkResolution carries bounded read-model resolution for one
// discovered link. Ambiguity always blocks until the provenance subsystem can
// supply an opaque decision binding source, candidates, winner, and evidence.
type StructuredLinkResolution struct {
	LinkIndex  int                `json:"linkIndex"`
	Candidates []ontology.NodeRef `json:"candidates,omitempty"`
}

// StructuredLinkRewriteInput is the pure finalize seam. Discovery scans each
// note once and resolves candidates in batches before calling this planner.
type StructuredLinkRewriteInput struct {
	NotePath     string                               `json:"notePath"`
	Content      string                               `json:"content"`
	Links        []obsidian.StructuredLink            `json:"links"`
	LinkSnapshot *obsidian.StructuredLinkScanSnapshot `json:"-"`
	Rewrites     []IdentifierRewrite                  `json:"rewrites"`
	Resolutions  []StructuredLinkResolution           `json:"resolutions"`
}

// StructuredLinkEdit is an exact, preconditioned component replacement.
type StructuredLinkEdit struct {
	NotePath     string                  `json:"notePath"`
	LinkIndex    int                     `json:"linkIndex"`
	Component    StructuredLinkComponent `json:"component"`
	Range        ontology.ByteRange      `json:"range"`
	Expected     string                  `json:"expected"`
	Replacement  string                  `json:"replacement"`
	TargetOldRef ontology.NodeRef        `json:"targetOldRef"`
	TargetNewRef ontology.NodeRef        `json:"targetNewRef"`
}

type LinkRewriteDiagnosticKind string

const (
	LinkRewriteDiagnosticUnresolvedTarget LinkRewriteDiagnosticKind = "UNRESOLVED_TARGET"
	LinkRewriteDiagnosticAmbiguousTarget  LinkRewriteDiagnosticKind = "AMBIGUOUS_TARGET"
	LinkRewriteDiagnosticUnsafeSpan       LinkRewriteDiagnosticKind = "UNSAFE_SPAN"
	LinkRewriteDiagnosticInvalidInput     LinkRewriteDiagnosticKind = "INVALID_INPUT"
	LinkRewriteDiagnosticConflictingEdit  LinkRewriteDiagnosticKind = "CONFLICTING_EDIT"
)

// LinkRewriteDiagnostic is transaction-local evidence. Blocking diagnostics
// prevent the connected collision component from entering apply.
type LinkRewriteDiagnostic struct {
	Kind       LinkRewriteDiagnosticKind `json:"kind"`
	NotePath   string                    `json:"notePath"`
	LinkIndex  int                       `json:"linkIndex"`
	Target     string                    `json:"target,omitempty"`
	Candidates []ontology.NodeRef        `json:"candidates,omitempty"`
	Blocking   bool                      `json:"blocking"`
	Message    string                    `json:"message"`
}

type StructuredLinkRewritePlan struct {
	NotePath          string                  `json:"notePath,omitempty"`
	SourceFingerprint string                  `json:"sourceFingerprint,omitempty"`
	Edits             []StructuredLinkEdit    `json:"edits"`
	Diagnostics       []LinkRewriteDiagnostic `json:"diagnostics,omitempty"`
	Fingerprint       string                  `json:"fingerprint"`

	sealed string
}

// PlanStructuredLinkRewrites finalizes exact structured-link edits without
// rescanning or mutating content. Ambiguous targets are never first-wins.
func PlanStructuredLinkRewrites(input StructuredLinkRewriteInput) StructuredLinkRewritePlan {
	return finalizeStructuredLinkRewritePlan(planStructuredLinkRewrites(input))
}

func planStructuredLinkRewrites(input StructuredLinkRewriteInput) StructuredLinkRewritePlan {
	notePath, err := paths.CleanRelPath(input.NotePath)
	if err != nil || notePath.String() == "" {
		return StructuredLinkRewritePlan{Diagnostics: []LinkRewriteDiagnostic{{
			Kind: LinkRewriteDiagnosticInvalidInput, NotePath: input.NotePath, LinkIndex: -1,
			Blocking: true, Message: "structured link source path must be vault-relative",
		}}}
	}
	canonicalNotePath := notePath.String()
	links, sourceFingerprint, validSnapshot := validatedStructuredLinkInput(input)
	if !validSnapshot {
		return StructuredLinkRewritePlan{NotePath: canonicalNotePath, Diagnostics: []LinkRewriteDiagnostic{{
			Kind: LinkRewriteDiagnosticUnsafeSpan, NotePath: canonicalNotePath, LinkIndex: -1,
			Blocking: true, Message: "structured links must be the complete sealed scan of current content",
		}}}
	}
	rewrites := append([]IdentifierRewrite(nil), input.Rewrites...)
	sort.Slice(rewrites, func(i, j int) bool { return linkRewriteSortKey(rewrites[i]) < linkRewriteSortKey(rewrites[j]) })
	for _, rewrite := range rewrites {
		if !validCanonicalLinkRef(rewrite.OldRef) || !validCanonicalLinkRef(rewrite.NewRef) {
			return StructuredLinkRewritePlan{NotePath: canonicalNotePath, SourceFingerprint: sourceFingerprint, Diagnostics: []LinkRewriteDiagnostic{{
				Kind: LinkRewriteDiagnosticInvalidInput, NotePath: canonicalNotePath, LinkIndex: -1,
				Blocking: true, Message: "identifier rewrite refs must use canonical vault-relative paths",
			}}}
		}
	}

	resolutions, diagnostics := indexLinkResolutions(canonicalNotePath, links, input.Resolutions)
	edits := make([]StructuredLinkEdit, 0)
	for linkIndex, link := range links {
		resolution, found := resolutions[linkIndex]
		if !found {
			if linkMentionsAnyRewrite(link, rewrites) {
				diagnostics = append(diagnostics, linkDiagnostic(canonicalNotePath, linkIndex, link, LinkRewriteDiagnosticUnresolvedTarget, nil, "structured link has no bounded resolution evidence"))
			}
			continue
		}

		candidates := canonicalCandidateRefs(resolution.Candidates)
		if !validCanonicalLinkRefs(candidates) {
			diagnostics = append(diagnostics, linkDiagnostic(canonicalNotePath, linkIndex, link, LinkRewriteDiagnosticInvalidInput, candidates, "structured link resolution refs must use canonical vault-relative paths"))
			continue
		}
		matching := rewritesForCandidates(rewrites, candidates)
		relevant := len(matching) > 0 || linkMentionsAnyRewrite(link, rewrites)
		if !relevant {
			continue
		}

		winner, resolved := resolveStructuredLinkWinner(candidates)
		if !resolved {
			kind := LinkRewriteDiagnosticAmbiguousTarget
			message := "structured link resolves to multiple canonical targets"
			if len(candidates) == 0 {
				kind = LinkRewriteDiagnosticUnresolvedTarget
				message = "structured link target could not be resolved"
			}
			diagnostics = append(diagnostics, linkDiagnostic(canonicalNotePath, linkIndex, link, kind, candidates, message))
			continue
		}

		winnerRewrites := rewritesForRef(rewrites, winner)
		if len(winnerRewrites) == 0 {
			continue
		}
		if err := validateStructuredLink(input.Content, link); err != nil {
			diagnostics = append(diagnostics, linkDiagnostic(canonicalNotePath, linkIndex, link, LinkRewriteDiagnosticUnsafeSpan, candidates, err.Error()))
			continue
		}
		winnerRewrites = rewritesMatchingStructuredLink(winnerRewrites, link)
		if len(winnerRewrites) == 0 {
			continue
		}
		if len(winnerRewrites) > 1 {
			diagnostics = append(diagnostics, linkDiagnostic(canonicalNotePath, linkIndex, link, LinkRewriteDiagnosticConflictingEdit, candidates, "structured link matches multiple identifier rewrite memberships"))
			continue
		}

		edits = append(edits, editsForStructuredLink(canonicalNotePath, linkIndex, link, winnerRewrites[0])...)
	}

	sort.Slice(edits, func(i, j int) bool { return structuredLinkEditSortKey(edits[i]) < structuredLinkEditSortKey(edits[j]) })
	sort.Slice(diagnostics, func(i, j int) bool {
		return linkDiagnosticSortKey(diagnostics[i]) < linkDiagnosticSortKey(diagnostics[j])
	})
	return StructuredLinkRewritePlan{NotePath: canonicalNotePath, SourceFingerprint: sourceFingerprint, Edits: edits, Diagnostics: diagnostics}
}

func validatedStructuredLinkInput(input StructuredLinkRewriteInput) ([]obsidian.StructuredLink, string, bool) {
	sourceFingerprint := obsidian.StructuredLinkSourceFingerprint(input.Content)
	if input.LinkSnapshot != nil {
		snapshot, err := input.LinkSnapshot.ValidatedSnapshot()
		if err != nil || snapshot.SourceFingerprint != sourceFingerprint {
			return nil, "", false
		}
		return snapshot.Links, sourceFingerprint, true
	}
	if len(input.Links) == 0 || !obsidian.ValidateStructuredLinkScan(input.Content, input.Links) {
		return nil, "", false
	}
	return append([]obsidian.StructuredLink(nil), input.Links...), sourceFingerprint, true
}

func indexLinkResolutions(notePath string, links []obsidian.StructuredLink, input []StructuredLinkResolution) (map[int]StructuredLinkResolution, []LinkRewriteDiagnostic) {
	resolutions := make(map[int]StructuredLinkResolution, len(input))
	blocked := make(map[int]bool)
	var diagnostics []LinkRewriteDiagnostic
	for _, raw := range input {
		resolution := raw
		resolution.Candidates = append([]ontology.NodeRef(nil), raw.Candidates...)
		if resolution.LinkIndex < 0 || resolution.LinkIndex >= len(links) {
			diagnostics = append(diagnostics, LinkRewriteDiagnostic{
				Kind: LinkRewriteDiagnosticInvalidInput, NotePath: notePath, LinkIndex: resolution.LinkIndex,
				Blocking: true, Message: "structured link resolution index is outside the discovered link set",
			})
			continue
		}
		if blocked[resolution.LinkIndex] {
			continue
		}
		if _, duplicate := resolutions[resolution.LinkIndex]; duplicate {
			diagnostics = append(diagnostics, linkDiagnostic(notePath, resolution.LinkIndex, links[resolution.LinkIndex], LinkRewriteDiagnosticInvalidInput, nil, "structured link has duplicate resolution evidence"))
			delete(resolutions, resolution.LinkIndex)
			blocked[resolution.LinkIndex] = true
			continue
		}
		resolutions[resolution.LinkIndex] = resolution
	}
	return resolutions, diagnostics
}

func resolveStructuredLinkWinner(candidates []ontology.NodeRef) (ontology.NodeRef, bool) {
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return ontology.NodeRef{}, false
}

func validateStructuredLink(content string, link obsidian.StructuredLink) error {
	if !obsidian.ValidateStructuredLinkSource(content, link) {
		return fmt.Errorf("structured link is not sealed to the current source scan")
	}
	checks := []struct {
		name     string
		span     obsidian.StructuredLinkSpan
		expected string
		required bool
	}{
		{name: "raw", span: link.RawSpan, expected: link.RawSpan.Text(content), required: true},
		{name: "target", span: link.TargetSpan, expected: link.Target, required: true},
		{name: "path", span: link.PathSpan, expected: link.Path, required: true},
		{name: "fragment", span: link.FragmentSpan, expected: link.Fragment, required: link.FragmentSpan.Valid()},
		{name: "display", span: link.DisplaySpan, expected: link.Display, required: link.DisplaySpan.Valid()},
	}
	for _, check := range checks {
		if !check.required {
			continue
		}
		if !check.span.Valid() || check.span.End > len(content) {
			return fmt.Errorf("structured link %s span is outside current content", check.name)
		}
		if actual := check.span.Text(content); actual != check.expected {
			return fmt.Errorf("structured link %s span expected %q, found %q", check.name, check.expected, actual)
		}
	}
	return nil
}

func editsForStructuredLink(notePath string, linkIndex int, link obsidian.StructuredLink, rewrite IdentifierRewrite) []StructuredLinkEdit {
	components := []struct {
		kind        StructuredLinkComponent
		span        obsidian.StructuredLinkSpan
		expected    string
		replacement string
	}{
		{kind: LinkComponentPath, span: link.PathSpan, expected: link.Path, replacement: replacementLinkPath(link.Path, rewrite)},
		{kind: LinkComponentFragment, span: link.FragmentSpan, expected: link.Fragment, replacement: replacementLinkFragment(link.Fragment, rewrite)},
		{kind: LinkComponentDisplay, span: link.DisplaySpan, expected: link.Display, replacement: replacementLinkDisplay(link.Display, rewrite)},
	}
	edits := make([]StructuredLinkEdit, 0, len(components))
	for _, component := range components {
		if !component.span.Valid() || component.expected == component.replacement {
			continue
		}
		edits = append(edits, StructuredLinkEdit{
			NotePath: notePath, LinkIndex: linkIndex, Component: component.kind,
			Range:    ontology.ByteRange{Start: component.span.Start, End: component.span.End},
			Expected: component.expected, Replacement: component.replacement,
			TargetOldRef: rewrite.OldRef, TargetNewRef: rewrite.NewRef,
		})
	}
	return edits
}

func replacementLinkPath(authored string, rewrite IdentifierRewrite) string {
	if authored == "" {
		return authored
	}
	if identifierComponentEqual(authored, rewrite.OldIdentifier) {
		return rewrite.NewIdentifier
	}
	if sameCanonicalLinkRef(rewrite.OldRef, rewrite.NewRef) {
		return authored
	}
	if replacement, changed := replaceAuthoredLinkBasename(authored, rewrite.OldRef.NotePath, rewrite.NewRef.NotePath); changed {
		return replacement
	}
	return authored
}

func replaceAuthoredLinkBasename(authored, oldCanonical, newCanonical string) (string, bool) {
	authoredBase := path.Base(strings.ReplaceAll(authored, "\\", "/"))
	oldBase := path.Base(strings.ReplaceAll(strings.TrimSpace(oldCanonical), "\\", "/"))
	newBase := path.Base(strings.ReplaceAll(strings.TrimSpace(newCanonical), "\\", "/"))
	if authoredBase == "" || oldBase == "" || newBase == "" || oldBase == newBase {
		return authored, false
	}
	authoredStem, authoredHasExt := stripMarkdownExtension(authoredBase)
	oldStem, _ := stripMarkdownExtension(oldBase)
	newStem, _ := stripMarkdownExtension(newBase)
	if !strings.EqualFold(authoredStem, oldStem) {
		return authored, false
	}
	writtenNewBase := newBase
	if !authoredHasExt {
		writtenNewBase = newStem
	}
	return strings.TrimSuffix(authored, authoredBase) + writtenNewBase, true
}

func stripMarkdownExtension(value string) (string, bool) {
	if len(value) >= 3 && strings.EqualFold(value[len(value)-3:], ".md") {
		return value[:len(value)-3], true
	}
	return value, false
}

func replacementLinkFragment(authored string, rewrite IdentifierRewrite) string {
	if authored == "" {
		return authored
	}
	oldFragment := strings.TrimPrefix(strings.TrimSpace(rewrite.OldRef.Fragment), "#")
	newFragment := strings.TrimPrefix(strings.TrimSpace(rewrite.NewRef.Fragment), "#")
	if oldFragment != "" && newFragment != "" && identifierComponentEqual(authored, oldFragment) {
		return newFragment
	}
	prefix := ""
	value := authored
	if strings.HasPrefix(value, "^") {
		prefix, value = "^", strings.TrimPrefix(value, "^")
	}
	if identifierComponentEqual(value, rewrite.OldIdentifier) {
		return prefix + rewrite.NewIdentifier
	}
	return authored
}

func replacementLinkDisplay(authored string, rewrite IdentifierRewrite) string {
	if identifierComponentEqual(authored, rewrite.OldIdentifier) {
		return rewrite.NewIdentifier
	}
	if replacement, ok := replaceBoundedIdentifier(authored, rewrite.OldIdentifier, rewrite.NewIdentifier); ok {
		return replacement
	}
	// Human-facing descendant labels conventionally use dots even though the
	// canonical identifier uses hyphens (SPEC-0001.US1). The resolved target
	// makes this substitution semantic rather than a prose-wide text rewrite.
	oldDisplay := DottedIdentifierDisplay(rewrite.OldIdentifier)
	newDisplay := DottedIdentifierDisplay(rewrite.NewIdentifier)
	if replacement, ok := replaceBoundedIdentifier(authored, oldDisplay, newDisplay); ok {
		return replacement
	}
	return authored
}

// DottedIdentifierDisplay renders every suffix-and-ordinal descendant segment
// with the author-facing dot convention, without assuming a fixed schema suffix
// vocabulary. For example, SPEC-0001-US1-TC2 becomes SPEC-0001.US1.TC2.
func DottedIdentifierDisplay(identifier string) string {
	parts := strings.Split(identifier, "-")
	if len(parts) < 2 {
		return identifier
	}
	var display strings.Builder
	display.Grow(len(identifier))
	display.WriteString(parts[0])
	for _, part := range parts[1:] {
		separator := '-'
		if isDerivedDisplaySegment(part) {
			separator = '.'
		}
		display.WriteRune(separator)
		display.WriteString(part)
	}
	return display.String()
}

func isDerivedDisplaySegment(segment string) bool {
	ordinalStart := -1
	for i := 0; i < len(segment); i++ {
		if segment[i] >= '0' && segment[i] <= '9' {
			ordinalStart = i
			break
		}
		if (segment[i] < 'A' || segment[i] > 'Z') && (segment[i] < 'a' || segment[i] > 'z') {
			return false
		}
	}
	if ordinalStart <= 0 {
		return false
	}
	for i := ordinalStart; i < len(segment); i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return false
		}
	}
	return true
}

func identifierComponentEqual(left, right string) bool {
	return ontology.NormalizeIdentifierSemanticValue(left) == ontology.NormalizeIdentifierSemanticValue(right)
}

func linkMentionsAnyRewrite(link obsidian.StructuredLink, rewrites []IdentifierRewrite) bool {
	for _, rewrite := range rewrites {
		for _, value := range []string{link.Path, strings.TrimPrefix(link.Fragment, "^"), link.Display} {
			if identifierComponentEqual(value, rewrite.OldIdentifier) {
				return true
			}
		}
		if replacementLinkPath(link.Path, rewrite) != link.Path ||
			replacementLinkFragment(link.Fragment, rewrite) != link.Fragment ||
			replacementLinkDisplay(link.Display, rewrite) != link.Display {
			return true
		}
	}
	return false
}

func rewritesForCandidates(rewrites []IdentifierRewrite, candidates []ontology.NodeRef) []IdentifierRewrite {
	var matching []IdentifierRewrite
	for _, rewrite := range rewrites {
		for _, candidate := range candidates {
			if sameCanonicalLinkRef(rewrite.OldRef, candidate) {
				matching = append(matching, rewrite)
				break
			}
		}
	}
	return matching
}

func rewritesForRef(rewrites []IdentifierRewrite, ref ontology.NodeRef) []IdentifierRewrite {
	var matching []IdentifierRewrite
	for _, rewrite := range rewrites {
		if sameCanonicalLinkRef(rewrite.OldRef, ref) {
			matching = append(matching, rewrite)
		}
	}
	return matching
}

func rewritesMatchingStructuredLink(rewrites []IdentifierRewrite, link obsidian.StructuredLink) []IdentifierRewrite {
	matching := make([]IdentifierRewrite, 0, len(rewrites))
	for _, rewrite := range rewrites {
		if replacementLinkPath(link.Path, rewrite) != link.Path ||
			replacementLinkFragment(link.Fragment, rewrite) != link.Fragment ||
			replacementLinkDisplay(link.Display, rewrite) != link.Display {
			matching = append(matching, rewrite)
		}
	}
	return matching
}

func canonicalCandidateRefs(input []ontology.NodeRef) []ontology.NodeRef {
	refs := append([]ontology.NodeRef(nil), input...)
	sort.Slice(refs, func(i, j int) bool { return nodeRefTotalSortKey(refs[i]) < nodeRefTotalSortKey(refs[j]) })
	out := refs[:0]
	for _, ref := range refs {
		if len(out) == 0 || nodeRefTotalSortKey(out[len(out)-1]) != nodeRefTotalSortKey(ref) {
			out = append(out, ref)
		}
	}
	return out
}

func sameCanonicalLinkRef(left, right ontology.NodeRef) bool {
	leftKey, rightKey := canonicalLinkRefKey(left), canonicalLinkRefKey(right)
	return leftKey != "" && rightKey != "" && leftKey == rightKey
}

func canonicalLinkRefKey(ref ontology.NodeRef) string {
	return canonicalRefKey(ref)
}

func validCanonicalLinkRefs(refs []ontology.NodeRef) bool {
	for _, ref := range refs {
		if !validCanonicalLinkRef(ref) {
			return false
		}
	}
	return true
}

func validCanonicalLinkRef(ref ontology.NodeRef) bool {
	notePath, err := paths.CleanRelPath(ref.NotePath)
	return err == nil && notePath.String() != "" && canonicalRefKey(ref) != ""
}

func linkRewriteSortKey(rewrite IdentifierRewrite) string {
	return strings.Join([]string{canonicalLinkRefKey(rewrite.OldRef), string(rewrite.Mode), ontology.NormalizeIdentifierSemanticValue(rewrite.OldIdentifier), canonicalLinkRefKey(rewrite.NewRef), rewrite.NewIdentifier}, "\x00")
}

func structuredLinkEditSortKey(edit StructuredLinkEdit) string {
	return fmt.Sprintf("%012d\x00%012d\x00%s\x00%s\x00%s", edit.Range.Start, edit.Range.End, edit.Component, canonicalLinkRefKey(edit.TargetOldRef), edit.Replacement)
}

func linkDiagnostic(notePath string, linkIndex int, link obsidian.StructuredLink, kind LinkRewriteDiagnosticKind, candidates []ontology.NodeRef, message string) LinkRewriteDiagnostic {
	return LinkRewriteDiagnostic{
		Kind: kind, NotePath: notePath, LinkIndex: linkIndex, Target: link.Target,
		Candidates: canonicalCandidateRefs(candidates), Blocking: true, Message: message,
	}
}

func linkDiagnosticSortKey(diagnostic LinkRewriteDiagnostic) string {
	return fmt.Sprintf("%012d\x00%s\x00%s", diagnostic.LinkIndex, diagnostic.Kind, diagnostic.Message)
}
