package viewconfig

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// writeExperienceView writes a custom view definition and its entry.
func writeExperienceView(t *testing.T, dir, id, mount, entry, source string) {
	t.Helper()
	writeViewFile(t, filepath.Join(dir, id+".yaml"), "apiVersion: rhizome.view.v1\nid: "+id+"\nname: "+id+"\nsource: {kind: custom, entry: "+entry+"}\nmount: "+mount+"\n")
	writeViewFile(t, filepath.Join(dir, entry), source)
}

// fakeDefaultExport stands in for viewscript.HasDefaultExport, which this
// package cannot import.
func fakeDefaultExport(_ string, source []byte) (bool, error) {
	return strings.Contains(string(source), "export default"), nil
}

func experienceOpts() ValidateOptions {
	return ValidateOptions{
		CheckReferences:   true,
		TypeNames:         map[string]struct{}{"Claim": {}, "Source": {}},
		NodeTypeNames:     map[string]struct{}{"Claim": {}, "Source": {}},
		InterfaceNames:    map[string]struct{}{"Evidence": {}},
		GroupNames:        map[string]struct{}{"Knowledge": {}},
		ScanDefaultExport: fakeDefaultExport,
	}
}

func viewByID(t *testing.T, views []ViewDefinition, id string) ViewDefinition {
	t.Helper()
	for _, view := range views {
		if view.ID == id {
			return view
		}
	}
	t.Fatalf("view %s not found", id)
	return ViewDefinition{}
}

func issuesAt(issues []Issue, code, path, field string) []Issue {
	var out []Issue
	for _, issue := range issues {
		if issue.Code == code && issue.Path == path && issue.Field == field {
			out = append(out, issue)
		}
	}
	return out
}

func TestExperienceFolderHoldsItsViewsAndPanel(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "knowledge")
	writeViewFile(t, filepath.Join(dir, ExperienceFile), "title: Knowledge\ngroup: Knowledge\ntypes: [Evidence]\npanel: knowledge.panel\ntailwind: false\n")
	writeExperienceView(t, dir, "knowledge.home", "{kind: group, group: Knowledge}", "home.tsx", "export default function Home() {}")
	writeExperienceView(t, filepath.Join(dir, "records"), "knowledge.claim", "{kind: node, type: Claim}", "claim.tsx", "export default function Claim() {}")
	writeExperienceView(t, dir, "knowledge.panel", "{kind: panel}", "panel.tsx", "export default function Panel() {}")
	writeExperienceView(t, root, "outside", "{kind: standalone}", "outside.tsx", "export default function Outside() {}")

	views, issues := LoadPath(root)
	require.Empty(t, issues)
	require.Len(t, views, 4, "experience.yml is not a view definition")
	result := Validate(views, experienceOpts())
	require.Empty(t, result.Issues)

	home := viewByID(t, result.Views, "knowledge.home")
	require.NotNil(t, home.Experience)
	require.True(t, home.ExperiencePage)
	require.Equal(t, "Knowledge", home.Experience.Title)
	require.Equal(t, "knowledge", home.Experience.Folder)
	require.False(t, home.Experience.UsesTailwind())
	require.Same(t, home.Experience, viewByID(t, result.Views, "knowledge.claim").Experience, "views below the folder belong to it")
	require.Same(t, home.Experience, viewByID(t, result.Views, "knowledge.panel").Experience)
	require.Nil(t, viewByID(t, result.Views, "outside").Experience)

	// The key names the root and folder, not the contents.
	key := home.Experience.Key()
	writeViewFile(t, filepath.Join(dir, "home.tsx"), "export default function Home() { return 1 }")
	writeViewFile(t, filepath.Join(dir, ExperienceFile), "title: Renamed\npanel: knowledge.panel\n")
	again, _ := LoadPath(root)
	require.Equal(t, key, viewByID(t, again, "knowledge.home").Experience.Key())
}

func TestExperienceKeysAndOrderFollowTheViewsRoot(t *testing.T) {
	repo := t.TempDir()
	body := "title: Same folder\n"
	// Flat, a team module, a bundled starter's module, and an ejected copy:
	// each is a views folder like any other.
	for _, dir := range []string{".rhizome/views/site", ".rhizome/modules/team/views/site", ".rhizome/modules/ao-kb/views/site", ".rhizome/modules/ejected/views/site"} {
		writeViewFile(t, filepath.Join(repo, dir, ExperienceFile), body)
		writeExperienceView(t, filepath.Join(repo, dir), strings.ReplaceAll(strings.TrimPrefix(dir, ".rhizome/"), "/", "-"), "{kind: standalone}", "page.tsx", "export default 1")
	}
	views, issues := LoadDefaultSource(repo)
	require.Empty(t, issues)
	result := Validate(views, ValidateOptions{ScanDefaultExport: fakeDefaultExport})
	require.Empty(t, result.Issues)
	keys := map[string]bool{}
	var experiences []*Experience
	for _, view := range result.Views {
		require.NotNil(t, view.Experience, view.ID)
		require.True(t, view.ExperiencePage, view.ID)
		keys[view.Experience.Key()] = true
		experiences = append(experiences, view.Experience)
	}
	require.Len(t, keys, 4, "the same folder in different roots is a different experience")
	modules := []string{}
	for _, exp := range experiences {
		modules = append(modules, exp.Module)
	}
	require.ElementsMatch(t, []string{"", "team", "ao-kb", "ejected"}, modules)
	// Views-root order: the flat folder, then modules by name, then bundled.
	flat, aokb, team := &Experience{Folder: "z"}, &Experience{Module: "ao-kb", Folder: "a"}, &Experience{Module: "team", Folder: "a"}
	bundled := &Experience{Bundled: true, Folder: "a"}
	require.True(t, experienceLess(flat, aokb))
	require.True(t, experienceLess(aokb, team))
	require.True(t, experienceLess(team, bundled))
	require.True(t, experienceLess(&Experience{Folder: "a"}, flat), "then folder path")
}

func TestExperiencesLoadFromAFilesystem(t *testing.T) {
	fsys := fstest.MapFS{
		"site/experience.yml": {Data: []byte("title: Site\n")},
		"site/page.yaml":      {Data: []byte("apiVersion: rhizome.view.v1\nid: site.page\nname: Page\nsource: {kind: custom, entry: page.tsx}\nmount: {kind: standalone}\n")},
		"site/page.tsx":       {Data: []byte("export default 1")},
	}
	views, issues := LoadFS(fsys)
	require.Empty(t, issues)
	require.Len(t, views, 1)
	result := Validate(views, ValidateOptions{EntryFS: fsys, ScanDefaultExport: fakeDefaultExport})
	require.Empty(t, result.Issues)
	require.Equal(t, "site/experience.yml", result.Views[0].Experience.Path)
	require.True(t, result.Views[0].ExperiencePage)
}

func TestNestedExperienceIsAnErrorAndItsViewsStayOutside(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "site")
	inner := filepath.Join(outer, "inner")
	writeViewFile(t, filepath.Join(outer, ExperienceFile), "title: Outer\n")
	writeViewFile(t, filepath.Join(inner, ExperienceFile), "title: Inner\n")
	writeExperienceView(t, inner, "inner.page", "{kind: standalone}", "page.tsx", "export default 1")
	views, _ := LoadPath(root)
	result := Validate(views, experienceOpts())
	require.Len(t, issuesAt(result.Issues, "nested_experience", filepath.Join(inner, ExperienceFile), ""), 1)
	page := viewByID(t, result.Views, "inner.page")
	require.NotNil(t, page.Experience)
	require.Equal(t, "Outer", page.Experience.Title, "the inner folder's views stay in the outer experience")
}

// Only a missing title or a decode error invalidates an experience; a bad
// reference drops that name and a panel problem drops the panel, so a starter
// that lists a type this vault lacks keeps its frame (SPEC-0136 R1).
func TestExperienceValidationReportsFileAndField(t *testing.T) {
	noPanel := func(t *testing.T, exp *Experience) { require.Empty(t, exp.Panel) }
	cases := []struct {
		name, yml, code, field string
		fatal                  bool
		extra                  func(t *testing.T, dir string)
		kept                   func(t *testing.T, exp *Experience)
	}{
		{name: "unknown field", yml: "title: T\ncolour: teal\n", code: "unknown_experience_field", field: "colour"},
		{name: "missing title", yml: "group: Knowledge\n", code: "missing_required_field", field: "title", fatal: true},
		{name: "decode error", yml: "title: [unclosed\n", code: "experience_yaml_decode_error", fatal: true},
		{name: "unknown group", yml: "title: T\ngroup: Missing\ntypes: [Claim]\n", code: "invalid_experience_reference", field: "group", kept: func(t *testing.T, exp *Experience) {
			require.Empty(t, exp.Group)
			require.Equal(t, []string{"Claim"}, exp.Types)
		}},
		{name: "unknown type", yml: "title: T\ngroup: Knowledge\ntypes: [Claim, Missing, Evidence]\n", code: "invalid_experience_reference", field: "types", kept: func(t *testing.T, exp *Experience) {
			require.Equal(t, []string{"Claim", "Evidence"}, exp.Types)
			require.Equal(t, "Knowledge", exp.Group)
		}},
		{name: "panel names nothing", yml: "title: T\npanel: nope\n", code: "invalid_experience_panel", field: "panel", kept: noPanel},
		{name: "panel is not a panel mount", yml: "title: T\npanel: site.page\n", code: "invalid_experience_panel", field: "panel", kept: noPanel},
		{name: "panel is HTML", yml: "title: T\npanel: site.panel\n", code: "invalid_experience_panel", field: "panel", kept: noPanel, extra: func(t *testing.T, dir string) {
			writeExperienceView(t, dir, "site.panel", "{kind: panel}", "panel.html", "<html></html>")
		}},
		{name: "panel without default export", yml: "title: T\npanel: site.panel\n", code: "invalid_experience_panel", field: "panel", kept: noPanel, extra: func(t *testing.T, dir string) {
			writeExperienceView(t, dir, "site.panel", "{kind: panel}", "panel.tsx", "export function Panel() {}")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "site")
			file := filepath.Join(dir, ExperienceFile)
			writeViewFile(t, file, tc.yml)
			writeExperienceView(t, dir, "site.page", "{kind: standalone}", "page.tsx", "export default 1")
			if tc.extra != nil {
				tc.extra(t, dir)
			}
			views, loadIssues := LoadPath(root)
			require.Empty(t, loadIssues)
			result := Validate(views, experienceOpts())
			found := issuesAt(result.Issues, tc.code, file, tc.field)
			require.NotEmpty(t, found, "%#v", result.Issues)
			require.Equal(t, tc.fatal, found[0].Severity == IssueFatal)
			page := viewByID(t, result.Views, "site.page")
			if tc.fatal {
				require.Nil(t, page.Experience, "an invalid experience leaves its views as ordinary views")
				require.False(t, page.ExperiencePage)
			} else {
				require.NotNil(t, page.Experience)
				require.True(t, page.ExperiencePage, "the page keeps its experience frame")
			}
			if tc.kept != nil {
				tc.kept(t, page.Experience)
				original := viewByID(t, views, "site.page").Experience
				require.NotSame(t, original, page.Experience, "the loaded experience is left unchanged")
			}
			for _, issue := range result.Issues {
				if issue.View == "site.page" || (issue.View == "site.panel" && issue.Severity != IssueWarning) {
					t.Fatalf("the views themselves stay valid: %#v", issue)
				}
			}
		})
	}
}

func TestExperienceViewsThatKeepTheirOwnFrameAreReported(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "site")
	writeViewFile(t, filepath.Join(dir, ExperienceFile), "title: Site\n")
	writeExperienceView(t, dir, "site.named", "{kind: standalone}", "named.tsx", "export function Named() {}")
	writeExperienceView(t, dir, "site.html", "{kind: standalone}", "page.html", "<html></html>")
	writeExperienceView(t, dir, "site.page", "{kind: standalone}", "page.tsx", "export default 1")
	views, _ := LoadPath(root)
	result := Validate(views, experienceOpts())
	for _, id := range []string{"site.named", "site.html"} {
		view := viewByID(t, result.Views, id)
		require.NotNil(t, view.Experience, "%s still belongs to its experience", id)
		require.False(t, view.ExperiencePage, "%s keeps its own frame", id)
		found := issuesAt(result.Issues, "experience_view_own_frame", view.Source.Path, "source.entry")
		require.Len(t, found, 1, id)
		require.Equal(t, IssueWarning, found[0].Severity)
	}
	require.True(t, viewByID(t, result.Views, "site.page").ExperiencePage)
}

func TestPanelMountsAreValidOnlyWhereAnExperienceNamesThem(t *testing.T) {
	root := t.TempDir()
	writeExperienceView(t, root, "loose.panel", "{kind: panel}", "loose.tsx", "export default 1")
	dir := filepath.Join(root, "site")
	writeViewFile(t, filepath.Join(dir, ExperienceFile), "title: Site\npanel: site.panel\n")
	writeExperienceView(t, dir, "site.panel", "{kind: panel}", "panel.tsx", "export default 1")
	writeExperienceView(t, dir, "site.extra", "{kind: panel}", "extra.tsx", "export default 1")
	writeExperienceView(t, dir, "site.typed", "{kind: panel, type: Claim}", "typed.tsx", "export default 1")
	views, _ := LoadPath(root)
	result := Validate(views, experienceOpts())
	for _, id := range []string{"loose.panel", "site.extra"} {
		view := viewByID(t, result.Views, id)
		require.Len(t, issuesAt(result.Issues, "invalid_panel_mount", view.Source.Path, "mount.kind"), 1, id)
	}
	typed := viewByID(t, result.Views, "site.typed")
	require.Len(t, issuesAt(result.Issues, "unexpected_field", typed.Source.Path, "mount.type"), 1)
	panel := viewByID(t, result.Views, "site.panel")
	for _, issue := range result.Issues {
		require.NotEqual(t, "site.panel", issue.View, "%#v", issue)
	}
	require.NotNil(t, panel.Experience)
	require.False(t, MatchesMount(panel.Mount, MountKindPanel, ""), "a panel is never a view choice")
}

func TestExperienceFileIsNeverServed(t *testing.T) {
	require.False(t, ServablePath("site/"+ExperienceFile))
}
