package structured

import "fmt"

// Persist locations in the diagnostic text, which every existing diagnostic
// surface retains. A known start position does not establish an exact range.
func diagnosticMessage(host string, issue Diagnostic) string {
	message := host
	if issue.Start.Line > 0 {
		message += fmt.Sprintf(":%d:%d (byte %d)", issue.Start.Line, issue.Start.Column, issue.Start.Offset)
	}
	message += fmt.Sprintf(" at %q: %s", issue.Address, issue.Message)
	for _, related := range issue.Related {
		message += fmt.Sprintf("; first occurrence %s:%d:%d (byte %d)", host, related.Line, related.Column, related.Offset)
	}
	return message
}
