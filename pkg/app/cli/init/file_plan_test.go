package init

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// applyAgentSurfacesForTest plans and applies only the agent surfaces, with no
// UI, and returns the paths it created, updated, or removed.
func applyAgentSurfacesForTest(root string, harnesses AgentHarnesses, section string, templates []string, opts AgentSurfaceOptions) ([]string, error) {
	record, _ := loadGeneratedFiles(root)
	plan := &filePlan{}
	if err := planAgentSurfaces(root, harnesses, section, templates, opts, record, plan); err != nil {
		return nil, err
	}
	report, err := applyFilePlan(root, plan, record, nil)
	return report.changedPaths(), err
}

func syncForTest(t *testing.T, root string, templates []string, ui ownershipUI) syncReport {
	t.Helper()
	report, err := syncGeneratedFiles(root, generatedInputs{
		harnesses: AgentHarnesses{HasAgents: true, HasAgentSkills: true, HasClaude: true},
		section:   "test guidance",
		templates: templates,
	}, ui)
	require.NoError(t, err)
	return report
}

// skillTemplateMatchesOnDisk reports whether every file of the named skill on
// disk equals what init renders.
func skillTemplateMatchesOnDisk(skillRoot string, templates []skillTemplate, name string) (bool, error) {
	rendered, err := renderSkillTemplates(templates)
	if err != nil {
		return false, err
	}
	for _, skill := range rendered {
		if skill.name != name {
			continue
		}
		for _, file := range skill.files {
			got, err := os.ReadFile(filepath.Join(skillRoot, name, filepath.FromSlash(file.path)))
			if err != nil {
				if os.IsNotExist(err) {
					return false, nil
				}
				return false, err
			}
			if !bytes.Equal(got, file.content) {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

// failUI fails the test if init asks anything.
type failUI struct{ t *testing.T }

func (u failUI) decideUnknown(changes []*change) string {
	u.t.Fatalf("unexpected unknown-files question: %v", uniqueGroupPaths(changes))
	return ""
}

func (u failUI) decideEdited(c *change, _ int) bool {
	u.t.Fatalf("unexpected edited-file question: %s", c.key)
	return false
}

// scriptedUI answers every question the same way and records what was asked.
type scriptedUI struct {
	unknown string
	take    bool
	asked   []string
	mirrors []int
}

func (u *scriptedUI) decideUnknown(changes []*change) string {
	u.asked = append(u.asked, "unknown:"+strings.Join(uniqueGroupPaths(changes), ","))
	return u.unknown
}

func (u *scriptedUI) decideEdited(c *change, mirrors int) bool {
	u.asked = append(u.asked, "edited:"+c.key)
	u.mirrors = append(u.mirrors, mirrors)
	return u.take
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func readTestFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(data)
}

// setWritten pretends Rhizome last wrote the given content at key.
func setWritten(t *testing.T, root, key, content string) {
	t.Helper()
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	record.Written[key] = contentFingerprint([]byte(content))
	require.NoError(t, record.save(root))
}

const sessionsRef = ".agents/skills/rhizome/references/sessions.md"
const routerSkill = ".agents/skills/agentic-engineering/SKILL.md"

func TestSyncCreatesEverythingWithoutAsking(t *testing.T) {
	root := t.TempDir()
	report := syncForTest(t, root, []string{templateCore, templateAgenticEngineering, templateActionItems}, failUI{t})

	require.Contains(t, report.Created, "AGENTS.md")
	require.Contains(t, report.Created, sessionsRef)
	require.Contains(t, report.Created, ".claude/skills/rhizome/SKILL.md")
	require.Contains(t, report.Created, "docs/engineering/testing-policy.md")
	require.Contains(t, readTestFile(t, root, "AGENTS.md"), "test guidance")
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	require.NotEmpty(t, record.Written["AGENTS.md#rhizome"])
	require.NotEmpty(t, record.Written["docs/engineering/testing-policy.md"], "starter docs are recorded at creation")
	require.NoFileExists(t, filepath.Join(root, ".agents", "skills", "rhizome", ".rhizome-managed"))
	require.NoFileExists(t, filepath.Join(root, ".agents", "skills", "rhizome", ".rhizome-managed-files"))

	second := syncForTest(t, root, []string{templateCore, templateAgenticEngineering, templateActionItems}, failUI{t})
	require.Empty(t, second.changedPaths(), "a second run changes nothing")
}

func TestSyncUpdatesUneditedOlderFilesWithoutAsking(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, failUI{t})
	want := readTestFile(t, root, sessionsRef)
	writeTestFile(t, root, sessionsRef, "older Rhizome version\n")
	setWritten(t, root, sessionsRef, "older Rhizome version\n")

	report := syncForTest(t, root, nil, failUI{t})

	require.Equal(t, want, readTestFile(t, root, sessionsRef))
	require.Contains(t, report.Updated, sessionsRef)
}

func TestSyncAsksOnceAboutAnEditedSkillAcrossMirrorsAndRemembersKeep(t *testing.T) {
	root := t.TempDir()
	templates := []string{templateCore, templateAgenticEngineering}
	syncForTest(t, root, templates, failUI{t})
	for _, rel := range []string{routerSkill, ".claude/skills/agentic-engineering/SKILL.md"} {
		writeTestFile(t, root, rel, readTestFile(t, root, rel)+"\nTeam note.\n")
		setWritten(t, root, rel, "an older Rhizome version\n")
	}

	ui := &scriptedUI{take: false}
	report := syncForTest(t, root, templates, ui)

	require.Equal(t, []string{"edited:" + routerSkill}, ui.asked)
	require.Equal(t, []int{1}, ui.mirrors)
	require.Contains(t, readTestFile(t, root, routerSkill), "Team note.")
	require.Empty(t, report.Kept, "a person decided, so nothing is reported as pending")

	syncForTest(t, root, templates, failUI{t})

	// A newer version than the declined one asks again.
	record, _ := loadGeneratedFiles(root)
	record.Declined[routerSkill] = "0000000000000000"
	record.Declined[".claude/skills/agentic-engineering/SKILL.md"] = "0000000000000000"
	require.NoError(t, record.save(root))
	again := &scriptedUI{take: true}
	syncForTest(t, root, templates, again)
	require.Len(t, again.asked, 1)
	require.NotContains(t, readTestFile(t, root, routerSkill), "Team note.")
}

func TestSyncWithoutUIKeepsEditedFilesAndListsThem(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, nil)
	writeTestFile(t, root, sessionsRef, "my edit\n")
	setWritten(t, root, sessionsRef, "an older Rhizome version\n")

	report := syncForTest(t, root, nil, nil)

	require.Equal(t, "my edit\n", readTestFile(t, root, sessionsRef))
	require.Equal(t, []string{sessionsRef}, report.Kept)
	record, _ := loadGeneratedFiles(root)
	require.Empty(t, record.Declined, "a batch run does not decide on anyone's behalf")
}

func TestSyncLeavesLocalEditsAloneWhenRhizomeHasNoNewerVersion(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, failUI{t})
	writeTestFile(t, root, sessionsRef, "my edit\n")

	report := syncForTest(t, root, nil, failUI{t})

	require.Equal(t, "my edit\n", readTestFile(t, root, sessionsRef))
	require.Empty(t, report.Kept)
	require.Empty(t, report.changedPaths())
}

func TestSyncGroupsUnrecordedDifferingFilesIntoOneQuestion(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, failUI{t})
	simulateUpgradeFromMarkers(t, root)
	writeTestFile(t, root, sessionsRef, "unknown history\n")
	writeTestFile(t, root, ".agents/skills/rhizome/SKILL.md", "unknown history\n")

	ui := &scriptedUI{unknown: "keep"}
	syncForTest(t, root, nil, ui)

	require.Len(t, ui.asked, 1)
	require.True(t, strings.HasPrefix(ui.asked[0], "unknown:"))
	require.Equal(t, "unknown history\n", readTestFile(t, root, sessionsRef))
	record, _ := loadGeneratedFiles(root)
	require.NotEmpty(t, record.Declined[sessionsRef])
	require.NotEmpty(t, record.Written[".agents/skills/rhizome/references/code-mode.md"], "matching files are recorded silently")

	update := &scriptedUI{unknown: "update"}
	simulateUpgradeFromMarkers(t, root)
	syncForTest(t, root, nil, update)
	require.NotEqual(t, "unknown history\n", readTestFile(t, root, sessionsRef))
}

func TestSyncUpdatesUnrecordedManagedBlocksWithoutAsking(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "AGENTS.md", "# Team rules\n\n"+buildManagedRhizomeBlock("older guidance")+"\n")

	syncForTest(t, root, nil, failUI{t})

	agents := readTestFile(t, root, "AGENTS.md")
	require.Contains(t, agents, "# Team rules")
	require.Contains(t, agents, "test guidance")
	require.NotContains(t, agents, "older guidance")
}

func TestSyncFoldsLegacySkillMarkersIntoTheRecord(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, failUI{t})
	simulateUpgradeFromMarkers(t, root)
	skillDir := filepath.Join(root, ".agents", "skills", "rhizome")
	older := []byte("older shipped reference\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(sessionsRef)), older, 0o644))
	sum := sha256.Sum256(older)
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, ".rhizome-managed"), []byte("managed-by: rzm init\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, ".rhizome-managed-files"), []byte("references/sessions.md "+hex.EncodeToString(sum[:])+"\n"), 0o644))
	writeTestFile(t, root, ".rhizome/template-rejections.yml", "rejections: {}\n")

	report := syncForTest(t, root, nil, failUI{t})

	require.Contains(t, report.Updated, sessionsRef, "the legacy manifest proves the reference was unedited")
	require.NoFileExists(t, filepath.Join(skillDir, ".rhizome-managed"))
	require.NoFileExists(t, filepath.Join(skillDir, ".rhizome-managed-files"))
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "template-rejections.yml"))
}

func TestSyncRemovesDroppedSkillFilesOnlyWhenUnedited(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, failUI{t})
	dropped := ".agents/skills/rhizome/references/dropped.md"
	edited := ".agents/skills/rhizome/references/edited.md"
	writeTestFile(t, root, dropped, "shipped once\n")
	setWritten(t, root, dropped, "shipped once\n")
	writeTestFile(t, root, edited, "my notes\n")
	setWritten(t, root, edited, "shipped once\n")

	report := syncForTest(t, root, nil, nil)

	require.NoFileExists(t, filepath.Join(root, filepath.FromSlash(dropped)))
	require.Contains(t, report.Removed, dropped)
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(edited)))
	require.Contains(t, report.Kept, edited)
}

func TestSyncRemovesAWorkflowThatIsTurnedOff(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, []string{templateCore, templateAgenticEngineering}, failUI{t})
	require.DirExists(t, filepath.Join(root, ".agents", "skills", "foundation-review"))
	// A file someone added to a Rhizome skill folder stays with its folder.
	writeTestFile(t, root, ".claude/skills/ingest-transcript/NOTES.md", "mine\n")

	syncForTest(t, root, nil, failUI{t})

	require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "foundation-review"))
	require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "agentic-engineering"))
	require.NoFileExists(t, filepath.Join(root, ".claude", "skills", "ingest-transcript", "SKILL.md"))
	require.FileExists(t, filepath.Join(root, ".claude", "skills", "ingest-transcript", "NOTES.md"))
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "views", "efforts.yaml"), "saved views of a removed workflow go")
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "query-recipes", "spec-driven.yaml"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"), "the schema stays for existing notes")
	require.FileExists(t, filepath.Join(root, "docs", "engineering", "testing-policy.md"), "team-owned docs stay")
	require.NotContains(t, readTestFile(t, root, "AGENTS.md"), managedTemplateBlockStartPrefix+templateAgenticEngineering)
}

func TestSyncRefusesToOverwriteAUserRhizomeSkill(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".agents/skills/rhizome/SKILL.md", "my own skill\n")

	_, err := syncGeneratedFiles(root, generatedInputs{
		harnesses: AgentHarnesses{HasAgentSkills: true},
		section:   "test guidance",
	}, nil)

	require.ErrorContains(t, err, "Rhizome skill collision")
	require.Equal(t, "my own skill\n", readTestFile(t, root, ".agents/skills/rhizome/SKILL.md"))
}

func TestStarterDocsAreCreatedButNotRefreshedUnlessRequested(t *testing.T) {
	root := t.TempDir()
	templates := []string{templateCore, templateAgenticEngineering}
	syncForTest(t, root, templates, failUI{t})
	doc := "docs/engineering/testing-policy.md"
	shipped := readTestFile(t, root, doc)
	writeTestFile(t, root, doc, "older shipped policy\n")
	setWritten(t, root, doc, "older shipped policy\n")

	syncForTest(t, root, templates, failUI{t})
	require.Equal(t, "older shipped policy\n", readTestFile(t, root, doc), "team-owned docs are create-only")

	_, err := syncGeneratedFiles(root, generatedInputs{
		harnesses:   AgentHarnesses{HasAgents: true, HasAgentSkills: true, HasClaude: true},
		section:     "test guidance",
		templates:   templates,
		refreshDocs: true,
	}, failUI{t})
	require.NoError(t, err)
	require.Equal(t, shipped, readTestFile(t, root, doc), "a requested refresh updates an unedited doc")
}

func TestContentFingerprintIgnoresLineEndings(t *testing.T) {
	require.Equal(t, contentFingerprint([]byte("a\nb\n")), contentFingerprint([]byte("a\r\nb\r\n")))
	require.Equal(t, contentFingerprint([]byte("a\nb")), contentFingerprint([]byte("a\nb\n\n")))
	require.NotEqual(t, contentFingerprint([]byte("a\nb\n")), contentFingerprint([]byte("a\nc\n")))
}

func TestGeneratedFilesRecordIsSortedAndUnreadableRecordsAreEmpty(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "b.md", "b\n")
	writeTestFile(t, root, "a.md", "a\n")
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	record.Written["b.md"] = "2"
	record.Written["a.md"] = "1"
	record.Written["gone.md"] = "3"
	require.NoError(t, record.save(root))

	body := readTestFile(t, root, ".rhizome/generated-files.yml")
	require.Less(t, strings.Index(body, "a.md"), strings.Index(body, "b.md"))
	require.NotContains(t, body, "gone.md", "entries for missing files are dropped")

	writeTestFile(t, root, ".rhizome/generated-files.yml", "<<<<<<< HEAD\nwritten: [\n")
	conflicted, ok := loadGeneratedFiles(root)
	require.False(t, ok)
	require.Empty(t, conflicted.Written)
}

func TestStarterValidationSelector(t *testing.T) {
	require.Equal(t, "all", starterValidationSelector([]string{".rhizome/ontology/core.graphql", ".rhizome/views/a.yaml"}))
	require.Equal(t, "query-recipes", starterValidationSelector([]string{".rhizome/query-recipes/core.yaml", "AGENTS.md"}))
	require.Empty(t, starterValidationSelector([]string{"AGENTS.md"}))
}

func TestSyncCleansUpRetiredSkillsWithoutAsking(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".agents/skills/rhizome-onboard/SKILL.md", "retired core skill\n")
	writeTestFile(t, root, ".agents/skills/rhizome-onboard/.rhizome-managed", "managed-by: rzm init\n")
	writeTestFile(t, root, ".claude/skills/rhizome-hub-notes/SKILL.md", "retired before markers existed\n")
	writeTestFile(t, root, ".agents/skills/plan/SKILL.md", "retired starter skill\n")
	writeTestFile(t, root, ".agents/skills/plan/.rhizome-managed", "managed-by: rzm init\n")
	writeTestFile(t, root, ".agents/skills/implement/SKILL.md", "hand written\n")
	writeTestFile(t, root, ".agents/skills/rhizome-personal/SKILL.md", "user owned\n")

	report := syncForTest(t, root, nil, failUI{t})

	require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "rhizome-onboard"))
	require.NoDirExists(t, filepath.Join(root, ".claude", "skills", "rhizome-hub-notes"), "retired rhizome-* names are Rhizome's by name")
	require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "plan"), "a marker proves a generic retired name is Rhizome's")
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "implement", "SKILL.md"), "a generic name without evidence may be the team's own skill")
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "rhizome-personal", "SKILL.md"))
	require.Empty(t, report.Kept)
}
func TestSyncAsksAboutUnknownUpdatesAndRemovalsSeparately(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, failUI{t})
	simulateUpgradeFromMarkers(t, root)
	writeTestFile(t, root, sessionsRef, "unknown history\n")
	// A skill of a starter this repository no longer uses, from before the record.
	writeTestFile(t, root, ".agents/skills/foundation-review/SKILL.md", "inactive starter skill\n")
	writeTestFile(t, root, ".agents/skills/foundation-review/.rhizome-managed", "managed-by: rzm init\n")

	ui := &scriptedUI{unknown: "keep"}
	syncForTest(t, root, nil, ui)

	require.Equal(t, []string{
		"unknown:" + sessionsRef,
		"unknown:.agents/skills/foundation-review/SKILL.md",
	}, ui.asked)
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "foundation-review", "SKILL.md"))

	// A kept removal is remembered.
	syncForTest(t, root, nil, failUI{t})
}
func TestSyncKeepsAnEditedBlockOfAStarterThatWasTurnedOff(t *testing.T) {
	root := t.TempDir()
	templates := []string{templateCore, templateAgenticEngineering, templateActionItems}
	syncForTest(t, root, templates, failUI{t})
	agents := readTestFile(t, root, "AGENTS.md")
	body, ok := managedBlockBody(agents, templateActionItems)
	require.True(t, ok)
	writeTestFile(t, root, "AGENTS.md", strings.Replace(agents, body, body+"\n- Team rule.", 1))

	report := syncForTest(t, root, []string{templateCore, templateAgenticEngineering}, nil)

	require.Contains(t, readTestFile(t, root, "AGENTS.md"), "- Team rule.")
	require.Contains(t, report.Kept, "AGENTS.md#"+templateActionItems)
	claude := readTestFile(t, root, "CLAUDE.md")
	require.NotContains(t, claude, managedTemplateBlockStartPrefix+templateActionItems, "the unedited copy is removed")
}

func TestCleanCloneWithoutRecordAcceptsTheCurrentRhizomeSkill(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, failUI{t})
	require.NoError(t, os.Remove(generatedFilesPath(root)))

	syncForTest(t, root, nil, failUI{t})

	record, _ := loadGeneratedFiles(root)
	require.NotEmpty(t, record.Written[sessionsRef])
}
func simulateUpgradeFromMarkers(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, os.Remove(generatedFilesPath(root)))
	for _, surface := range []string{".agents/skills", ".claude/skills"} {
		writeTestFile(t, root, surface+"/rhizome/.rhizome-managed", "managed-by: rzm init\n")
	}
}

func TestUnreadableRecordDoesNotBlockSetup(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, nil)
	writeTestFile(t, root, ".rhizome/generated-files.yml", "<<<<<<< HEAD\nwritten: [\n")

	report := syncForTest(t, root, nil, nil)

	require.Empty(t, report.Kept, "unchanged files match the current version and are recorded again")
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	require.NotEmpty(t, record.Written[sessionsRef])
}

func TestSyncLeavesAnUnlinkedWorkflowInPlace(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, []string{templateCore, templateAgenticEngineering}, failUI{t})
	writeTestFile(t, root, routerSkill, "Our own version now.\n")

	_, err := syncGeneratedFiles(root, generatedInputs{
		harnesses: AgentHarnesses{HasAgents: true, HasAgentSkills: true, HasClaude: true},
		section:   "test guidance",
		templates: []string{templateCore},
		surfaceOpts: AgentSurfaceOptions{
			PreserveTemplateBlocks: []string{templateAgenticEngineering},
			PreserveSkillTemplates: []string{templateAgenticEngineering},
		},
	}, failUI{t})
	require.NoError(t, err)

	require.Equal(t, "Our own version now.\n", readTestFile(t, root, routerSkill))
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "foundation-review", "SKILL.md"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "views", "efforts.yaml"))
	require.Contains(t, readTestFile(t, root, "AGENTS.md"), managedTemplateBlockStartPrefix+templateAgenticEngineering)
}
