package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/loginpath"
)

// Doctor check names, stable for `rzm tasks doctor --json`.
const (
	CheckTrust       = "trust"
	CheckPrimaryRef  = "primary-ref"
	CheckPush        = "push"
	CheckTaskFile    = "task-file"
	CheckObsidianGit = "obsidian-git"
	CheckCommand     = "command"
	CheckHarness     = "harness"
	CheckMCP         = "mcp"
	CheckDeny        = "deny"
)

// DoctorOptions choose what `rzm tasks doctor` checks.
type DoctorOptions struct {
	// Task limits the report to one task; empty means every task.
	Task string
	// Checkout is the host checkout tasks land in; empty means the main
	// checkout.
	Checkout string
	// Push runs `git push --dry-run` against the primary remote; the
	// caller passes the person's per-machine push setting.
	Push bool
}

// DoctorCheck is one thing doctor looked at.
type DoctorCheck struct {
	Check  string `json:"check"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	// Skipped marks a check doctor did not run, as a push check in an
	// untrusted repository; it is not OK.
	Skipped bool `json:"skipped,omitempty"`
}

// TaskDoctor is one task's checks.
type TaskDoctor struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	OK     bool          `json:"ok"`
	Checks []DoctorCheck `json:"checks"`
}

// DoctorReport is the whole `rzm tasks doctor` result: repository checks,
// then each task's (SPEC-0127 US11).
type DoctorReport struct {
	OK     bool          `json:"ok"`
	Checks []DoctorCheck `json:"checks"`
	Tasks  []TaskDoctor  `json:"tasks"`
}

// Doctor reports what tasks need on this machine and whether they have it.
// It runs nothing but Git, the harness status probe, and the harness's MCP
// list, and changes no state.
func (s *Service) Doctor(ctx context.Context, opts DoctorOptions) (DoctorReport, error) {
	report := DoctorReport{Checks: []DoctorCheck{}, Tasks: []TaskDoctor{}}
	trust := DoctorCheck{Check: CheckTrust, OK: true, Detail: "the repository is trusted"}
	untrusted := s.untrusted()
	if untrusted != "" {
		trust.OK, trust.Detail = false, untrusted
	}
	report.Checks = append(report.Checks, trust)
	ref, commit, err := s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef)
	if err != nil {
		report.Checks = append(report.Checks, DoctorCheck{Check: CheckPrimaryRef, Detail: err.Error()})
		return report.finish(), nil
	}
	remote, branch, remoteErr := s.primaryRemote(ctx, ref)
	report.Checks = append(report.Checks, s.freshness(ref, commit, remote, remoteErr))
	switch {
	case opts.Push && untrusted != "":
		// A push runs the repository's hooks and its configured commands.
		report.Checks = append(report.Checks, DoctorCheck{Check: CheckPush, Skipped: true, Detail: "skipped: the repository is not trusted, and a push would run its hooks"})
	case opts.Push:
		report.Checks = append(report.Checks, s.pushCheck(ctx, commit, remote, branch, remoteErr))
	}
	checkout := opts.Checkout
	if checkout == "" {
		checkout = s.opts.Repo.MainCheckout
	}
	if checkout != "" {
		report.Checks = append(report.Checks, obsidianGitCheck(checkout))
	}
	defs, problems, err := LoadDefinitions(ctx, s.opts.Repo, commit)
	if err != nil {
		return report, err
	}
	for _, p := range problems {
		report.Checks = append(report.Checks, DoctorCheck{Check: CheckTaskFile, Detail: p.Path + ": " + p.Message})
	}
	if opts.Task != "" {
		i := slices.IndexFunc(defs, func(d Definition) bool { return d.ID == opts.Task })
		if i < 0 {
			return report, fmt.Errorf("task %q is not declared on the primary ref", opts.Task)
		}
		defs = defs[i : i+1]
	}
	probe := harnessProbe{agent: s.opts.Agent, dir: s.opts.Repo.MainCheckout}
	for _, def := range defs {
		report.Tasks = append(report.Tasks, probe.task(ctx, def))
	}
	return report.finish(), nil
}

func (r DoctorReport) finish() DoctorReport {
	r.OK = allOK(r.Checks)
	for _, task := range r.Tasks {
		r.OK = r.OK && task.OK
	}
	return r
}

func allOK(checks []DoctorCheck) bool {
	return !slices.ContainsFunc(checks, func(c DoctorCheck) bool { return !c.OK })
}

// primaryRemote names the remote and branch the primary ref tracks: the
// ref itself when it is a remote-tracking branch, or its upstream.
// ponytail: a remote whose name contains "/" is split at the first one.
func (s *Service) primaryRemote(ctx context.Context, ref string) (remote, branch string, err error) {
	full, err := s.opts.Repo.git(ctx, "rev-parse", "--symbolic-full-name", ref)
	if err != nil {
		return "", "", err
	}
	if strings.HasPrefix(full, "refs/heads/") {
		if full, err = s.opts.Repo.git(ctx, "rev-parse", "--symbolic-full-name", ref+"@{upstream}"); err != nil {
			return "", "", fmt.Errorf("%s is a local branch with no upstream", ref)
		}
	}
	rest, ok := strings.CutPrefix(full, "refs/remotes/")
	if remote, branch, found := strings.Cut(rest, "/"); ok && found {
		return remote, branch, nil
	}
	return "", "", fmt.Errorf("%s is not a branch on a remote", ref)
}

// freshness says how old the primary ref is: when the repository last
// fetched. It is information only; a repository whose primary ref is local
// has nothing to fetch.
func (s *Service) freshness(ref, commit, remote string, remoteErr error) DoctorCheck {
	check := DoctorCheck{Check: CheckPrimaryRef, OK: true}
	switch at, ok := s.opts.Repo.FetchedAt(); {
	case remoteErr != nil:
		check.Detail = fmt.Sprintf("%s at %s; %v, so nothing is fetched", ref, short(commit), remoteErr)
	case !ok:
		check.Detail = fmt.Sprintf("%s at %s; never fetched from %s", ref, short(commit), remote)
	default:
		check.Detail = fmt.Sprintf("%s at %s; last fetched %s ago (%s)", ref, short(commit), s.opts.Now().Sub(at).Round(time.Minute), at.Format(time.RFC3339))
	}
	return check
}

// pushCheck asks the remote whether a push to the primary branch would be
// accepted, pushing the commit it already has so nothing would change. The
// repository's hooks do not run, as for the runner's own Git commands.
func (s *Service) pushCheck(ctx context.Context, commit, remote, branch string, remoteErr error) DoctorCheck {
	check := DoctorCheck{Check: CheckPush}
	if remoteErr != nil {
		check.Detail = "push is on, but " + remoteErr.Error()
		return check
	}
	hooks, err := os.MkdirTemp("", "rhizome-no-hooks-*")
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	defer os.RemoveAll(hooks)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	target := fmt.Sprintf("%s:refs/heads/%s", commit, branch)
	if _, err := s.opts.Repo.git(ctx, "-c", "core.hooksPath="+hooks, "push", "--dry-run", "--quiet", remote, target); err != nil {
		check.Detail = redactURLCredentials(err.Error())
		return check
	}
	check.OK, check.Detail = true, fmt.Sprintf("git push --dry-run to %s %s succeeds", remote, branch)
	return check
}

// obsidianGitCheck reports Obsidian Git committing or pulling on a timer in
// the host checkout, which moves HEAD under an in-place run. The plugin's
// settings start auto commit-and-sync when autoSaveInterval > 0 and auto
// pull when autoPullInterval > 0 (minutes); Obsidian loads only plugins
// listed in community-plugins.json.
func obsidianGitCheck(checkout string) DoctorCheck {
	check := DoctorCheck{Check: CheckObsidianGit, OK: true, Detail: "Obsidian Git is not enabled in " + checkout}
	var enabled []string
	if data, err := os.ReadFile(filepath.Join(checkout, ".obsidian", "community-plugins.json")); err != nil || json.Unmarshal(data, &enabled) != nil || !slices.Contains(enabled, "obsidian-git") {
		return check
	}
	var settings struct {
		AutoSaveInterval float64 `json:"autoSaveInterval"`
		AutoPullInterval float64 `json:"autoPullInterval"`
	}
	data, err := os.ReadFile(filepath.Join(checkout, ".obsidian", "plugins", "obsidian-git", "data.json"))
	if errors.Is(err, os.ErrNotExist) {
		check.Detail = "Obsidian Git is enabled with its defaults, which do not commit or pull on a timer"
		return check
	}
	if err == nil {
		err = json.Unmarshal(data, &settings)
	}
	if err != nil {
		check.OK, check.Detail = false, "read Obsidian Git settings: "+err.Error()
		return check
	}
	var timers []string
	if settings.AutoSaveInterval > 0 {
		timers = append(timers, fmt.Sprintf("auto-commits every %g minutes", settings.AutoSaveInterval))
	}
	if settings.AutoPullInterval > 0 {
		timers = append(timers, fmt.Sprintf("auto-pulls every %g minutes", settings.AutoPullInterval))
	}
	if len(timers) == 0 {
		check.Detail = "Obsidian Git is enabled without auto commit or pull"
		return check
	}
	check.OK = false
	check.Detail = fmt.Sprintf("Obsidian Git %s in %s; that moves HEAD under in-place task runs, so turn its timers off", strings.Join(timers, " and "), checkout)
	return check
}

// harnessProbe checks tasks' harness needs, probing each harness once.
type harnessProbe struct {
	agent AgentRunner
	dir   string
	seen  map[harness.Kind]*harnessFacts
}

type harnessFacts struct {
	signIn  DoctorCheck
	listed  bool
	servers []harness.MCPServerState
	listErr error
}

func (p *harnessProbe) task(ctx context.Context, def Definition) TaskDoctor {
	td := TaskDoctor{ID: def.ID, Name: def.Name, Checks: []DoctorCheck{}}
	for _, name := range def.Requires.Commands {
		check := DoctorCheck{Check: CheckCommand, OK: true, Detail: fmt.Sprintf("%q is the runtime's own rzm", name)}
		if name != "rzm" {
			if path, err := loginpath.Look(name); err != nil {
				check.OK, check.Detail = false, err.Error()
			} else {
				check.Detail = fmt.Sprintf("%q is %s", name, path)
			}
		}
		td.Checks = append(td.Checks, check)
	}
	if strings.TrimSpace(def.Prompt) != "" {
		td.Checks = append(td.Checks, p.harnessChecks(ctx, def)...)
	}
	td.OK = allOK(td.Checks)
	return td
}

func (p *harnessProbe) harnessChecks(ctx context.Context, def Definition) []DoctorCheck {
	resolver, ok := p.agent.(harnessResolver)
	if !ok {
		return []DoctorCheck{{Check: CheckHarness, Detail: "no agent harness is available to run the prompt"}}
	}
	kind, driver, err := resolver.Harness(ctx, def.ID)
	if err != nil {
		return []DoctorCheck{{Check: CheckHarness, Detail: err.Error()}}
	}
	if p.seen == nil {
		p.seen = map[harness.Kind]*harnessFacts{}
	}
	facts := p.seen[kind]
	if facts == nil {
		facts = &harnessFacts{signIn: signInCheck(ctx, kind, driver)}
		p.seen[kind] = facts
	}
	checks := []DoctorCheck{facts.signIn}
	if len(def.Deny) > 0 {
		deny := DoctorCheck{Check: CheckDeny, OK: true, Detail: fmt.Sprintf("%s refuses %s", kind, strings.Join(def.Deny, ", "))}
		if !enforcesDeny(kind) {
			deny.OK, deny.Detail = false, fmt.Sprintf("%s cannot enforce deny; use Claude Code for this task or remove deny", kind)
		}
		checks = append(checks, deny)
	}
	if len(def.Requires.MCP) > 0 && !facts.listed {
		facts.servers, facts.listErr = listMCP(ctx, kind, driver, p.dir)
		facts.listed = true
	}
	if facts.listErr != nil {
		return append(checks, DoctorCheck{Check: CheckMCP, Detail: facts.listErr.Error()})
	}
	for _, name := range def.Requires.MCP {
		ok, detail := mcpState(kind, name, facts.servers)
		checks = append(checks, DoctorCheck{Check: CheckMCP, OK: ok, Detail: detail})
	}
	return checks
}

func signInCheck(ctx context.Context, kind harness.Kind, driver harness.Harness) DoctorCheck {
	check := DoctorCheck{Check: CheckHarness}
	status, err := driver.Status(ctx)
	switch {
	case err != nil:
		check.Detail = fmt.Sprintf("%s status: %v", kind, err)
	case !status.Installed:
		check.Detail = fmt.Sprintf("%s is not installed", kind)
	case !status.LoggedIn:
		check.Detail = fmt.Sprintf("%s is not signed in; run `%s`", kind, status.LoginHint)
	default:
		check.OK, check.Detail = true, fmt.Sprintf("%s %s is signed in", kind, status.Version)
	}
	return check
}
