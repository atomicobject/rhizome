package obsidian

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestRemoveTags(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		tagsToDelete    []string
		expectedContent string
		expectedChanged bool
	}{
		{
			name: "remove frontmatter tag",
			content: `---
title: Test Note
tags: [work, personal]
---
# Test Note
Some content here.`,
			tagsToDelete: []string{"work"},
			expectedContent: `---
tags:
- personal
title: Test Note
---
# Test Note
Some content here.`,
			expectedChanged: true,
		},
		{
			name: "remove last frontmatter tag",
			content: `---
title: Test Note
tags: [work]
---
# Test Note
Some content here.`,
			tagsToDelete: []string{"work"},
			expectedContent: `---
title: Test Note
---
# Test Note
Some content here.`,
			expectedChanged: true,
		},
		{
			name: "remove entire frontmatter when only tags exist",
			content: `---
tags: [work]
---
# Test Note
Some content here.`,
			tagsToDelete: []string{"work"},
			expectedContent: `# Test Note
Some content here.`,
			expectedChanged: true,
		},
		{
			name:            "remove hashtag",
			content:         "# Test Note\nThis is about #work and other things.\nMore content here.",
			tagsToDelete:    []string{"work"},
			expectedContent: "# Test Note\nThis is about and other things.\nMore content here.",
			expectedChanged: true,
		},
		{
			name:            "ignore hashtags in code blocks",
			content:         "# Test Note\nThis is about #work.\n\n```\n#work should not be deleted here\n```\n\nMore #work content.",
			tagsToDelete:    []string{"work"},
			expectedContent: "# Test Note\nThis is about.\n\n```\n#work should not be deleted here\n```\n\nMore content.",
			expectedChanged: true,
		},
		{
			name:            "ignore hashtags in inline code",
			content:         "# Test Note\nUse `#work` snippet and #work outside.",
			tagsToDelete:    []string{"work"},
			expectedContent: "# Test Note\nUse `#work` snippet and outside.",
			expectedChanged: true,
		},
		{
			name:            "no changes when tag not found",
			content:         "# Test Note\nThis is about #other things.",
			tagsToDelete:    []string{"work"},
			expectedContent: "# Test Note\nThis is about #other things.",
			expectedChanged: false,
		},
		{
			name:            "remove single hashtag",
			content:         "This is about #work and other things.",
			tagsToDelete:    []string{"work"},
			expectedContent: "This is about and other things.",
			expectedChanged: true,
		},
		{
			name:            "remove multiple hashtags",
			content:         "This is #work and #urgent stuff.",
			tagsToDelete:    []string{"work", "urgent"},
			expectedContent: "This is and stuff.",
			expectedChanged: true,
		},
		{
			name:            "remove hashtag at beginning",
			content:         "#work is important today.",
			tagsToDelete:    []string{"work"},
			expectedContent: "is important today.",
			expectedChanged: true,
		},
		{
			name:            "remove hashtag at end",
			content:         "Today I'm focusing on #work",
			tagsToDelete:    []string{"work"},
			expectedContent: "Today I'm focusing on",
			expectedChanged: true,
		},
		{
			name:            "case insensitive matching",
			content:         "This is about #Work and other things.",
			tagsToDelete:    []string{"work"},
			expectedContent: "This is about and other things.",
			expectedChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, changed := RemoveTags(tt.content, tt.tagsToDelete)

			assert.Equal(t, tt.expectedChanged, changed, "Changed flag should match expected")

			assertTagEditResult(t, tt.expectedContent, result)
		})
	}
}

func TestReplaceTags(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		fromTags        []string
		toTag           string
		expectedContent string
		expectedChanged bool
	}{
		{
			name: "replace frontmatter tag",
			content: `---
title: Test Note
tags: [work, personal]
---
# Test Note
Some content here.`,
			fromTags: []string{"work"},
			toTag:    "office",
			expectedContent: `---
tags:
- office
- personal
title: Test Note
---
# Test Note
Some content here.`,
			expectedChanged: true,
		},
		{
			name:            "replace hashtag",
			content:         "# Test Note\nThis is about #work and other things.\nMore #work content here.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "# Test Note\nThis is about #office and other things.\nMore #office content here.",
			expectedChanged: true,
		},
		{
			name: "replace multiple tags to same destination",
			content: `---
tags: [work, personal, job]
---
# Test Note
Some content here.`,
			fromTags: []string{"work", "job"},
			toTag:    "office",
			expectedContent: `---
tags:
- office
- personal
---
# Test Note
Some content here.`,
			expectedChanged: true,
		},
		{
			name:            "preserve both prose tag occurrences",
			content:         "# Test Note\nThis is about #work and #office.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "# Test Note\nThis is about #office and #office.",
			expectedChanged: true,
		},
		{
			name:            "ignore hashtags in code blocks",
			content:         "# Test Note\nThis is about #work.\n\n```\n#work should not be renamed here\n```\n\nMore #work content.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "# Test Note\nThis is about #office.\n\n```\n#work should not be renamed here\n```\n\nMore #office content.",
			expectedChanged: true,
		},
		{
			name:            "ignore hashtags in inline code",
			content:         "# Test Note\nUse `#work` snippet and #work outside.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "# Test Note\nUse `#work` snippet and #office outside.",
			expectedChanged: true,
		},
		{
			name:            "no changes when tag not found",
			content:         "# Test Note\nThis is about #other things.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "# Test Note\nThis is about #other things.",
			expectedChanged: false,
		},
		{
			name: "hierarchical frontmatter rename",
			content: `---
tags: [a/b, a/b/c, personal]
---
# Test Note
Some content here.`,
			fromTags: []string{"a"},
			toTag:    "project",
			expectedContent: `---
tags:
- project/b
- project/b/c
- personal
---
# Test Note
Some content here.`,
			expectedChanged: true,
		},
		{
			name:            "hierarchical hashtag rename",
			content:         "# Test Note\nThis is about #a/b and #a/b/c but not #ab or #abc.",
			fromTags:        []string{"a"},
			toTag:           "project",
			expectedContent: "# Test Note\nThis is about #project/b and #project/b/c but not #ab or #abc.",
			expectedChanged: true,
		},
		{
			name:            "hierarchical rename preserves case in suffix",
			content:         "# Test Note\nTags: #Work/Important and #work/URGENT.",
			fromTags:        []string{"work"},
			toTag:           "project",
			expectedContent: "# Test Note\nTags: #project/Important and #project/URGENT.",
			expectedChanged: true,
		},
		{
			name:            "no false positive matches",
			content:         "# Test Note\nTags: #work #workplace #working #workday but also #work/sub.",
			fromTags:        []string{"work"},
			toTag:           "job",
			expectedContent: "# Test Note\nTags: #job #workplace #working #workday but also #job/sub.",
			expectedChanged: true,
		},
		{
			name:            "multiple hierarchical renames",
			content:         "# Test Note\nTags: #a/x #b/y #a/z #other.",
			fromTags:        []string{"a", "b"},
			toTag:           "project",
			expectedContent: "# Test Note\nTags: #project/x #project/y #project/z #other.",
			expectedChanged: true,
		},
		{
			name: "mixed frontmatter and hashtag hierarchical",
			content: `---
tags: [work/frontend, work/backend]
---
# Test Note
Also discussing #work/testing and #other/stuff.`,
			fromTags: []string{"work"},
			toTag:    "project",
			expectedContent: `---
tags:
- project/frontend
- project/backend
---
# Test Note
Also discussing #project/testing and #other/stuff.`,
			expectedChanged: true,
		},
		{
			name:            "replace single hashtag",
			content:         "This is about #work and other things.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "This is about #office and other things.",
			expectedChanged: true,
		},
		{
			name:            "replace multiple hashtags",
			content:         "This is #work and #job stuff.",
			fromTags:        []string{"work", "job"},
			toTag:           "office",
			expectedContent: "This is #office and #office stuff.",
			expectedChanged: true,
		},
		{
			name:            "replace hashtag at beginning",
			content:         "#work is important today.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "#office is important today.",
			expectedChanged: true,
		},
		{
			name:            "replace hashtag at end",
			content:         "Today I'm focusing on #work",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "Today I'm focusing on #office",
			expectedChanged: true,
		},
		{
			name:            "no change when tag not found",
			content:         "This is about #other things.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "This is about #other things.",
			expectedChanged: false,
		},
		{
			name:            "case insensitive matching",
			content:         "This is about #Work and other things.",
			fromTags:        []string{"work"},
			toTag:           "office",
			expectedContent: "This is about #office and other things.",
			expectedChanged: true,
		},
		{
			name:            "hierarchical hashtag rename",
			content:         "Tags: #work/frontend #work/backend #other",
			fromTags:        []string{"work"},
			toTag:           "project",
			expectedContent: "Tags: #project/frontend #project/backend #other",
			expectedChanged: true,
		},
		{
			name:            "no false positive on similar tags",
			content:         "Tags: #work #workplace #working #workday #work/sub",
			fromTags:        []string{"work"},
			toTag:           "job",
			expectedContent: "Tags: #job #workplace #working #workday #job/sub",
			expectedChanged: true,
		},
		{
			name:            "deep hierarchical rename",
			content:         "Deep: #a/b/c/d/e should become project",
			fromTags:        []string{"a"},
			toTag:           "project",
			expectedContent: "Deep: #project/b/c/d/e should become project",
			expectedChanged: true,
		},
		{
			name:            "case preservation in hierarchical suffix",
			content:         "Mixed: #Work/Important/URGENT",
			fromTags:        []string{"work"},
			toTag:           "project",
			expectedContent: "Mixed: #project/Important/URGENT",
			expectedChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, changed := ReplaceTags(tt.content, tt.fromTags, tt.toTag)

			assert.Equal(t, tt.expectedChanged, changed, "Changed flag should match expected")

			assertTagEditResult(t, tt.expectedContent, result)
		})
	}
}

// Compare frontmatter as YAML while preserving the authored body exactly.
func assertTagEditResult(t *testing.T, want, got string) {
	t.Helper()
	wantYAML, wantBody, wantFrontmatter := splitTagEditFrontmatter(want)
	gotYAML, gotBody, gotFrontmatter := splitTagEditFrontmatter(got)
	assert.Equal(t, wantFrontmatter, gotFrontmatter)
	assert.Equal(t, wantBody, gotBody)
	if wantFrontmatter {
		var wantFields, gotFields map[string]any
		if assert.NoError(t, yaml.Unmarshal([]byte(wantYAML), &wantFields)) && assert.NoError(t, yaml.Unmarshal([]byte(gotYAML), &gotFields)) {
			assert.Equal(t, wantFields, gotFields)
		}
	}
}

func splitTagEditFrontmatter(content string) (string, string, bool) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content, false
	}
	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) != 3 {
		return "", content, false
	}
	return parts[1], parts[2], true
}
