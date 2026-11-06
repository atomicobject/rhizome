package skilloverlay

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkillOverlayCodeSyntax(t *testing.T) {
	const markers = "<!-- rzm:skill-slot id=\"x\" mode=\"extension\" -->\n<!-- /rzm:skill-slot -->"
	for _, tc := range []struct {
		name, body string
		slots      int
	}{
		{"unmatched backtick", "`\n" + markers, 1},
		{"escaped backtick", "\\`\n" + markers, 1},
		{"indented code", "    <!-- rzm:skill-slot id=\"x\" mode=\"extension\" -->\n    <!-- /rzm:skill-slot -->", 1},
		{"multiline inline code", "`example\n" + markers + "\n`", 1},
		{"false closing fence", "```md\n```text\n" + markers, 1},
		{"nested fence", "  ```md\n" + markers + "\n  ```", 0},
		{"CRLF fence", "~~~md\r\n" + markers + "\r\n~~~\r\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slots, err := parseSkillOverlaySlots("skill", "SKILL.md", tc.body)
			require.NoError(t, err)
			require.Len(t, slots, tc.slots)
			require.Equal(t, tc.slots > 0, hasUnparsedSkillSlotMarker(tc.body))
		})
	}
}
