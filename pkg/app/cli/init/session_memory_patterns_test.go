package init

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

// The session-memory scripts match applies-to patterns in JavaScript, and
// Rhizome matches the same patterns as glob code anchors. Both read this case
// table; the Node side runs in the starter's script tests.
func TestSessionMemoryPatternsAgreeWithDoublestar(t *testing.T) {
	raw, err := os.ReadFile("testdata/session-memory/patterns.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases [][3]any
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		pattern, path, want := c[0].(string), c[1].(string), c[2].(bool)
		got, err := doublestar.Match(pattern, path)
		if err != nil || got != want {
			t.Errorf("doublestar.Match(%q, %q) = %v, %v; the case table says %v", pattern, path, got, err, want)
		}
	}
}
