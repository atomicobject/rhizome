package embeddings

import (
	"strings"
	"testing"
)

func TestSummarizeFrontmatterPriority(t *testing.T) {
	fm := map[string]interface{}{
		"zzz":         "last alphabetically",
		"aaa":         "first alphabetically",
		"tags":        []interface{}{"foo", "bar"},
		"summary":     "This is the summary",
		"description": "This is the description",
		"status":      "draft",
	}

	result := summarizeFrontmatter(fm)

	// summary should come before description (priority order)
	summaryIdx := strings.Index(result, "summary=")
	descIdx := strings.Index(result, "description=")
	tagsIdx := strings.Index(result, "tags=")

	if summaryIdx == -1 {
		t.Error("summary not found in result")
	}
	if descIdx == -1 {
		t.Error("description not found in result")
	}
	if tagsIdx == -1 {
		t.Error("tags not found in result")
	}

	// Check priority order: summary < description < tags < others
	if summaryIdx > descIdx {
		t.Errorf("summary should come before description: %s", result)
	}
	if descIdx > tagsIdx {
		t.Errorf("description should come before tags: %s", result)
	}

	// Only blessed keys should be included; unblessed keys are omitted.
	if strings.Contains(result, "aaa=") {
		t.Errorf("unblessed key aaa should not be included: %s", result)
	}
	if strings.Contains(result, "zzz=") {
		t.Errorf("unblessed key zzz should not be included: %s", result)
	}
	if strings.Contains(result, "status=") {
		t.Errorf("unblessed key status should not be included: %s", result)
	}

	t.Logf("Result: %s", result)
}

func TestSummarizeFrontmatterCaseInsensitive(t *testing.T) {
	// YAML preserves original key casing, but users write "Summary" or "summary" interchangeably.
	// The function should find keys case-insensitively.
	fm := map[string]interface{}{
		"Summary":     "Capitalized summary",
		"DESCRIPTION": "ALL CAPS description",
		"Tags":        []interface{}{"foo", "bar"},
	}

	result := summarizeFrontmatter(fm)

	// All should be found despite case differences
	if !strings.Contains(result, "summary=Capitalized summary") {
		t.Errorf("capitalized Summary key should be found: %s", result)
	}
	if !strings.Contains(result, "description=ALL CAPS description") {
		t.Errorf("uppercase DESCRIPTION key should be found: %s", result)
	}
	if !strings.Contains(result, "tags=foo,bar") {
		t.Errorf("mixed-case Tags key should be found: %s", result)
	}

	t.Logf("Result: %s", result)
}

func TestSummarizeFrontmatterStableOutput(t *testing.T) {
	// The output should be deterministic regardless of Go's map iteration order.
	fm := map[string]interface{}{
		"tags":        []interface{}{"a", "b"},
		"description": "desc",
		"summary":     "sum",
	}

	// Run multiple times to catch any iteration order issues.
	first := summarizeFrontmatter(fm)
	for i := 0; i < 10; i++ {
		result := summarizeFrontmatter(fm)
		if result != first {
			t.Errorf("output should be stable: got %q then %q", first, result)
		}
	}
}
