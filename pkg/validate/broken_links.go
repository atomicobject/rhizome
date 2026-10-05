package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"gopkg.in/yaml.v3"
)

// Broken-link issue codes are public validation contracts. Keep note,
// heading, and block failures distinct so remediation never confuses an
// existing note with a missing target.
const (
	IssueCodeBrokenNoteLink    = "broken_note_link"
	IssueCodeBrokenHeadingLink = "broken_heading_link"
	IssueCodeBrokenBlockLink   = "broken_block_link"
)

// IssueCodePlaceholderNoteLink marks an unresolved link whose target never
// existed: a deliberate Obsidian placeholder rather than a link that broke.
const IssueCodePlaceholderNoteLink = "placeholder_note_link"

// RunBrokenLinks checks for broken wikilinks in vault notes.
func RunBrokenLinks(runCtx RunContext, opts Options) CheckResult {
	return RunBrokenLinksContext(context.Background(), runCtx, opts)
}

// unresolvedLinkScan is one broken-link scan split by git evidence into links
// that broke (the default gate) and placeholders that never had a target.
type unresolvedLinkScan struct {
	broke        []obsidian.BrokenLink
	placeholders []obsidian.BrokenLink
	notes        []string
	allNotes     []string
	notesErr     error
	scanErr      error
	groups       map[brokenLinkGroupKey]brokenLinkGroup
	brokeGroups  []brokenLinkGroup
}

// sharedUnresolvedLinkScan runs the scan once per suite run. Checks in one run
// share the run's context, options, and immutable source view.
type sharedUnresolvedLinkScan struct {
	once sync.Once
	scan unresolvedLinkScan
	err  error
}

func unresolvedLinksFor(ctx context.Context, runCtx RunContext, opts Options) (unresolvedLinkScan, error) {
	shared := runCtx.unresolvedLinks
	if shared == nil {
		return scanUnresolvedLinks(ctx, runCtx, opts)
	}
	shared.once.Do(func() {
		shared.scan, shared.err = scanUnresolvedLinks(ctx, runCtx, opts)
	})
	return shared.scan, shared.err
}

func isStarterDir(dir, name string) bool {
	content, err := os.ReadFile(filepath.Join(dir, "template.yaml"))
	if err != nil {
		return false
	}
	var meta struct {
		ID string `yaml:"id"`
	}
	return yaml.Unmarshal(content, &meta) == nil && strings.TrimSpace(meta.ID) == name
}

func scanUnresolvedLinks(ctx context.Context, runCtx RunContext, opts Options) (unresolvedLinkScan, error) {
	var scan unresolvedLinkScan
	broken, err := obsidian.FindBrokenLinksContext(ctx, runCtx.VaultDef, runCtx.NoteReader, obsidian.BrokenLinksOptions{
		WikilinkOptions: obsidian.WikilinkOptions{
			SkipAnchors: opts.SkipAnchors,
			SkipEmbeds:  opts.SkipEmbeds,
		},
		IncludeImages: opts.IncludeImages,
	})
	scan.scanErr = err
	if ctxErr := ctx.Err(); ctxErr != nil {
		return scan, ctxErr
	}
	// A starter's agents/AGENTS.md block is authored relative to the
	// repository that will receive it, so its links cannot be resolved against
	// this vault. Like the init registry, recognize a starter by a
	// template.yaml whose id matches its directory name.
	starterBlocks := map[string]bool{}
	broken = slices.DeleteFunc(broken, func(link obsidian.BrokenLink) bool {
		source := path.Clean(filepath.ToSlash(link.Source))
		if path.Base(source) != "AGENTS.md" || path.Base(path.Dir(source)) != "agents" {
			return false
		}
		exempt, seen := starterBlocks[source]
		if !seen {
			starter := path.Dir(path.Dir(source))
			exempt = isStarterDir(filepath.Join(runCtx.VaultDef.BasePath(), filepath.FromSlash(starter)), path.Base(starter))
			starterBlocks[source] = exempt
		}
		return exempt
	})
	policy, err := placeholderPolicy(runCtx)
	if err != nil {
		return scan, err
	}
	var history linkHistory
	needsHistory := false
	for _, link := range broken {
		if normalizeBrokenLinkReason(link.Reason) == obsidian.BrokenLinkReasonNoteMissing {
			needsHistory = true
			break
		}
	}
	if needsHistory {
		history = loadLinkHistory(ctx, runCtx.VaultDef.BasePath())
	}
	for _, link := range broken {
		missingNote := normalizeBrokenLinkReason(link.Reason) == obsidian.BrokenLinkReasonNoteMissing
		if missingNote && policy != obsidian.PlaceholderLinksStrict && history.available && !history.broke(link.Target) {
			scan.placeholders = append(scan.placeholders, link)
			continue
		}
		scan.broke = append(scan.broke, link)
	}
	switch {
	case !needsHistory || policy == obsidian.PlaceholderLinksStrict:
	case !history.available:
		scan.notes = append(scan.notes, fmt.Sprintf("Git history is unavailable (%s), so every unresolved link counts as broken; placeholders cannot be told apart from links that broke.", history.unavailable))
	case len(scan.placeholders) > 0:
		scan.notes = append(scan.notes, fmt.Sprintf("%d placeholder link(s) name notes that never existed in git history and are not counted; list them with `rzm validate placeholder-links`.", len(scan.placeholders)))
	}

	scan.allNotes, scan.notesErr = getNotesListContext(ctx, runCtx.VaultDef, runCtx.NoteReader)
	matcher := newBrokenLinkMatcherIndex(scan.allNotes)
	if runCtx.brokenLinkCandidates != nil {
		matcher = *runCtx.brokenLinkCandidates
	}
	evidence := retargetEvidence{history: history, noteMetadata: runCtx.NoteMetadata, noteContent: func(notePath string) (string, error) {
		return runCtx.NoteReader.GetContents(runCtx.VaultDef, notePath)
	}}
	brokeGroups, err := groupBrokenLinks(ctx, scan.broke, scan.allNotes, matcher, evidence)
	if err != nil {
		return scan, err
	}
	placeholderGroups, err := groupBrokenLinks(ctx, scan.placeholders, scan.allNotes, matcher, evidence)
	if err != nil {
		return scan, err
	}
	scan.brokeGroups = brokeGroups
	scan.groups = map[brokenLinkGroupKey]brokenLinkGroup{}
	for _, group := range append(brokeGroups, placeholderGroups...) {
		scan.groups[group.key] = group
	}
	return scan, nil
}

func placeholderPolicy(runCtx RunContext) (string, error) {
	vaultPath := strings.TrimSpace(runCtx.VaultDef.BasePath())
	if vaultPath == "" {
		vaultPath = runCtx.VaultPath
	}
	local, err := obsidian.LoadLocalConfig(vaultPath)
	if errors.Is(err, obsidian.ErrNoLocalConfig) || vaultPath == "" {
		return obsidian.PlaceholderLinksHistory, nil
	}
	if err != nil {
		return "", fmt.Errorf("load broken-links config: %w", err)
	}
	switch policy := strings.TrimSpace(local.Validation.BrokenLinks.Placeholders); policy {
	case "", obsidian.PlaceholderLinksHistory:
		return obsidian.PlaceholderLinksHistory, nil
	case obsidian.PlaceholderLinksStrict:
		return policy, nil
	default:
		return "", fmt.Errorf("validation.brokenLinks.placeholders must be %q or %q, got %q", obsidian.PlaceholderLinksHistory, obsidian.PlaceholderLinksStrict, policy)
	}
}

func groupKeyFor(link obsidian.BrokenLink) brokenLinkGroupKey {
	return brokenLinkGroupKey{
		target: link.Target, reason: normalizeBrokenLinkReason(link.Reason),
		fragment: link.Fragment, markdown: link.Markdown,
	}
}

// RunBrokenLinksContext propagates cancellation through the health scan and
// retains partial broken-link findings when individual note reads fail. Only
// links that broke are issues; placeholders are counted in a note.
func RunBrokenLinksContext(ctx context.Context, runCtx RunContext, opts Options) CheckResult {
	result := CheckResult{Name: CheckBrokenLinks, OK: true}
	scan, err := unresolvedLinksFor(ctx, runCtx, opts)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	if scan.scanErr != nil {
		result.OK = false
		result.Error = scan.scanErr.Error()
	}
	result.Notes = slices.Clone(scan.notes)
	result.Fixes, err = buildBrokenLinkFixesFromGroups(ctx, scan.brokeGroups, scan.allNotes, scan.notesErr)
	if err != nil {
		result.OK = false
		fixError := fmt.Sprintf("identify broken-link issue: %v", err)
		if result.Error == "" {
			result.Error = fixError
		} else {
			result.Error += "; " + fixError
		}
		result.Fixes = nil
		return result
	}
	result.IssueCount = len(scan.broke)
	if len(scan.broke) == 0 {
		result.Summary = "no broken links"
		return result
	}
	if scan.notesErr != nil {
		result.OK = false
		if result.Error == "" {
			result.Error = scan.notesErr.Error()
		}
	}
	result.Issues = make([]Issue, 0, len(scan.broke))
	for _, link := range scan.broke {
		result.Issues = append(result.Issues, brokenLinkValidationIssue(link, scan.groups[groupKeyFor(link)]))
	}
	result.Summary = fmt.Sprintf("%d broken links", len(scan.broke))
	return result
}

// RunPlaceholderLinks lists unresolved links whose target never existed in
// git history: deliberate placeholders that the broken-links gate does not
// count. It is a maintenance pass in the audit suite.
func RunPlaceholderLinks(ctx context.Context, runCtx RunContext, opts Options) CheckResult {
	result := CheckResult{Name: CheckPlaceholderLinks, OK: true}
	scan, err := unresolvedLinksFor(ctx, runCtx, opts)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	if scan.scanErr != nil {
		result.OK = false
		result.Error = scan.scanErr.Error()
	}
	for _, note := range scan.notes {
		if !strings.Contains(note, "placeholder-links") {
			result.Notes = append(result.Notes, note)
		}
	}
	result.IssueCount = len(scan.placeholders)
	if len(scan.placeholders) == 0 {
		result.Summary = "no placeholder links"
		return result
	}
	result.Issues = make([]Issue, 0, len(scan.placeholders))
	for _, link := range scan.placeholders {
		issue := brokenLinkValidationIssue(link, scan.groups[groupKeyFor(link)])
		issue.Code = IssueCodePlaceholderNoteLink
		issue.Message = fmt.Sprintf("Placeholder link [[%s]]: no note by that name has ever existed; create the note, or leave the placeholder deliberately.", authoredBrokenLinkTarget(link.Target, link.Fragment))
		result.Issues = append(result.Issues, issue)
	}
	result.Summary = fmt.Sprintf("%d placeholder links", len(scan.placeholders))
	return result
}

func getNotesListContext(ctx context.Context, vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader) ([]string, error) {
	if reader, ok := noteReader.(obsidian.ContextNoteListReader); ok {
		return reader.GetNotesListContext(ctx, vaultDef)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	notes, err := noteReader.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

func brokenLinkValidationIssue(link obsidian.BrokenLink, group brokenLinkGroup) Issue {
	var candidates []string
	var confidence string
	if normalizeBrokenLinkReason(link.Reason) == obsidian.BrokenLinkReasonNoteMissing {
		candidates, confidence = group.candidates()
	}
	linkType := string(link.LinkType)
	if link.Markdown {
		linkType = "mdlink"
	}
	// The variant is the missing target: the note for a missing note (any
	// fragment), the note and fragment for a missing heading or block.
	authoredTarget := authoredBrokenLinkTarget(link.Target, link.Fragment)
	missing := authoredTarget
	if normalizeBrokenLinkReason(link.Reason) == obsidian.BrokenLinkReasonNoteMissing {
		missing = link.Target
	}
	return Issue{
		Code:    brokenLinkIssueCode(link.Reason),
		Path:    link.Source,
		Source:  link.Source,
		Target:  authoredTarget,
		Message: brokenLinkIssueMessage(link),
		Line:    link.Line,
		Variant: newIssueVariant(missing, missing),
		Data: mustMarshal(BrokenLinkData{
			Target: link.Target, LinkType: linkType, Alias: link.Alias,
			Fragment: link.Fragment, Reason: string(link.Reason), Candidates: candidates,
			CandidateConfidence: confidence,
		}),
	}
}

func brokenLinkIssueMessage(link obsidian.BrokenLink) string {
	authoredTarget := authoredBrokenLinkTarget(link.Target, link.Fragment)
	switch normalizeBrokenLinkReason(link.Reason) {
	case obsidian.BrokenLinkReasonHeadingMissing:
		return fmt.Sprintf("Missing heading fragment %q in [[%s]]; restore that heading or retarget the authored link.", link.Fragment, authoredTarget)
	case obsidian.BrokenLinkReasonBlockMissing:
		return fmt.Sprintf("Missing block fragment %q in [[%s]]; restore that block ID or retarget the authored link.", link.Fragment, authoredTarget)
	default:
		return fmt.Sprintf("Missing note target [[%s]]; create the note or retarget the authored link without dropping its fragment.", authoredTarget)
	}
}

func brokenLinkIssueCode(reason obsidian.BrokenLinkReason) string {
	switch normalizeBrokenLinkReason(reason) {
	case obsidian.BrokenLinkReasonHeadingMissing:
		return IssueCodeBrokenHeadingLink
	case obsidian.BrokenLinkReasonBlockMissing:
		return IssueCodeBrokenBlockLink
	default:
		return IssueCodeBrokenNoteLink
	}
}

func normalizeBrokenLinkReason(reason obsidian.BrokenLinkReason) obsidian.BrokenLinkReason {
	if reason == "" {
		return obsidian.BrokenLinkReasonNoteMissing
	}
	return reason
}
