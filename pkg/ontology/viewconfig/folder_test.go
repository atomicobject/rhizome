package viewconfig

import "testing"

func TestServablePathKeepsDefinitionsAndPrivateFilesAway(t *testing.T) {
	for rel, want := range map[string]bool{
		"poc/board.tsx":                 true,
		"poc/components/Card.tsx":       true,
		"poc/data.json":                 true,
		"poc/board.yaml":                false,
		"poc/other.YML":                 false,
		"poc/.env":                      false,
		"poc/.git/config":               false,
		".hidden/board.tsx":             false,
		"poc/node_modules/pkg/index.js": false,
		"poc//board.tsx":                false,
		"../secret.txt":                 false,
	} {
		if got := ServablePath(rel); got != want {
			t.Errorf("ServablePath(%q) = %v, want %v", rel, got, want)
		}
	}
}
