package viewconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCustomView(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const customViewYAML = `apiVersion: rhizome.view.v1
id: poc.board
name: Board
source:
  kind: custom
  entry: ENTRY
mount:
  kind: standalone
  group: Custom
`

func issueCodes(issues []Issue) map[string]string {
	codes := map[string]string{}
	for _, issue := range issues {
		codes[issue.Code] = issue.Field
	}
	return codes
}

func TestCustomViewLoadsAndValidates(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "poc")
	writeCustomView(t, app, "board.yaml", replaceEntry("board.tsx"))
	writeCustomView(t, app, "board.tsx", "export default () => null")
	// Definitions inside node_modules belong to packages, not this vault.
	writeCustomView(t, filepath.Join(app, "node_modules", "pkg"), "view.yaml", "id: nope")

	views, issues := LoadPath(root)
	if len(issues) != 0 || len(views) != 1 {
		t.Fatalf("load: views=%d issues=%v", len(views), issues)
	}
	result := Validate(views, ValidateOptions{})
	if len(result.Issues) != 0 {
		t.Fatalf("unexpected issues: %v", result.Issues)
	}
	got := result.Views[0]
	if got.EntryPath() != filepath.Join(app, "board.tsx") || got.Defaults.Variant != "" {
		t.Fatalf("entry=%q defaults=%+v", got.EntryPath(), got.Defaults)
	}
}

func TestCustomViewRejectsBadDefinitions(t *testing.T) {
	cases := map[string]struct {
		yaml string
		code string
	}{
		"escape":        {replaceEntry("../other/board.tsx"), "invalid_custom_entry"},
		"absolute":      {replaceEntry("/etc/passwd.js"), "invalid_custom_entry"},
		"extension":     {replaceEntry("board.md"), "invalid_custom_entry"},
		"private entry": {replaceEntry(".build/board.tsx"), "invalid_custom_entry"},
		"missing file":  {replaceEntry("absent.tsx"), "custom_entry_not_found"},
		"missing entry": {replaceEntry(`""`), "missing_required_field"},
		"defaults":      {replaceEntry("board.tsx") + "defaults:\n  first: 10\n", "unexpected_field"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeCustomView(t, root, "board.yaml", tc.yaml)
			writeCustomView(t, root, "board.tsx", "export default () => null")
			views, _ := LoadPath(root)
			codes := issueCodes(Validate(views, ValidateOptions{}).Issues)
			if _, ok := codes[tc.code]; !ok {
				t.Fatalf("want %s, got %v", tc.code, codes)
			}
		})
	}
}

func TestCustomViewAcceptsMountedContexts(t *testing.T) {
	for _, mount := range []MountSpec{
		{Kind: MountKindType, Type: "Note"}, {Kind: MountKindInterface, Interface: "Collection"},
		{Kind: MountKindGroup, Group: "Delivery"}, {Kind: MountKindGroup, Group: "*"}, {Kind: MountKindNode, Type: "Note"},
	} {
		def := ViewDefinition{APIVersion: APIVersion, ID: "v", Name: "V", SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "v.tsx"}, Mount: mount}
		if issues := Validate([]ViewDefinition{def}, ValidateOptions{}).Issues; len(issues) != 0 {
			t.Fatalf("mount %+v: %v", mount, issues)
		}
	}
}

func replaceEntry(entry string) string {
	return strings.Replace(customViewYAML, "ENTRY", entry, 1)
}

func TestCustomViewRejectsEntrySymlinkedOutOfItsFolder(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "board.tsx")
	writeCustomView(t, filepath.Dir(outside), "board.tsx", "export default null")
	writeCustomView(t, root, "board.yaml", replaceEntry("board.tsx"))
	if err := os.Symlink(outside, filepath.Join(root, "board.tsx")); err != nil {
		t.Skip("symlinks unavailable")
	}
	views, _ := LoadPath(root)
	if field, ok := issueCodes(Validate(views, ValidateOptions{}).Issues)["custom_entry_not_found"]; !ok || field != "source.entry" {
		t.Fatalf("a symlinked entry outside the folder must fail validation")
	}
}

func TestCustomConfigurationLoadsJSONValues(t *testing.T) {
	root := t.TempDir()
	writeCustomView(t, root, "board.tsx", "export default () => null")
	writeCustomView(t, root, "view.yaml", replaceEntry("board.tsx")+"configuration:\n  heading: Delivery\n  limit: 12\n  showGraph: true\n  columns: [title, status]\n")
	defs, issues := LoadPath(root)
	if len(issues) != 0 || len(defs) != 1 {
		t.Fatalf("load %v", issues)
	}
	result := Validate(defs, ValidateOptions{})
	if len(result.Issues) != 0 {
		t.Fatalf("validate %v", result.Issues)
	}
	if result.Views[0].Configuration["heading"] != "Delivery" {
		t.Fatalf("configuration %+v", result.Views[0].Configuration)
	}
}

func TestAuthoredGeneratedFlagIsIgnored(t *testing.T) {
	root := t.TempDir()
	writeCustomView(t, root, "board.tsx", "export default () => null")
	writeCustomView(t, root, "view.yaml", replaceEntry("board.tsx")+"generated: true\n")
	defs, issues := LoadPath(root)
	if len(issues) != 0 || len(defs) != 1 {
		t.Fatalf("load %v", issues)
	}
	if defs[0].Generated {
		t.Fatal("authored files cannot mark themselves generated")
	}
}
