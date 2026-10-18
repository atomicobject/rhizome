package validate

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestClassifyLifecycleEditProtectsHistoricalPlanButAllowsLinkRepair(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nSee [[old-target|context]].\n")
	targetStart := bytes.Index(before, []byte("old-target"))
	protected := ClassifyLifecycleEdit(LifecycleEdit{
		TypeName:     "EffortNote",
		EffortStatus: "complete",
		Before:       before,
		After:        []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nChanged.\n"),
	})
	allowed := ClassifyLifecycleEdit(LifecycleEdit{
		TypeName:     "EffortNote",
		EffortStatus: "complete",
		Before:       before,
		After:        []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nSee [[new-target|context]].\n"),
		Claims: []LifecycleClaim{{
			Kind:         LifecycleEditBrokenLink,
			StartByte:    targetStart,
			EndByte:      targetStart + len("old-target"),
			ExpectedText: "old-target",
			Replacement:  "new-target",
		}},
	})

	assert.Equal(t, LifecycleProtected, protected.Decision)
	assert.Equal(t, LifecycleAllowed, allowed.Decision)
}

func TestClassifyLifecycleEditDerivesHistoricalStateAndRejectsSpoofedMetadata(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nBefore.\n")
	after := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nAfter.\n")

	derived := ClassifyLifecycleEdit(LifecycleEdit{Before: before, After: after})
	spoofed := ClassifyLifecycleEdit(LifecycleEdit{
		TypeName:     "ReferenceDoc",
		EffortStatus: "active",
		Before:       before,
		After:        after,
	})

	assert.Equal(t, LifecycleProtected, derived.Decision)
	assert.Equal(t, LifecycleProtected, spoofed.Decision)
}

func TestClassifyLifecycleEditRejectsBrokenLinkReplacementInjection(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nSee [[old-target|context]].\n")
	start := bytes.Index(before, []byte("old-target"))
	replacement := "new-target]]\nProtected rewrite"
	after := append([]byte(nil), before[:start]...)
	after = append(after, replacement...)
	after = append(after, before[start+len("old-target"):]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind:         LifecycleEditBrokenLink,
			StartByte:    start,
			EndByte:      start + len("old-target"),
			ExpectedText: "old-target",
			Replacement:  replacement,
		}},
	})

	assert.Equal(t, LifecycleProtected, result.Decision)
}

func TestClassifyLifecycleEditRejectsBodyStatusSpoof(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Notes\n\nstatus: complete\n")
	start := bytes.LastIndex(before, []byte("complete"))
	after := append([]byte(nil), before[:start]...)
	after = append(after, "archived"...)
	after = append(after, before[start+len("complete"):]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind:         LifecycleEditArchiveStatus,
			StartByte:    start,
			EndByte:      start + len("complete"),
			ExpectedText: "complete",
			Replacement:  "archived",
		}},
	})

	assert.Equal(t, LifecycleProtected, result.Decision)
}

func TestClassifyLifecycleEditRejectsEmbeddedHistoryHeading(t *testing.T) {
	before := []byte("---\r\ntype: EffortNote\r\nstatus: complete\r\n---\r\n# Effort\r\n\r\nParagraph mentions ## Deviations but has no section.\r\n\r\n## Status\r\n\r\nComplete.\r\n")
	insertAt := bytes.Index(before, []byte("## Status"))
	replacement := "- (post-closure: audit) must not authorize fake headings\r\n"
	after := append([]byte(nil), before[:insertAt]...)
	after = append(after, replacement...)
	after = append(after, before[insertAt:]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind: LifecycleEditAppendHistory, Section: "Deviations",
			StartByte: insertAt, EndByte: insertAt, Replacement: replacement,
		}},
	})

	assert.Equal(t, LifecycleProtected, result.Decision)
}

func TestClassifyLifecycleEditRejectsFencedHistoryHeading(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n```md\n## Deviations\n```\n\n## Status\n\nComplete.\n")
	insertAt := bytes.Index(before, []byte("## Status"))
	replacement := "- (post-closure: audit) must not authorize fenced headings\n"
	after := append([]byte(nil), before[:insertAt]...)
	after = append(after, replacement...)
	after = append(after, before[insertAt:]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind: LifecycleEditAppendHistory, Section: "Deviations",
			StartByte: insertAt, EndByte: insertAt, Replacement: replacement,
		}},
	})

	assert.Equal(t, LifecycleProtected, result.Decision)
}

func TestClassifyLifecycleEditAllowsHistoryAppendAtRealCRLFSectionEnd(t *testing.T) {
	before := []byte("---\r\ntype: EffortNote\r\nstatus: complete\r\n---\r\n# Effort\r\n\r\n## Deviations\r\n\r\nNone.\r\n\r\n##\tStatus\r\n\r\nComplete.\r\n")
	insertAt := bytes.Index(before, []byte("##\tStatus"))
	replacement := "- (post-closure: audit) approved correction\r\n"
	after := append([]byte(nil), before[:insertAt]...)
	after = append(after, replacement...)
	after = append(after, before[insertAt:]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind: LifecycleEditAppendHistory, Section: "Deviations",
			StartByte: insertAt, EndByte: insertAt, Replacement: replacement,
		}},
	})

	assert.Equal(t, LifecycleAllowed, result.Decision)
}

func TestClassifyLifecycleEditAllowsHistoryAppendAtSectionEOF(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Execution Notes\n\nExisting.\n")
	replacement := "- (post-closure: audit) approved correction\n"
	after := append(append([]byte(nil), before...), replacement...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind: LifecycleEditAppendHistory, Section: "Execution Notes",
			StartByte: len(before), EndByte: len(before), Replacement: replacement,
		}},
	})

	assert.Equal(t, LifecycleAllowed, result.Decision)
}
