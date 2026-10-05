package contextpack

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTrimToBudgetUTF8(t *testing.T) {
	for _, tt := range []struct {
		name   string
		text   string
		budget int
		want   string
	}{
		{"accent split", "café more", 4, "caf"},
		{"accent complete", "café more", 5, "café"},
		{"CJK split", "a世界", 3, "a"},
		{"CJK complete", "a世界", 4, "a世"},
		{"emoji split", "a🙂more", 4, "a"},
		{"emoji complete", "a🙂more", 5, "a🙂"},
		{"first rune cannot fit", "🙂more", 3, ""},
		{"trim surrounding space", "  éé  ", 3, "é"},
		{"ASCII prefix", "abcdef", 3, "abc"},
		{"newline preference", "hello\nworld", 9, "hello"},
		{"newline indicator", "café\n" + strings.Repeat("界", 30), 40, "café\n…"},
		{"fits exactly", "café", 5, "café"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimToBudget(tt.text, tt.budget); got != tt.want {
				t.Fatalf("TrimToBudget(%q, %d) = %q; want %q", tt.text, tt.budget, got, tt.want)
			}
		})
	}
}

func TestTrimPreservesUTF8WithinByteBudget(t *testing.T) {
	text := "é界🙂 café" // Exercise every byte boundary in mixed-width text.
	for budget := 0; budget <= len(text)+1; budget++ {
		for name, trim := range map[string]func(string, int) string{
			"text": TrimToBudget,
			"markdown": func(text string, budget int) string {
				out, truncated := TrimMarkdown(text, budget)
				if truncated != (budget < len(text)) {
					t.Errorf("budget %d: truncated = %v", budget, truncated)
				}
				return out
			},
		} {
			got := trim(text, budget)
			if !utf8.ValidString(got) || len(got) > budget || !strings.HasPrefix(text, got) {
				t.Errorf("%s budget %d: invalid or oversized prefix %q (%d bytes)", name, budget, got, len(got))
			}
		}
	}
}

func TestTrimToBudgetDetailedRetainedSourceBytes(t *testing.T) {
	for _, tt := range []struct {
		text     string
		budget   int
		want     string
		retained int
	}{
		{"anything", 0, "", 0},
		{"🙂more", 3, "", 0},
		{"  café  ", 5, "café", 5},
		{"café more", 4, "caf", 3},
		{"hello\nworld", 9, "hello", 5},
		{"café\n" + strings.Repeat("界", 30), 40, "café\n…", 5},
	} {
		got, retained := TrimToBudgetDetailed(tt.text, tt.budget)
		if got != tt.want || retained != tt.retained {
			t.Errorf("budget %d: got %q/%d retained, want %q/%d", tt.budget, got, retained, tt.want, tt.retained)
		}
	}
}
