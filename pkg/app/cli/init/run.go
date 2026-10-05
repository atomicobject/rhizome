// Docs:
// - [[init-starter-workflow#^spec-0038-us1]]
// - [[init-starter-workflow#^spec-0038-us2]]
// - [[init-starter-workflow#^spec-0038-us3]]
// - [[init-template-architecture]]
// - [Init (Hub)](docs/hubs/Init (Hub).md)
package init

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// checkSkillTemplates fails before any write when a skill template for
// the chosen workflows does not render.
func checkSkillTemplates(templates []string) error {
	if len(templates) == 0 {
		return nil
	}
	_, _, err := loadAllSkillTemplatesWithReport(templates)
	return err
}

// Run executes the init workflow: detect, ask only in a terminal, and write.
func Run(opts RunOptions) error {
	binaryManager, err := normalizeBinaryManagerOption(opts)
	if err != nil {
		return err
	}
	opts.BinaryManager = binaryManager
	workflows, err := parseWorkflowOption(opts.Workflow)
	if err != nil {
		return err
	}
	workflowChosen := strings.TrimSpace(opts.Workflow) != ""
	if _, err := normalizeSearchOption(opts.Search); err != nil {
		return err
	}
	if _, err := agentFlagModes(opts.Agents); err != nil {
		return err
	}
	if opts.Dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		opts.Dir = cwd
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	// --check only reports, so it never asks.
	if opts.Check {
		opts.Interactive = false
	}
	reader := bufio.NewReader(opts.Stdin)

	// One credential session per init run: every prompt site shares its keys
	// and skips, so each credential is asked for at most once per run.
	// Docs: [[init-starter-workflow#^spec-0038-us5]]
	var sessionOpts []credentials.Option
	if opts.Interactive {
		sessionOpts = append(sessionOpts, credentials.WithPrompts(reader, opts.Stdout))
	}
	r := &initRun{opts: opts, reader: reader, session: credentials.NewSession(sessionOpts...)}
	// Only a person at a terminal decides about edited files; every other run
	// keeps them and lists them.
	// Docs: [[init-starter-workflow#^SPEC-0038-US3-AC4]]
	if opts.Interactive {
		r.ui = newTerminalOwnershipUI(reader, opts.Stdout)
	}

	if !opts.Check {
		root, migrated, err := migrateLegacyWorkflowConfig(opts.Dir)
		if err != nil {
			return err
		}
		if migrated {
			fmt.Fprintf(opts.Stdout, "Migrated v0.49 workflow state to %s/.rhizome/workflows.yml\n", root)
		}
	}

	r.layout, err = DetectLayout(opts.Dir)
	if err != nil {
		return err
	}
	if !r.layout.HasExistingConfig && hasStarterManagementOption(opts) {
		return fmt.Errorf("--eject and --restore need an existing Rhizome setup; run rzm init first")
	}
	includeIgnored, err := includeIgnoredInputs(r.layout.ProjectRoot, opts.IncludeIgnored)
	if err != nil {
		return err
	}
	mergeDeferredIgnoredCodeSuggestions(&r.layout, includeIgnored)
	// Non-interactive runs cannot ask about ignored nested repositories, so
	// they name them and the flag that includes them.
	// Docs: [[init-starter-workflow#^SPEC-0038-US6-AC5]]
	if !opts.Interactive {
		var candidates []IgnoredRepoCandidate
		for _, candidate := range r.layout.IgnoredRepoCandidates {
			if !contains(includeIgnored, candidate.Rel) {
				candidates = append(candidates, candidate)
			}
		}
		noticeIgnoredSubtreeCandidates(opts.Stdout, candidates)
	}
	if !workflowChosen {
		if workflows, err = inferTemplateChoices(r.layout.ProjectRoot, &r.layout.ExistingLocal); err != nil {
			return err
		}
	}
	s := setup{workflows: workflows, includeIgnored: includeIgnored}
	if r.layout.HasExistingConfig {
		s.cfg = r.layout.ExistingLocal
		return r.rerun(s)
	}
	return r.firstRun(s, workflowChosen)
}

// firstRun sets up a repository that has no Rhizome config.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US1]]
func (r *initRun) firstRun(s setup, workflowChosen bool) error {
	out := r.opts.Stdout
	if !r.opts.Interactive && !workflowChosen {
		s.workflows = []string{templateAgenticEngineering}
	}
	if _, err := applyBinaryManagerOption(&s.cfg, r.opts); err != nil {
		return err
	}
	outcome, err := r.runFirstRun(&s, workflowChosen)
	if errors.Is(err, errSetupCancelled) {
		if outcome.savedKey != "" {
			fmt.Fprintf(out, "Setup cancelled; nothing was written to this repository. The %s you pasted stays saved in ~/.config/rhizome/config.yml.\n", outcome.savedKey)
		} else {
			fmt.Fprintln(out, "Setup cancelled; nothing was written.")
		}
		return nil
	}
	if err != nil {
		return err
	}
	// Render guidance from the config as it will be written, so the next run
	// renders the same blocks.
	pruneConfigForWrite(&s.cfg)
	res, err := r.resolve(s)
	if err != nil {
		return err
	}
	if r.opts.Check {
		fmt.Fprintln(out)
		fmt.Fprintln(out, styleHeading(out, "Changes"))
		lines := append([]planLine{{mark: "+", text: "Create .rhizome/config.yml"}}, skipLines(s.skips)...)
		lines = append(lines, fileLines(res.files.pending())...)
		printPlanLines(out, lines, true)
		return ErrChangesPending
	}
	report, err := r.apply(s, res)
	if err != nil {
		return err
	}
	// Report what was written before the pinned-binary download, so a slow
	// or failed download does not hide it.
	printSetupSummary(out, report, res.effective, s.skips, outcome.savedKey)
	if err := ensurePinnedBinary(r.layout.ProjectRoot, res.cfg, r.opts); err != nil {
		return err
	}
	indexed, indexErr := r.offerIndex("Build the search index now? [Y/n]: ")
	printNextSteps(out, report, searchHint(outcome.provider, outcome.ready), !indexed)
	return indexErr
}

// plannedStarterNotePaths lists the starter doc paths that will exist after
// this run, so notes includes can cover them before guidance is rendered.
func plannedStarterNotePaths(templates []string) []string {
	var out []string
	for _, template := range templates {
		files, err := loadStarterTemplates(template)
		if err != nil {
			continue
		}
		for _, file := range files {
			out = append(out, filepath.ToSlash(file.Path))
		}
	}
	return out
}

// clearRetiredWorkflowManagement removes starter family update policies and
// source fingerprints, which the generated-files record replaces.
func clearRetiredWorkflowManagement(cfg *obsidian.LocalConfig) bool {
	mgmt := &cfg.WorkflowTemplateManagement
	if isEmptyWorkflowTemplateUpdatePolicy(mgmt.UpdatePolicy) && len(mgmt.SourceFingerprints) == 0 {
		return false
	}
	mgmt.UpdatePolicy = obsidian.WorkflowTemplateUpdatePolicy{}
	mgmt.SourceFingerprints = nil
	return true
}

func ejectedAgenticEngineering(ejected []string) bool {
	return stringSet(normalizeMetadataIDsWithCanonicalIDs(ejected))[templateAgenticEngineering]
}

func hasStarterManagementOption(opts RunOptions) bool {
	return strings.TrimSpace(opts.Eject) != "" || strings.TrimSpace(opts.Restore) != ""
}
