package init

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// rerunRepo returns a repository set up by a first run without a terminal.
func rerunRepo(t *testing.T) string {
	t.Helper()
	root := firstRunRepo(t)
	_, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	return root
}

func runInit(t *testing.T, opts RunOptions) (string, error) {
	t.Helper()
	var out bytes.Buffer
	opts.Stdout, opts.Stderr = &out, &out
	if opts.Stdin == nil {
		opts.Stdin = bytes.NewBuffer(nil)
	}
	err := Run(opts)
	return out.String(), err
}

func loadConfigForTest(t *testing.T, root string) *obsidian.LocalConfig {
	t.Helper()
	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	return cfg
}

func TestRerunWithNothingPendingIsUpToDate(t *testing.T) {
	root := rerunRepo(t)

	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Rhizome is set up in "+filepath.Base(root))
	require.Contains(t, out, "Workflow  Agentic Engineering")
	require.Contains(t, out, "Everything is up to date.")
	require.NotContains(t, out, "Changes")

	_, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)

	out, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n")})
	require.NoError(t, err)
	require.Contains(t, out, "Everything is up to date. Press s for settings or Enter to exit")
}

func TestFirstRunCheckWritesNothing(t *testing.T) {
	root := firstRunRepo(t)

	out, err := runInit(t, RunOptions{Dir: root, Check: true})

	require.ErrorIs(t, err, ErrChangesPending)
	require.Contains(t, out, "Set up Rhizome in")
	require.Contains(t, out, "+ Create .rhizome/config.yml")
	require.Contains(t, out, ".agents/skills/rhizome/SKILL.md")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
	require.NoFileExists(t, filepath.Join(root, "AGENTS.md"))
}

func TestRerunCheckListsMissingFilesAndWritesNothing(t *testing.T) {
	root := rerunRepo(t)
	skill := filepath.Join(root, ".agents", "skills", "rhizome", "SKILL.md")
	require.NoError(t, os.Remove(skill))

	out, err := runInit(t, RunOptions{Dir: root, Check: true})

	require.ErrorIs(t, err, ErrChangesPending)
	require.Contains(t, out, "+ 1 new Rhizome file")
	require.Contains(t, out, ".agents/skills/rhizome/SKILL.md")
	require.NoFileExists(t, skill)
}

func TestRerunWithoutATerminalKeepsAnEditedSkillAndCheckNamesIt(t *testing.T) {
	root := rerunRepo(t)
	const rel = ".agents/skills/agentic-engineering/SKILL.md"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("my edit\n"), 0o644))
	setWritten(t, root, rel, "an older Rhizome version\n")

	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Needs a decision")
	require.Contains(t, out, "! 1 file you edited has a newer version\n      "+rel)
	body, err := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, err)
	require.Equal(t, "my edit\n", string(body))

	// A run without a terminal would not change the file, so --check passes
	// while still naming it.
	out, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "! 1 file you edited has a newer version\n      "+rel)

	// In a terminal the edited file is part of the change list and is asked about.
	out, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\nn\n")})
	require.NoError(t, err)
	require.Contains(t, out, "Apply? [Y/n/s for settings]")
	require.Contains(t, out, "has local edits and a newer Rhizome version")
}

func TestRerunNeedsNothingForCodeAddedAfterSetup(t *testing.T) {
	root := firstRunRepo(t)
	require.NoError(t, os.Remove(filepath.Join(root, "go.mod")))
	require.NoError(t, os.RemoveAll(filepath.Join(root, "cmd")))
	_, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Equal(t, obsidian.LocalCodeConfig{Enabled: true}, loadConfigForTest(t, root).Code, "a docs-only first run still turns code on")
	writeFixtureFile(t, root, "go.mod", "module demo\n")
	writeFixtureFile(t, root, "cmd/app/main.go", "package main\nfunc main() {}\n")

	out, err := runInit(t, RunOptions{Dir: root})

	require.NoError(t, err)
	require.Contains(t, out, "Code      Go")
	require.Contains(t, out, "Everything is up to date.")
}

func TestRerunSuggestsTurningOnCodeForAnOlderDocsOnlySetup(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "docs/a.md", "# A\n")
	writeProjectConfig(t, root, "notes:\n  includes: [\"docs/**/*.md\"]\n", obsidian.LocalWorkflowConfig{})

	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Suggestions")
	require.Contains(t, out, "+ Turn on code indexing, so code added later is indexed")
	require.False(t, loadConfigForTest(t, root).Code.Enabled, "a run without a terminal leaves the choice to a person")

	_, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n")})
	require.NoError(t, err)
	require.True(t, loadConfigForTest(t, root).Code.Enabled)
}

func TestRerunSuggestsRemovingCodeFolderLimitsWhenCodeIsOutsideThem(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "cmd/app/main.go", "package main\n")
	writeFixtureFile(t, root, "tools/gen/main.go", "package main\n")
	writeProjectConfig(t, root, "code:\n  enabled: true\n  go:\n    roots: [cmd]\n", obsidian.LocalWorkflowConfig{})

	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Code      Go (limited to cmd/)")
	require.Contains(t, out, "Suggestions")
	require.Contains(t, out, "+ Index code in tools/ (remove the code folder limits)")
	require.Equal(t, []string{"cmd"}, loadConfigForTest(t, root).Code.Go.Roots, "a deliberate limit stays until a person agrees")

	_, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err, "suggestions do not fail --check")

	_, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n")})
	require.NoError(t, err)
	require.Equal(t, obsidian.LocalCodeConfig{Enabled: true}, loadConfigForTest(t, root).Code)
}

func TestFirstRunIndexesAllMarkdown(t *testing.T) {
	root := firstRunRepo(t)
	writeFixtureFile(t, root, "README.md", "# Demo\n")

	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.False(t, notesLimited(loadConfigForTest(t, root).Notes), "first runs write no notes folder limits")

	writeFixtureFile(t, root, "guides/new.md", "# New\n")
	out, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Everything is up to date.", "Markdown added later needs no change")
	require.Contains(t, out, "Docs      all Markdown (")
}

func TestRerunSuggestsRemovingNotesFolderLimits(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "docs/a.md", "# A\n")
	writeFixtureFile(t, root, "guides/b.md", "# B\n")
	writeFixtureFile(t, root, "README.md", "# Demo\n")
	writeFixtureFile(t, root, "third_party/lib/README.md", "# Lib\n")
	writeProjectConfig(t, root, "notes:\n  includes: [\"docs/**/*.md\", \"docs/efforts/**/*.html\"]\n", obsidian.LocalWorkflowConfig{})

	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Docs      docs/ only (1 note)")
	require.Contains(t, out, "+ Index Markdown in guides/ and README.md (remove the notes folder limits)", "content init proposes to skip does not count")
	require.True(t, notesLimited(loadConfigForTest(t, root).Notes))

	_, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n")})
	require.NoError(t, err)
	cfg := loadConfigForTest(t, root)
	require.False(t, notesLimited(cfg.Notes))
	require.Contains(t, cfg.Notes.Includes, "docs/efforts/**/*.html", "includes for other formats stay")
}

func TestCodeDriftTurnsCodeOnAndRemovesLimitsOnlyForCodeOutsideThem(t *testing.T) {
	layout := DetectedLayout{Code: CodeSuggestion{
		Languages: []string{"go", "python"},
		Files:     []string{"cmd/app/main.go", "scripts/run.py", "third_party/lib/x.go", "docs/a.md", "setup.py"},
	}}

	off := obsidian.LocalConfig{}
	require.Equal(t, []string{"Turn on code indexing (Go and Python found)"}, codeDrift(&off, layout))
	require.True(t, off.Code.Enabled)
	require.Empty(t, codeDrift(&off, layout), "code on without folder limits covers everything")

	limited := obsidian.LocalConfig{Code: obsidian.LocalCodeConfig{
		Enabled: true,
		Go:      &obsidian.LocalCodeLangConfig{Roots: []string{"cmd"}},
		Python:  &obsidian.LocalCodeLangConfig{Roots: []string{"scripts"}, Ignore: []string{"**/gen/**"}},
	}}
	require.Equal(t, []string{"Index code in top-level files (remove the code folder limits)"}, codeDrift(&limited, layout), "third_party/ is a proposed skip, not missing code")
	require.Nil(t, limited.Code.Go)
	require.Equal(t, &obsidian.LocalCodeLangConfig{Ignore: []string{"**/gen/**"}}, limited.Code.Python, "other settings in a language block stay")

	covered := obsidian.LocalConfig{Code: obsidian.LocalCodeConfig{Enabled: true, Go: &obsidian.LocalCodeLangConfig{Roots: []string{"cmd", "scripts", "."}}}}
	require.Empty(t, codeDrift(&covered, layout), "a . folder is no limit")

	dotted := obsidian.LocalConfig{Code: obsidian.LocalCodeConfig{Enabled: true, Go: &obsidian.LocalCodeLangConfig{Roots: []string{"./cmd/", "./scripts"}}, DisabledLanguages: []string{"python"}}}
	require.Empty(t, codeDrift(&dotted, layout), "folders written as ./cmd/ cover cmd, and disabled languages are not missing code")
}

func TestRerunSuggestsNewDefaultAddonsAndAppliesThemOnlyWhenConfirmed(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{Templates: []string{templateAgenticEngineering}})
	actionItems := filepath.Join(root, ".rhizome", "ontology", "action-items.graphql")

	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Suggestions")
	require.Contains(t, out, "+ Add Action items (now part of Agentic Engineering)")
	require.NoFileExists(t, actionItems)

	out, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("n\n")})
	require.NoError(t, err)
	require.Contains(t, out, "Nothing changed.")
	require.NoFileExists(t, actionItems)

	_, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n")})
	require.NoError(t, err)
	require.FileExists(t, actionItems)
	require.Equal(t, []string{templateActionItems}, loadConfigForTest(t, root).WorkflowTemplateAddons.Enabled)
}

func TestSettingsMovingAwayFromAWorkflowRemovesOrEjectsIt(t *testing.T) {
	for _, tc := range []struct {
		name, choice string
		eject        bool
	}{{"remove", "2", false}, {"eject", "1", true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := rerunRepo(t)
			skill := filepath.Join(root, ".agents", "skills", "agentic-engineering", "SKILL.md")
			require.FileExists(t, skill)

			// s opens settings, 4 the workflow section, 3 picks guidance only,
			// then remove or eject; Enter leaves settings and Enter applies.
			out, err := runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("s\n4\n3\n" + tc.choice + "\n\n\n")})

			require.NoError(t, err)
			require.Contains(t, out, "What should happen to the files Agentic Engineering installed?")
			require.Contains(t, out, "Workflow: Agentic Engineering → Search and agent guidance only")
			cfg := loadConfigForTest(t, root)
			require.Empty(t, cfg.WorkflowTemplates)
			require.FileExists(t, filepath.Join(root, "docs", "engineering", "testing-policy.md"), "docs stay either way")
			if tc.eject {
				require.FileExists(t, skill)
				require.Contains(t, cfg.WorkflowTemplateManagement.Ejected, templateAgenticEngineering)
			} else {
				require.NoFileExists(t, skill)
				require.Empty(t, cfg.WorkflowTemplateManagement.Ejected)
			}

			// The choice sticks: docs left on disk do not bring the workflow back.
			out, err = runInit(t, RunOptions{Dir: root})
			require.NoError(t, err)
			require.Contains(t, out, "Everything is up to date.")
			if tc.eject {
				// Restoring the kept workflow selects it again instead of
				// removing the files it kept.
				_, err = runInit(t, RunOptions{Dir: root, Restore: templateAgenticEngineering})
				require.NoError(t, err)
				require.FileExists(t, skill)
				cfg = loadConfigForTest(t, root)
				require.Contains(t, cfg.WorkflowTemplates, templateAgenticEngineering)
				require.NotContains(t, cfg.WorkflowTemplateManagement.Ejected, templateAgenticEngineering)
			}
		})
	}
}

func TestSettingsAgentsTurnsCursorOn(t *testing.T) {
	root := rerunRepo(t)

	// s opens settings, 3 the agents section, 2 toggles Cursor; Enter leaves
	// agents, Enter leaves settings, Enter applies.
	out, err := runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("s\n3\n2\n\n\n\n")})

	require.NoError(t, err)
	require.Contains(t, out, "[x] Cursor")
	require.Contains(t, out, "→ Cursor")
	require.FileExists(t, filepath.Join(root, ".cursor", "rules", "rhizome.mdc"))
	require.Equal(t, agentModeOn, loadConfigForTest(t, root).Agents.Cursor)
}

func TestSettingsSearchCanTurnSearchOff(t *testing.T) {
	root := rerunRepo(t)

	out, err := runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("s\n2\n4\n\n\n")})

	require.NoError(t, err)
	require.Contains(t, out, "Search: off until a key is available → off")
	cfg := loadConfigForTest(t, root)
	require.NotNil(t, cfg.NoteEmbeddings)
	require.False(t, cfg.NoteEmbeddings.Enabled)
}

func TestSettingsRemovesFolderLimits(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "docs/a.md", "# A\n")
	writeFixtureFile(t, root, "cmd/app/main.go", "package main\n")
	writeProjectConfig(t, root, "notes:\n  includes: [\"docs/**/*.md\"]\ncode:\n  enabled: true\n  go:\n    roots: [cmd]\n", obsidian.LocalWorkflowConfig{})

	// s opens settings, 1 what gets indexed, 4 removes the limits; Enter
	// leaves settings and Enter applies.
	out, err := runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("s\n1\n4\n\n\n")})

	require.NoError(t, err)
	require.Contains(t, out, "4  Index all Markdown and code (remove the folder limits in .rhizome/config.yml)")
	cfg := loadConfigForTest(t, root)
	require.False(t, notesLimited(cfg.Notes))
	require.Equal(t, obsidian.LocalCodeConfig{Enabled: true}, cfg.Code)
}

func TestRerunSkillCollisionWritesNothingUntilAgentFilesAreOff(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module example.com/app\n")
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{})
	userSkill := filepath.Join(root, ".agents", "skills", coreRhizomeSkillName, "SKILL.md")
	writeFixtureFile(t, root, ".agents/skills/"+coreRhizomeSkillName+"/SKILL.md", "# User Rhizome skill\n")

	_, err := runInit(t, RunOptions{Dir: root, IncludeIgnored: []string{"app"}})
	require.ErrorContains(t, err, "Rhizome skill collision")
	require.ErrorContains(t, err, "--agents none")
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "ignore"))

	_, err = runInit(t, RunOptions{Dir: root, IncludeIgnored: []string{"app"}, Agents: "none"})
	require.NoError(t, err)
	require.Contains(t, readIgnoreFile(t, root), "!/app/\n")
	body, err := os.ReadFile(userSkill)
	require.NoError(t, err)
	require.Equal(t, "# User Rhizome skill\n", string(body))
}

func TestAcceptSuggestionsAppliesThemWithoutATerminal(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "cmd/app/main.go", "package main\n")
	writeFixtureFile(t, root, "tools/gen/main.go", "package main\n")
	writeProjectConfig(t, root, "code:\n  enabled: true\n  go:\n    roots: [cmd]\n", obsidian.LocalWorkflowConfig{})
	_, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)

	_, err = runInit(t, RunOptions{Dir: root, Check: true, AcceptSuggestions: true})
	require.ErrorIs(t, err, ErrChangesPending, "accepted suggestions count as changes")

	out, err := runInit(t, RunOptions{Dir: root, AcceptSuggestions: true})
	require.NoError(t, err)
	require.Contains(t, out, "Suggestions (applied: --accept-suggestions)")
	require.Contains(t, out, "✓ Code indexing covers the whole repository")
	require.Equal(t, obsidian.LocalCodeConfig{Enabled: true}, loadConfigForTest(t, root).Code)
}

func TestRerunOffersToUpdateTheIndexOnlyAfterChanges(t *testing.T) {
	root := rerunRepo(t)
	builds := 0
	index := func(string) error { builds++; return nil }

	out, err := runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n"), IndexNow: index})
	require.NoError(t, err)
	require.NotContains(t, out, "search index now", "nothing changed, so nothing to index")

	require.NoError(t, os.Remove(filepath.Join(root, ".agents", "skills", "rhizome", "SKILL.md")))
	out, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n\n"), IndexNow: index})
	require.NoError(t, err)
	require.Contains(t, out, "Update the search index now? [Y/n]")
	require.Equal(t, 1, builds)
	require.NotContains(t, out, "pick up the changes", "the index step is done, so Next does not repeat it")
}

func TestSearchOllamaKeepsARemoteServer(t *testing.T) {
	root := rerunRepo(t)
	cfg := loadConfigForTest(t, root)
	cfg.NoteEmbeddings = &embeddings.Config{Enabled: true, Provider: searchOllama, Endpoint: "http://gpu.example.test:11434"}
	require.NoError(t, obsidian.SaveLocalConfig(root, *cfg))

	_, err := runInit(t, RunOptions{Dir: root, Search: searchOllama})

	require.NoError(t, err)
	got := loadConfigForTest(t, root).NoteEmbeddings
	require.NotNil(t, got)
	require.True(t, got.Enabled)
	require.Equal(t, "http://gpu.example.test:11434", got.Endpoint)
}

func TestRerunRebuildsAMissingOwnershipRecordAndRemovesOldMarkers(t *testing.T) {
	root := rerunRepo(t)
	record := filepath.Join(root, ".rhizome", generatedFilesName)
	require.NoError(t, os.Remove(record))
	marker := filepath.Join(root, ".agents", "skills", "rhizome", ".rhizome-managed")
	require.NoError(t, os.WriteFile(marker, nil, 0o644))

	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.ErrorIs(t, err, ErrChangesPending)
	require.Contains(t, out, ".rhizome/generated-files.yml")
	require.Contains(t, out, ".agents/skills/rhizome/.rhizome-managed")

	out, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.NotContains(t, out, "Nothing changed.")
	require.FileExists(t, record)
	require.NoFileExists(t, marker)
	_, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
}

func TestCheckListsARemovalThatWaitsOnAnEditedSkillAsADecision(t *testing.T) {
	root := rerunRepo(t)
	var refs []string
	for _, dir := range []string{".agents", ".claude"} {
		if _, err := os.Stat(filepath.Join(root, dir, "skills", "rhizome")); err != nil {
			continue
		}
		ref := dir + "/skills/rhizome/references/retired.md"
		writeTestFile(t, root, ref, "# retired\n")
		setWritten(t, root, ref, "# retired\n")
		router := dir + "/skills/rhizome/SKILL.md"
		writeTestFile(t, root, router, "edited router that still links references/retired.md\n")
		setWritten(t, root, router, "an older Rhizome router\n")
		refs = append(refs, ref)
	}
	require.NotEmpty(t, refs)

	out, err := runInit(t, RunOptions{Dir: root, Check: true})

	require.NoError(t, err, "a removal only a person can unblock does not keep --check failing:\n%s", out)
	require.Contains(t, out, "Needs a decision")
	require.Contains(t, out, "Rhizome no longer ships, if you take the update to the skill that links")
	require.Contains(t, out, refs[0])

	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	for _, ref := range refs {
		require.FileExists(t, filepath.Join(root, filepath.FromSlash(ref)), "the kept router still links it")
	}
}
