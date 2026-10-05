package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type HeadingRenameUpgradeMode string

const (
	HeadingRenameUpgradeAuto   HeadingRenameUpgradeMode = "auto"
	HeadingRenameUpgradeAlways HeadingRenameUpgradeMode = "always"
	HeadingRenameUpgradeNever  HeadingRenameUpgradeMode = "never"
)

type HeadingRenameFallbackMode string

const (
	HeadingRenameFallbackHeading HeadingRenameFallbackMode = "heading"
	HeadingRenameFallbackBlockID HeadingRenameFallbackMode = "block-id"
)

type RenameHeadingParams struct {
	Context          context.Context
	NoteMetadata     notemeta.Indexer
	Path             string
	OldHeading       string
	NewHeading       string
	Apply            bool
	UpgradeToBlockID HeadingRenameUpgradeMode
	Fallback         HeadingRenameFallbackMode
}

type RenameHeadingResult struct {
	Path              string                    `json:"path"`
	OldHeading        string                    `json:"oldHeading"`
	NewHeading        string                    `json:"newHeading"`
	Applied           bool                      `json:"applied"`
	BlockID           string                    `json:"blockId,omitempty"`
	MatchedReferences int                       `json:"matchedReferences"`
	Rewritten         int                       `json:"rewritten"`
	UpgradedToBlockID int                       `json:"upgradedToBlockId"`
	Skipped           []HeadingRenameDiagnostic `json:"skipped,omitempty"`
	Rewrites          []HeadingRenameRewrite    `json:"rewrites,omitempty"`
	Message           string                    `json:"message,omitempty"`
}

type HeadingRenameRewrite struct {
	SourceNote string `json:"sourceNote"`
	Line       int    `json:"line,omitempty"`
	OldTarget  string `json:"oldTarget"`
	NewTarget  string `json:"newTarget"`
	Kind       string `json:"kind"`
}

type HeadingRenameDiagnostic struct {
	Code       string `json:"code"`
	SourceNote string `json:"sourceNote,omitempty"`
	Line       int    `json:"line,omitempty"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
}

func RenameHeading(vault obsidian.VaultManager, params RenameHeadingParams) (RenameHeadingResult, error) {
	var result RenameHeadingResult
	if strings.TrimSpace(params.Path) == "" || strings.TrimSpace(params.OldHeading) == "" || strings.TrimSpace(params.NewHeading) == "" {
		return result, errors.New("path, old heading, and new heading are required")
	}
	if params.UpgradeToBlockID == "" {
		params.UpgradeToBlockID = HeadingRenameUpgradeAuto
	}
	if params.Fallback == "" {
		params.Fallback = HeadingRenameFallbackBlockID
	}
	if params.UpgradeToBlockID != HeadingRenameUpgradeAuto && params.UpgradeToBlockID != HeadingRenameUpgradeAlways && params.UpgradeToBlockID != HeadingRenameUpgradeNever {
		return result, fmt.Errorf("unsupported --upgrade-to-block-id value %q", params.UpgradeToBlockID)
	}
	if params.Fallback != HeadingRenameFallbackHeading && params.Fallback != HeadingRenameFallbackBlockID {
		return result, fmt.Errorf("unsupported --fallback value %q", params.Fallback)
	}
	vaultPath, err := vault.Path()
	if err != nil {
		return result, err
	}
	ctx := params.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if params.Apply {
		// Recovery acquires the vault lease itself and must precede both this
		// action's lease and the source snapshots used for planning.
		if err := ontology.RecoverInterruptedEditsContext(ctx, vaultPath); err != nil {
			return result, fmt.Errorf("recover heading rename edits: %w", err)
		}
		release, err := acquireWriteLock(ctx, vaultPath)
		if err != nil {
			return result, err
		}
		defer release()
	}
	vaultDef, err := vault.Definition()
	if err != nil {
		return result, err
	}
	notePath := string(paths.Normalize(obsidian.NormalizeWithDefaultExt(params.Path, ".md")))
	if _, err := noteMutationFormat(params.NoteMetadata, notePath, noteformat.CapabilityStructuralContentMutation); err != nil {
		return result, err
	}
	allNotes, err := (&obsidian.Note{}).GetNotesList(vaultDef)
	if err != nil {
		return result, err
	}
	notePath, err = headingRenameNotePath(vaultPath, notePath, allNotes)
	if err != nil {
		return result, err
	}
	result.Path = notePath
	result.OldHeading = params.OldHeading
	result.NewHeading = params.NewHeading
	absPath := filepath.Join(vaultPath, notePath)
	contentBytes, err := os.ReadFile(absPath)
	if err != nil {
		return result, err
	}
	content := string(contentBytes)
	originals := map[string]string{notePath: content}
	matches := matchingHeadingLines(content, params.OldHeading)
	if len(matches) == 0 {
		return result, fmt.Errorf("heading not found: %q", params.OldHeading)
	}
	if len(matches) > 1 {
		return result, fmt.Errorf("ambiguous heading %q: %d matches", params.OldHeading, len(matches))
	}
	match := matches[0]
	blockID := match.blockID
	shouldUseBlock := params.UpgradeToBlockID == HeadingRenameUpgradeAlways || (params.UpgradeToBlockID == HeadingRenameUpgradeAuto && blockID != "")
	if blockID == "" && params.UpgradeToBlockID == HeadingRenameUpgradeAlways {
		blockID = ontology.BlockSafeIdentifier(params.NewHeading)
	}
	if blockID == "" && params.UpgradeToBlockID == HeadingRenameUpgradeAuto && params.Fallback == HeadingRenameFallbackBlockID {
		blockID = ontology.BlockSafeIdentifier(params.NewHeading)
		shouldUseBlock = blockID != ""
	}
	if params.UpgradeToBlockID == HeadingRenameUpgradeNever {
		shouldUseBlock = false
	}
	result.BlockID = blockID

	cache := obsidian.BuildNotePathCache(allNotes)
	for _, source := range allNotes {
		body, captured := originals[source]
		if !captured {
			bodyBytes, readErr := os.ReadFile(filepath.Join(vaultPath, source))
			if readErr != nil {
				if params.Apply {
					return result, fmt.Errorf("read heading rename source %s: %w", source, readErr)
				}
				continue
			}
			body = string(bodyBytes)
		}
		for _, link := range obsidian.ScanWikilinks(body, obsidian.DefaultWikilinkOptions) {
			rawTarget, fragment := splitLinkFragment(link.Target)
			if fragment == "" || strings.HasPrefix(fragment, "^") {
				continue
			}
			resolved, ok := source, true
			if rawTarget != "" {
				resolved, ok = cache.ResolveNote(rawTarget)
			}
			if !ok || resolved != notePath {
				continue
			}
			if obsidian.NormalizeWikilinkFragment(fragment) != obsidian.NormalizeWikilinkFragment(params.OldHeading) {
				if nearHeadingRenameFragment(fragment, params.OldHeading) {
					result.Skipped = append(result.Skipped, HeadingRenameDiagnostic{
						Code:       "heading_rename_skipped",
						SourceNote: source,
						Line:       link.Line,
						Reason:     "ambiguous_target",
						Message:    "link fragment is only a near-match for the renamed heading",
					})
				}
				continue
			}
			result.MatchedReferences++
			if link.InsideCodeBlock {
				result.Skipped = append(result.Skipped, HeadingRenameDiagnostic{
					Code:       "heading_rename_skipped",
					SourceNote: source,
					Line:       link.Line,
					Reason:     "code_block",
					Message:    "link is inside a fenced code block",
				})
				continue
			}
			newFragment := params.NewHeading
			kind := "heading_text_rewrite"
			if shouldUseBlock && blockID != "" {
				newFragment = "^" + strings.TrimPrefix(blockID, "^")
				kind = "block_id_upgrade"
				result.UpgradedToBlockID++
			}
			newTarget := rawTarget + "#" + newFragment
			result.Rewrites = append(result.Rewrites, HeadingRenameRewrite{
				SourceNote: source,
				Line:       link.Line,
				OldTarget:  link.Target,
				NewTarget:  newTarget,
				Kind:       kind,
			})
			result.Rewritten++
			originals[source] = body
		}
	}
	if result.MatchedReferences == 0 && obsidian.NormalizeWikilinkFragment(params.OldHeading) != obsidian.NormalizeWikilinkFragment(params.NewHeading) {
		result.Message = "no inbound heading references remain"
	}
	if !params.Apply {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	updated := map[string]string{notePath: replaceHeadingLine(content, match, params.NewHeading, shouldUseBlock || match.blockID != "", blockID)}
	for _, rewrite := range result.Rewrites {
		body, composed := updated[rewrite.SourceNote]
		if !composed {
			body = originals[rewrite.SourceNote]
		}
		updated[rewrite.SourceNote] = replaceExactWikilinkTarget(body, rewrite.OldTarget, rewrite.NewTarget)
	}
	session := ontology.NewEditSession(vaultDef, &obsidian.Note{}, nil)
	for path, finalContent := range updated {
		original := originals[path]
		if err := session.StageFileTransform(path, func(current string) (string, error) {
			if current != original {
				return "", fmt.Errorf("source changed since heading rename planning: %s", path)
			}
			return finalContent, nil
		}); err != nil {
			return result, err
		}
	}
	commit, err := session.CommitWithOptions(ctx, ontology.CommitOptions{VaultWriteLeaseHeld: true})
	if err != nil {
		return result, err
	}
	if commit.Outcome != ontology.CommitOutcomeCommitted && commit.Outcome != ontology.CommitOutcomeUnchanged {
		messages := make([]string, 0, len(commit.Conflicts))
		for _, conflict := range commit.Conflicts {
			messages = append(messages, conflict.Message)
		}
		return result, fmt.Errorf("heading rename %s: %s", commit.Outcome, strings.Join(messages, "; "))
	}
	result.Applied = commit.Outcome == ontology.CommitOutcomeCommitted
	if len(commit.Warnings) > 0 {
		if result.Message != "" {
			result.Message += "; "
		}
		result.Message += strings.Join(commit.Warnings, "; ")
	}
	return result, nil
}

// Use the inventory spelling for all reads and staged writes. EvalSymlinks
// resolves parent aliases but can retain requested casing on macOS, so a
// case-folded inventory match also needs proof that it names the same file.
func headingRenameNotePath(root, requested string, allNotes []string) (string, error) {
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(filepath.Join(vaultPaths.Root(), requested))
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("heading rename path %s uses an unsupported symbolic-link alias", requested)
	}
	abs, err := vaultPaths.Abs(paths.RelPath(requested))
	if err != nil {
		return "", err
	}
	rel, err := vaultPaths.RelStrict(abs.String())
	if err != nil {
		return "", err
	}
	for _, note := range allNotes {
		if note == rel.String() {
			return note, nil
		}
	}
	info, err = os.Stat(abs.String())
	if err != nil {
		return "", err
	}
	for _, note := range allNotes {
		if !strings.EqualFold(note, rel.String()) {
			continue
		}
		candidate, err := os.Stat(filepath.Join(root, note))
		if err != nil {
			return "", err
		}
		if os.SameFile(info, candidate) {
			return note, nil
		}
	}
	return rel.String(), nil
}

type headingLineMatch struct {
	start   int
	end     int
	level   int
	text    string
	blockID string
}

// Horizontal whitespace only: \s would cross line breaks and swallow the newline after the heading.
var headingLineWithHashesRE = regexp.MustCompile(`(?m)^(#{1,6})[ \t]+([^\r\n]+?)[ \t\r]*$`)
var trailingHeadingBlockIDRE = regexp.MustCompile(`\s+\^([A-Za-z0-9_-]+)\s*$`)

func matchingHeadingLines(content, oldHeading string) []headingLineMatch {
	var matches []headingLineMatch
	oldNorm := obsidian.NormalizeWikilinkFragment(oldHeading)
	for _, loc := range headingLineWithHashesRE.FindAllStringSubmatchIndex(content, -1) {
		if len(loc) < 6 {
			continue
		}
		if headingRenameInFencedCodeBlockAt(content, loc[0]) {
			continue
		}
		text := strings.TrimSpace(content[loc[4]:loc[5]])
		blockID := ""
		if m := trailingHeadingBlockIDRE.FindStringSubmatch(text); len(m) == 2 {
			blockID = m[1]
			text = strings.TrimSpace(trailingHeadingBlockIDRE.ReplaceAllString(text, ""))
		}
		if obsidian.NormalizeWikilinkFragment(text) != oldNorm {
			continue
		}
		matches = append(matches, headingLineMatch{
			start:   loc[0],
			end:     loc[5], // keep trailing spaces and CR so only the heading text changes
			level:   loc[3] - loc[2],
			text:    text,
			blockID: blockID,
		})
	}
	return matches
}

func replaceHeadingLine(content string, match headingLineMatch, newHeading string, useBlock bool, blockID string) string {
	line := strings.Repeat("#", match.level) + " " + strings.TrimSpace(newHeading)
	if useBlock && strings.TrimSpace(blockID) != "" {
		line += " ^" + strings.TrimPrefix(strings.TrimSpace(blockID), "^")
	}
	return content[:match.start] + line + content[match.end:]
}

func replaceExactWikilinkTarget(content, oldTarget, newTarget string) string {
	for _, link := range obsidian.ScanWikilinks(content, obsidian.DefaultWikilinkOptions) {
		if link.InsideCodeBlock || link.Target != oldTarget {
			continue
		}
		raw := content[link.Start:link.End]
		targetStart := strings.Index(raw, oldTarget)
		if targetStart < 0 {
			continue
		}
		targetEnd := targetStart + len(oldTarget)
		if targetEnd < len(raw) {
			next := raw[targetEnd]
			if next != '|' && next != ']' {
				continue
			}
		}
		absStart := link.Start + targetStart
		absEnd := link.Start + targetEnd
		return content[:absStart] + newTarget + content[absEnd:]
	}
	return content
}

func splitLinkFragment(target string) (string, string) {
	idx := strings.Index(target, "#")
	if idx < 0 {
		return target, ""
	}
	return target[:idx], target[idx+1:]
}

func headingRenameInFencedCodeBlockAt(content string, pos int) bool {
	if pos <= 0 {
		return false
	}
	lines := strings.SplitAfter(content[:pos], "\n")
	inFence := false
	fenceMarker := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 3 {
			continue
		}
		marker := ""
		if strings.HasPrefix(trimmed, "```") {
			marker = "```"
		} else if strings.HasPrefix(trimmed, "~~~") {
			marker = "~~~"
		}
		if marker == "" {
			continue
		}
		if !inFence {
			inFence = true
			fenceMarker = marker
			continue
		}
		if marker == fenceMarker {
			inFence = false
			fenceMarker = ""
		}
	}
	return inFence
}

func nearHeadingRenameFragment(fragment, oldHeading string) bool {
	left := obsidian.NormalizeWikilinkFragment(fragment)
	right := obsidian.NormalizeWikilinkFragment(oldHeading)
	if left == "" || right == "" || left == right {
		return false
	}
	return headingRenameDistanceAtMost(left, right, 2)
}

func headingRenameDistanceAtMost(a, b string, maxDistance int) bool {
	if absInt(len(a)-len(b)) > maxDistance {
		return false
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > maxDistance {
			return false
		}
		prev, curr = curr, prev
	}
	return prev[len(b)] <= maxDistance
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
