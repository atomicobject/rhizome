package init

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandAgentTemplateEmbedsCodeSyntax(t *testing.T) {
	const embed = "![[missing-characterization-template]]"
	for _, tc := range []struct {
		name, body string
		protected  bool
	}{
		{"unmatched backtick", "`\n" + embed, true},
		{"escaped backtick", "\\`" + embed, true},
		{"indented code", "    " + embed, false},
		{"multiline inline code", "`example\n" + embed + "\n`", true},
		{"false closing fence", "```md\n```text\n" + embed, false},
		{"nested fence", "  ```md\n" + embed + "\n  ```", true},
		{"CRLF fence", "~~~md\r\n" + embed + "\r\n~~~\r\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := expandAgentTemplateEmbeds(tc.body)
			if tc.protected {
				require.NoError(t, err)
				require.Equal(t, tc.body, out)
			} else {
				require.ErrorContains(t, err, "missing-characterization-template")
			}
		})
	}
}
