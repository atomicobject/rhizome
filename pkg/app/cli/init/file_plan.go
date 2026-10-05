// One ownership rule for every file init generates: create missing files,
// update files that still match what Rhizome last wrote, and ask only about
// files someone edited.
//
// Docs:
// - [[init-starter-workflow#^SPEC-0038-US3-AC4]]
// - [[init-starter-workflow#^SPEC-0038-US3-AC5]]
// - [[init-template-architecture]]
package init

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// generatedFile is one whole file init wants on disk.
type generatedFile struct {
	rel     string // project-relative slash path; also the record key
	content []byte
	mode    os.FileMode
	// group joins copies of one file in several agent folders so they share
	// one decision. Empty means the file decides alone.
	group string
	// teamOwned files (starter docs) are created when missing and otherwise
	// left alone unless refresh is set.
	teamOwned bool
	refresh   bool
	// fingerprint overrides contentFingerprint when part of a file may change
	// without counting as an edit.
	fingerprint func([]byte) string
}

// generatedDoc is a shared instruction file (AGENTS.md, CLAUDE.md) where only
// the fenced managed blocks belong to Rhizome.
type generatedDoc struct {
	rel    string
	header string
	blocks []managedDocBlock
	// preserve lists template block keys that stay exactly as they are.
	preserve []string
}

// generatedRemoval is a file Rhizome wrote earlier and no longer ships.
type generatedRemoval struct {
	rel   string
	group string
	// requires names a planned file that must match its new version after
	// writes, for example a SKILL.md that must stop linking a reference before
	// the reference is removed.
	requires string
	// pruneDir is removed afterwards when nothing but legacy marker files
	// remain in it.
	pruneDir string
	// retired marks a file of a skill Rhizome retired. Without a record it is
	// removed without asking.
	retired bool
}

type filePlan struct {
	files    []generatedFile
	docs     []generatedDoc
	removals []generatedRemoval
	// legacyDirs are skill folders whose old marker files are folded into the
	// record and deleted once the record owns the folder.
	legacyDirs []string
	// commandDirs hold rhizome-* command files; stale ones are cleaned up
	// when the plan is applied.
	commandDirs     []string
	currentCommands map[string]struct{}
}

func (p *filePlan) addFile(file generatedFile) {
	if file.mode == 0 {
		file.mode = 0o644
	}
	p.files = append(p.files, file)
}

type fileState uint8

const (
	stateMissing  fileState = iota // not on disk
	stateCurrent                   // already matches the new version
	stateUnedited                  // matches what Rhizome last wrote
	stateEdited                    // differs from what Rhizome last wrote
	stateLocal                     // edited, but Rhizome has no newer version
	stateDeclined                  // edited, and this version was already declined
	stateUnknown                   // no record and differs from the new version
)

// change is one decision unit: a file, a managed block, or a removal.
type change struct {
	key      string // record key
	rel      string // display path
	group    string
	state    fileState
	removal  bool
	current  []byte // on-disk content (block body for managed blocks)
	desired  []byte // nil for removals
	desiredF string // fingerprint of desired
	take     bool   // decision: write or remove
	// asked records that a person made the decision. Only then is a kept
	// version remembered as declined; batch runs keep without deciding.
	asked bool
}

// ownershipUI asks about files init cannot decide alone. A nil UI keeps every
// edited or unknown file, which is the non-interactive behavior.
type ownershipUI interface {
	// decideUnknown asks once about files with no record that differ from
	// the new version, or that Rhizome no longer ships. It returns
	// "update", "keep", or "review".
	decideUnknown(changes []*change) string
	// decideEdited reports whether to take the new version (or removal).
	decideEdited(c *change, mirrors int) bool
}

// syncReport lists what a run did, for the summary.
type syncReport struct {
	Created []string
	Updated []string
	Removed []string
	// Kept are edited files with a newer Rhizome version left as they are.
	Kept []string
	// Declined are files a person chose to keep as they are.
	Declined []string
	// Warnings name paths skipped because a folder could not be created, for
	// example a symlink that does not resolve.
	Warnings []string
}

func (r syncReport) changedPaths() []string {
	out := append(append([]string{}, r.Created...), r.Updated...)
	return append(out, r.Removed...)
}

// generatedInputs is everything that decides which files init generates.
type generatedInputs struct {
	harnesses   AgentHarnesses
	section     string
	templates   []string
	surfaceOpts AgentSurfaceOptions
	refreshDocs bool
}

// syncGeneratedFiles plans every generated file, applies the ownership rule,
// and saves the record.
func syncGeneratedFiles(projectRoot string, in generatedInputs, ui ownershipUI) (syncReport, error) {
	prepared, err := prepareGeneratedFiles(projectRoot, in)
	if err != nil {
		return syncReport{}, err
	}
	return prepared.apply(ui)
}

// preparedFiles is a classified plan: what init would write, update, keep,
// or remove. Nothing is written until apply.
type preparedFiles struct {
	root          string
	plan          *filePlan
	record        *generatedFiles
	changes       []*change
	docStates     []docState
	removeRhizome bool // the retired generated RHIZOME.md is present
}

func prepareGeneratedFiles(projectRoot string, in generatedInputs) (*preparedFiles, error) {
	record, _ := loadGeneratedFiles(projectRoot)
	plan := &filePlan{}
	if err := planAgentSurfaces(projectRoot, in.harnesses, in.section, in.templates, in.surfaceOpts, record, plan); err != nil {
		return nil, err
	}
	if err := planTemplateScaffold(projectRoot, in.templates, in.refreshDocs, plan); err != nil {
		return nil, err
	}
	if err := planRemovedStarterFiles(in.templates, in.surfaceOpts.PreserveSkillTemplates, record, plan); err != nil {
		return nil, err
	}
	prepared, err := classifyFilePlan(projectRoot, plan, record)
	if err != nil {
		return nil, err
	}
	prepared.removeRhizome = agentSurfacesEnabled(in.harnesses) && hasLegacyRhizomeFile(projectRoot)
	return prepared, nil
}

func classifyFilePlan(projectRoot string, plan *filePlan, record *generatedFiles) (*preparedFiles, error) {
	changes, docStates, err := classifyPlan(projectRoot, plan, record)
	if err != nil {
		return nil, err
	}
	return &preparedFiles{root: projectRoot, plan: plan, record: record, changes: changes, docStates: docStates}, nil
}

// applyFilePlan classifies every planned target, asks the UI about edited and
// unknown ones, then writes, removes, and saves the record.
func applyFilePlan(projectRoot string, plan *filePlan, record *generatedFiles, ui ownershipUI) (syncReport, error) {
	prepared, err := classifyFilePlan(projectRoot, plan, record)
	if err != nil {
		return syncReport{}, err
	}
	return prepared.apply(ui)
}

// removalWaitsOn returns the change for the file a removal of rel requires
// when this run would leave that file at another version, such as an edited
// skill router that still links rel; applyRemovals skips such removals. It
// returns nil when the removal can go ahead.
func (p *preparedFiles) removalWaitsOn(rel string, byKey map[string]*change) *change {
	for _, removal := range p.plan.removals {
		if removal.rel != rel || removal.requires == "" {
			continue
		}
		router := byKey[removal.requires]
		if router != nil && router.take {
			return nil
		}
		for _, file := range p.plan.files {
			if file.rel == removal.requires && fileMatches(p.root, file.rel, file.content) {
				return nil
			}
		}
		if router == nil {
			router = &change{key: removal.requires, rel: removal.requires}
		}
		return router
	}
	return nil
}

// pendingFiles groups what applying the plan would change, for the rerun
// change list and --check.
type pendingFiles struct {
	Create []string // missing files and blocks
	Update []string // files nobody edited with a newer version
	Remove []string // files Rhizome no longer ships
	// RemoveWithUpdate are files Rhizome no longer ships that go only if a
	// person takes the update to an edited file that still links them.
	RemoveWithUpdate []string
	Edited           []string // files someone edited that have a newer version
	Unknown          []string // files Rhizome cannot tell were edited
}

func (p *preparedFiles) pending() pendingFiles {
	var out pendingFiles
	seen := map[string]bool{}
	add := func(list *[]string, path string) {
		if !seen[path] {
			seen[path] = true
			*list = append(*list, path)
		}
	}
	byKey := map[string]*change{}
	for _, c := range p.changes {
		// The decisions a run makes without asking; apply decides again.
		c.take = c.state == stateMissing || c.state == stateUnedited
		byKey[c.key] = c
	}
	for _, c := range p.changes {
		// Managed blocks are listed by the shared-doc pass below, which knows
		// whether the file already exists.
		if strings.Contains(c.key, "#") && (c.state == stateMissing || c.state == stateUnedited) {
			continue
		}
		switch {
		case c.removal && c.state == stateUnedited:
			switch router := p.removalWaitsOn(c.rel, byKey); {
			case router == nil:
				add(&out.Remove, c.rel)
			case router.state == stateEdited || router.state == stateUnknown:
				// Taking that update removes this file too, so the
				// decision lists it.
				add(&out.RemoveWithUpdate, c.rel)
			}
		case c.state == stateMissing:
			add(&out.Create, c.rel)
		case c.state == stateUnedited:
			add(&out.Update, c.rel)
		case c.state == stateEdited:
			add(&out.Edited, c.key)
		case c.state == stateUnknown:
			add(&out.Unknown, c.key)
		}
	}
	// A shared instruction file can change without a block changing, for
	// example when blocks are reordered.
	for i, doc := range p.plan.docs {
		state := p.docStates[i]
		if renderDoc(doc, state, byKey) == state.existing {
			continue
		}
		if state.exists {
			add(&out.Update, doc.rel)
		} else {
			add(&out.Create, doc.rel)
		}
	}
	if p.removeRhizome {
		add(&out.Remove, "RHIZOME.md")
	}
	// Old marker files go once the record owns files in their folder.
	for _, dir := range p.plan.legacyDirs {
		if !p.ownsAfterApply(dir) {
			continue
		}
		for _, name := range legacyManagedSkillFiles {
			if _, err := os.Stat(filepath.Join(p.root, filepath.FromSlash(dir), name)); err == nil {
				add(&out.Remove, dir+"/"+name)
			}
		}
	}
	if p.recordStale() {
		rel := path.Join(obsidian.RhizomeDirName, generatedFilesName)
		if _, err := os.Stat(generatedFilesPath(p.root)); err == nil {
			add(&out.Update, rel)
		} else {
			add(&out.Create, rel)
		}
	}
	return out
}

// ownsAfterApply reports whether the record will own a file under dir once
// the run applies.
func (p *preparedFiles) ownsAfterApply(dir string) bool {
	if p.record.ownsUnder(dir) {
		return true
	}
	for _, c := range p.changes {
		if !c.removal && strings.HasPrefix(c.key, dir+"/") && (c.take || c.state == stateCurrent) {
			return true
		}
	}
	return false
}

// recordStale reports whether applying would rewrite generated-files.yml
// even when every file is current: the record is missing, unreadable, or
// lacks a current file.
func (p *preparedFiles) recordStale() bool {
	if p.record.unreadable {
		return true
	}
	known := 0
	for _, c := range p.changes {
		if c.removal || c.state != stateCurrent {
			continue
		}
		if p.record.Written[c.key] != c.desiredF {
			return true
		}
		known++
	}
	_, err := os.Stat(generatedFilesPath(p.root))
	return err != nil && (known > 0 || len(p.record.Written) > 0)
}

func (p *preparedFiles) apply(ui ownershipUI) (syncReport, error) {
	projectRoot, plan, record := p.root, p.plan, p.record
	recordStale := p.recordStale()
	resolveChanges(p.changes, ui)

	report := syncReport{}
	byKey := map[string]*change{}
	for _, c := range p.changes {
		byKey[c.key] = c
	}

	for _, file := range plan.files {
		c := byKey[file.rel]
		if err := applyFileChange(projectRoot, file, c, record, &report); err != nil {
			return report, err
		}
	}
	for i, doc := range plan.docs {
		if err := applyDocChanges(projectRoot, doc, p.docStates[i], byKey, record, &report); err != nil {
			return report, err
		}
	}
	if err := applyRemovals(projectRoot, plan, byKey, record, &report); err != nil {
		return report, err
	}

	for _, dir := range plan.legacyDirs {
		// Markers are the only ownership evidence until the record has some.
		if !record.ownsUnder(dir) {
			continue
		}
		for _, name := range legacyManagedSkillFiles {
			if _, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(dir), name)); err == nil {
				report.Removed = append(report.Removed, dir+"/"+name)
			}
		}
		if err := removeLegacySkillFiles(filepath.Join(projectRoot, filepath.FromSlash(dir))); err != nil {
			return report, err
		}
	}
	for _, dir := range plan.commandDirs {
		if _, err := cleanupStaleArtifacts(filepath.Join(projectRoot, filepath.FromSlash(dir)), plan.currentCommands); err != nil {
			return report, err
		}
	}
	if err := removeLegacyRejections(projectRoot); err != nil {
		return report, err
	}
	if p.removeRhizome {
		removed, err := removeLegacyRhizomeFile(projectRoot)
		if err != nil {
			return report, err
		}
		if removed {
			report.Removed = append(report.Removed, "RHIZOME.md")
		}
	}
	sort.Strings(report.Kept)
	if recordStale {
		rel := path.Join(obsidian.RhizomeDirName, generatedFilesName)
		if _, err := os.Stat(generatedFilesPath(projectRoot)); err == nil {
			report.Updated = append(report.Updated, rel)
		} else {
			report.Created = append(report.Created, rel)
		}
	}
	return report, record.save(projectRoot)
}
