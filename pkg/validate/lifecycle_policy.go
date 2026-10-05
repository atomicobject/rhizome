package validate

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// LifecycleDecision is the plan-time classification for one repair operation.
type LifecycleDecision string

const (
	LifecycleNotHistorical LifecycleDecision = "not_historical"
	LifecycleAllowed       LifecycleDecision = "allowed"
	LifecycleProtected     LifecycleDecision = "protected"
)

// LifecycleEditKind identifies a claimed post-closure exception.
type LifecycleEditKind string

const (
	LifecycleEditContent       LifecycleEditKind = "content"
	LifecycleEditAppendHistory LifecycleEditKind = "append_history"
	LifecycleEditBrokenLink    LifecycleEditKind = "broken_link"
	LifecycleEditArchiveStatus LifecycleEditKind = "archive_status"
)

// LifecycleClaim covers one exact before/after hunk. Every output hunk must be
// covered by a validated claim or a historical edit is protected.
type LifecycleClaim struct {
	Kind         LifecycleEditKind `json:"kind"`
	Section      string            `json:"section,omitempty"`
	StartByte    int               `json:"startByte"`
	EndByte      int               `json:"endByte"`
	ExpectedText string            `json:"expectedText,omitempty"`
	Replacement  string            `json:"replacement,omitempty"`
}

// LifecycleEdit provides raw source evidence for lifecycle classification.
type LifecycleEdit struct {
	TypeName     string           `json:"typeName,omitempty"`
	EffortStatus string           `json:"effortStatus,omitempty"`
	Before       []byte           `json:"before,omitempty"`
	After        []byte           `json:"after,omitempty"`
	Claims       []LifecycleClaim `json:"claims,omitempty"`
}

// LifecycleRange identifies bytes that remain protected.
type LifecycleRange struct {
	StartByte int `json:"startByte"`
	EndByte   int `json:"endByte"`
}

// LifecyclePolicyResult records the decision persisted on a repair operation.
type LifecyclePolicyResult struct {
	Decision        LifecycleDecision `json:"decision,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	ProtectedRanges []LifecycleRange  `json:"protectedRanges,omitempty"`
}

// ClassifyLifecycleEdit enforces SPEC-0051 from raw before/after bytes. Claims
// are evidence, not authority: they must reconstruct the entire output and each
// claimed exception is independently validated.
func ClassifyLifecycleEdit(edit LifecycleEdit) LifecyclePolicyResult {
	frontmatter, err := obsidian.ExtractFrontmatter(string(edit.Before))
	if err != nil {
		return protectedLifecycleResult(len(edit.Before), "historical source frontmatter is invalid")
	}
	typeName := frontmatterString(frontmatter, "type")
	status := strings.ToLower(frontmatterString(frontmatter, "status"))
	if supplied := strings.TrimSpace(edit.TypeName); supplied != "" && supplied != typeName {
		return protectedLifecycleResult(len(edit.Before), "lifecycle type metadata does not match source")
	}
	if supplied := strings.ToLower(strings.TrimSpace(edit.EffortStatus)); supplied != "" && supplied != status {
		return protectedLifecycleResult(len(edit.Before), "lifecycle status metadata does not match source")
	}
	if typeName != "EffortNote" || (status != "complete" && status != "archived") {
		return LifecyclePolicyResult{Decision: LifecycleNotHistorical}
	}
	if bytes.Equal(edit.Before, edit.After) {
		return LifecyclePolicyResult{Decision: LifecycleAllowed}
	}
	claims := append([]LifecycleClaim(nil), edit.Claims...)
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].StartByte != claims[j].StartByte {
			return claims[i].StartByte < claims[j].StartByte
		}
		return claims[i].EndByte < claims[j].EndByte
	})
	if err := validateLifecycleClaims(edit.Before, edit.After, claims); err != nil {
		return protectedLifecycleResult(len(edit.Before), err.Error())
	}
	for _, claim := range claims {
		var err error
		switch claim.Kind {
		case LifecycleEditBrokenLink:
			err = validateBrokenLinkClaim(edit.Before, claim)
		case LifecycleEditArchiveStatus:
			err = validateArchiveStatusClaim(status, edit.Before, claim)
		case LifecycleEditAppendHistory:
			err = validateHistoryAppendClaim(edit.Before, claim)
		default:
			err = fmt.Errorf("historical content edit has no allowed exception")
		}
		if err != nil {
			return protectedLifecycleResult(len(edit.Before), err.Error())
		}
	}
	return LifecyclePolicyResult{Decision: LifecycleAllowed}
}

func validateLifecycleClaims(before, after []byte, claims []LifecycleClaim) error {
	if len(claims) == 0 {
		return fmt.Errorf("historical edit has uncovered output changes")
	}
	var rebuilt bytes.Buffer
	cursor := 0
	for _, claim := range claims {
		if claim.StartByte < cursor || claim.EndByte < claim.StartByte || claim.EndByte > len(before) {
			return fmt.Errorf("historical edit claims overlap or exceed source")
		}
		if string(before[claim.StartByte:claim.EndByte]) != claim.ExpectedText {
			return fmt.Errorf("historical edit claim expected text is stale")
		}
		rebuilt.Write(before[cursor:claim.StartByte])
		rebuilt.WriteString(claim.Replacement)
		cursor = claim.EndByte
	}
	rebuilt.Write(before[cursor:])
	if !bytes.Equal(rebuilt.Bytes(), after) {
		return fmt.Errorf("historical edit has uncovered output changes")
	}
	return nil
}

func validateBrokenLinkClaim(before []byte, claim LifecycleClaim) error {
	if claim.StartByte == claim.EndByte || claim.Replacement == "" {
		return fmt.Errorf("broken-link repair must replace one target")
	}
	if strings.ContainsAny(claim.Replacement, "\r\n") {
		return fmt.Errorf("broken-link replacement cannot contain a newline")
	}
	prefix := before[:claim.StartByte]
	suffix := before[claim.EndByte:]
	wikiStart := bytes.LastIndex(prefix, []byte("[["))
	wikiEnd := bytes.Index(suffix, []byte("]]"))
	if wikiStart >= 0 && wikiEnd >= 0 {
		targetPrefix := prefix[wikiStart+2:]
		targetSuffix := suffix[:wikiEnd]
		if len(targetPrefix) == 0 && (len(targetSuffix) == 0 || targetSuffix[0] == '|' || targetSuffix[0] == '#') {
			if strings.ContainsAny(claim.Replacement, "[]|") {
				return fmt.Errorf("wikilink replacement escapes the target")
			}
			return nil
		}
	}
	markdownStart := bytes.LastIndex(prefix, []byte("]("))
	markdownEnd := bytes.IndexByte(suffix, ')')
	if markdownStart >= 0 && markdownEnd >= 0 && len(prefix[markdownStart+2:]) == 0 && markdownEnd == 0 {
		if strings.ContainsAny(claim.Replacement, "()") {
			return fmt.Errorf("markdown link replacement escapes the target")
		}
		return nil
	}
	return fmt.Errorf("broken-link repair changes more than the link target")
}

func frontmatterString(frontmatter map[string]interface{}, key string) string {
	for candidate, value := range frontmatter {
		if !strings.EqualFold(strings.TrimSpace(candidate), key) {
			continue
		}
		text, _ := value.(string)
		return strings.TrimSpace(text)
	}
	return ""
}

func validateArchiveStatusClaim(status string, before []byte, claim LifecycleClaim) error {
	if status != "complete" || claim.ExpectedText != "complete" || claim.Replacement != "archived" {
		return fmt.Errorf("only complete to archived is allowed after closure")
	}
	start, end, ok := frontmatterScalarRange(before, "status")
	if !ok || claim.StartByte != start || claim.EndByte != end {
		return fmt.Errorf("archive transition must target the status field")
	}
	return nil
}

func frontmatterScalarRange(content []byte, key string) (int, int, bool) {
	if !bytes.HasPrefix(content, []byte("---")) {
		return 0, 0, false
	}
	openEnd := bytes.IndexByte(content, '\n')
	if openEnd < 0 {
		return 0, 0, false
	}
	closeOffset := bytes.Index(content[openEnd+1:], []byte("\n---"))
	if closeOffset < 0 {
		return 0, 0, false
	}
	frontmatterEnd := openEnd + 1 + closeOffset
	cursor := openEnd + 1
	for cursor < frontmatterEnd {
		lineEnd := bytes.IndexByte(content[cursor:frontmatterEnd], '\n')
		if lineEnd < 0 {
			lineEnd = frontmatterEnd - cursor
		}
		line := content[cursor : cursor+lineEnd]
		colon := bytes.IndexByte(line, ':')
		if colon >= 0 && strings.EqualFold(strings.TrimSpace(string(line[:colon])), key) {
			valueStart := cursor + colon + 1
			for valueStart < cursor+lineEnd && (content[valueStart] == ' ' || content[valueStart] == '\t') {
				valueStart++
			}
			valueEnd := cursor + lineEnd
			for valueEnd > valueStart && (content[valueEnd-1] == ' ' || content[valueEnd-1] == '\t' || content[valueEnd-1] == '\r') {
				valueEnd--
			}
			return valueStart, valueEnd, true
		}
		cursor += lineEnd + 1
	}
	return 0, 0, false
}

func validateHistoryAppendClaim(before []byte, claim LifecycleClaim) error {
	section := strings.TrimSpace(claim.Section)
	switch section {
	case "Execution Notes", "Deviations", "Compounding Follow-ups":
	default:
		return fmt.Errorf("historical append targets a protected section")
	}
	if claim.StartByte != claim.EndByte || claim.ExpectedText != "" {
		return fmt.Errorf("historical note entries must be append-only")
	}
	sectionEnd, ok := lifecycleHistorySectionEnd(before, section)
	if !ok {
		return fmt.Errorf("historical append section is missing")
	}
	if claim.StartByte != sectionEnd {
		return fmt.Errorf("historical note entries must append at section end")
	}
	for _, line := range strings.Split(strings.TrimSpace(claim.Replacement), "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "(post-closure: ") {
			return fmt.Errorf("historical note entries require post-closure provenance")
		}
	}
	return nil
}

func lifecycleHistorySectionEnd(content []byte, section string) (int, bool) {
	target := []byte("## " + section)
	found := false
	for _, heading := range obsidian.EnumerateMarkdownTargets(string(content)) {
		if heading.Kind != obsidian.MarkdownTargetHeading || heading.Level != 2 {
			continue
		}
		if found {
			return heading.StartByte, true
		}
		found = bytes.Equal(content[heading.StartByte:heading.EndByte], target)
	}
	if found {
		return len(content), true
	}
	return 0, false
}

func protectedLifecycleResult(length int, reason string) LifecyclePolicyResult {
	return LifecyclePolicyResult{
		Decision: LifecycleProtected,
		Reason:   reason,
		ProtectedRanges: []LifecycleRange{{
			StartByte: 0,
			EndByte:   length,
		}},
	}
}
