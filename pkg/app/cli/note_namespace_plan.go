package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// The engine invokes this read-only planner after recovery under its lease.
// One source snapshot supplies both backlink content and publication witnesses.
type noteNamespacePlanner struct {
	root            string
	definition      obsidian.VaultDefinition
	metadata        notemeta.Indexer
	moves           []MoveRequest
	overwrite       bool
	updateBacklinks bool
	codeMappings    []coderefs.RefMapping
	summary         noteNamespaceSummary
}

type noteNamespaceSummary struct {
	Results          []MoveResult
	TotalLinkUpdates int
	Skipped          []string
	HeadingPointers  []HeadingPointer
}

type noteNamespaceFile struct {
	content []byte
	mode    uint32
}

type noteNamespaceHost struct {
	source, final string
	file          *noteNamespaceFile
}

func (p *noteNamespacePlanner) PlanNamespaceMutation(ctx context.Context, lease *validate.IndexLockLease) (validate.NamespaceMutationPlan, error) {
	if err := lease.RequireHeldForVault(p.root); err != nil {
		return validate.NamespaceMutationPlan{}, err
	}
	plans, err := planNoteMoves(p.root, p.metadata, p.moves, p.overwrite)
	if err != nil {
		return validate.NamespaceMutationPlan{}, err
	}
	vaultPaths, err := paths.NewVaultPaths(p.root)
	if err != nil {
		return validate.NamespaceMutationPlan{}, err
	}
	files := make(map[string]*noteNamespaceFile)
	capture := func(rel string) (*noteNamespaceFile, error) {
		if file, ok := files[rel]; ok {
			return file, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Endpoints already have canonical parents; walked hosts retain their
		// own leaf names so an alias cannot become another host's witness.
		abs := filepath.Join(vaultPaths.Root(), filepath.FromSlash(rel))
		info, err := os.Lstat(abs)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("namespace source is not a regular file: %s", rel)
		}
		content, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("read namespace source %s: %w", rel, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file := &noteNamespaceFile{content: content, mode: uint32(info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky))}
		files[rel] = file
		return file, nil
	}
	var operations []validate.RepairOperation
	var summary noteNamespaceSummary
	var mappings []linkMapping
	p.codeMappings = nil
	for _, plan := range plans {
		file, err := capture(plan.source.rel)
		if err != nil {
			return validate.NamespaceMutationPlan{}, err
		}
		destination := &validate.RepairDestinationState{Kind: validate.RepairDestinationAbsent}
		if plan.caseOnly {
			destination.Kind = validate.RepairDestinationCaseOnly
		} else if plan.target.info != nil {
			target, err := capture(plan.target.rel)
			if err != nil {
				return validate.NamespaceMutationPlan{}, err
			}
			destination = &validate.RepairDestinationState{
				Kind: validate.RepairDestinationOccupied, Content: target.content,
				Hash: validate.SourceHash(target.content), Mode: target.mode,
			}
		}
		operations = append(operations, validate.RepairOperation{
			Kind: validate.RepairOperationRename, Path: plan.source.rel, DestinationPath: plan.target.rel,
			SourceHash: validate.SourceHash(file.content), SourceMode: &file.mode, DestinationState: destination,
		})
		summary.Results = append(summary.Results, MoveResult{Source: plan.source.rel, Target: plan.target.rel})
		mappings = append(mappings, linkMapping{Old: plan.source.rel, New: plan.target.rel, OldAliases: plan.sourceAliases()})
		p.codeMappings = append(p.codeMappings, coderefs.RefMapping{OldPath: plan.source.rel, NewPath: plan.target.rel, OldPathAliases: plan.sourceAliases()})
	}
	if p.updateBacklinks {
		matcher := ignore.LoadUnifiedMatcher(p.root, p.definition.Excludes)
		hosts, err := noteNamespaceHosts(ctx, vaultPaths, plans, matcher)
		if err != nil {
			return validate.NamespaceMutationPlan{}, err
		}
		setNamespaceBasenameUniqueness(mappings, hosts, matcher)
		for i := range hosts {
			host := &hosts[i]
			reachable, ignored := namespaceHostVisibility(host.final, matcher)
			if !reachable {
				continue
			}
			if ignored {
				summary.Skipped = append(summary.Skipped, host.final)
				continue
			}
			if filepath.Ext(host.final) != ".md" {
				continue
			}
			file, err := capture(host.source)
			if err != nil {
				return validate.NamespaceMutationPlan{}, err
			}
			content := string(file.content)
			for j, mapping := range mappings {
				rewritten, count := obsidian.RewriteLinksInContentWithOptions(content, mapping.Old, mapping.New, mapping.BasenameUnique, mapping.OldAliases...)
				content = rewritten
				summary.Results[j].LinkUpdates += count
				summary.TotalLinkUpdates += count
			}
			host.file = &noteNamespaceFile{content: []byte(content), mode: file.mode}
			if content != string(file.content) {
				operations = append(operations, validate.RepairOperation{
					Kind: validate.RepairOperationWrite, Path: host.source, Content: host.file.content,
					SourceHash: validate.SourceHash(file.content), SourceMode: &file.mode,
				})
			}
		}
		for i := range summary.Results {
			pointers := namespaceHeadingPointers(hosts, summary.Results[i].Target)
			summary.Results[i].HeadingPointers = pointers
			summary.HeadingPointers = append(summary.HeadingPointers, pointers...)
		}
	}
	// Full display diagnostics belong to this invocation. Recovery needs only
	// fixed counts, so heading pointers and paths cannot exhaust its summary cap.
	body, err := json.Marshal(struct {
		Moves       int `json:"moves"`
		LinkUpdates int `json:"linkUpdates"`
	}{len(summary.Results), summary.TotalLinkUpdates})
	if err != nil {
		return validate.NamespaceMutationPlan{}, err
	}
	p.summary = summary
	return validate.NamespaceMutationPlan{Operations: operations, Summary: body}, nil
}

func noteNamespaceHosts(ctx context.Context, vaultPaths paths.VaultPaths, plans []noteMovePlan, matcher *ignore.Matcher) ([]noteNamespaceHost, error) {
	moved := make(map[string]string)
	overwritten := make(map[string]bool)
	for _, plan := range plans {
		moved[obsidian.NormalizeForComparison(plan.source.rel)] = plan.target.rel
		if plan.target.info != nil && !plan.caseOnly {
			overwritten[obsidian.NormalizeForComparison(plan.target.rel)] = true
		}
	}
	var hosts []noteNamespaceHost
	seen := make(map[string]bool)
	add := func(rel string) {
		key := obsidian.NormalizeForComparison(rel)
		final, isMoved := moved[key]
		if !isMoved {
			if overwritten[key] {
				return
			}
			final = rel
		}
		seen[key] = true
		hosts = append(hosts, noteNamespaceHost{source: rel, final: final})
	}
	// Relative walk entries preserve leaf aliases while the vault helper owns
	// normalization and confinement of their inventory keys.
	err := fs.WalkDir(os.DirFS(vaultPaths.Root()), ".", func(walkRel string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		rel, err := vaultPaths.RelStrict(walkRel)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if matcher.IsIgnoredShallow(rel.String(), true) {
				return fs.SkipDir
			}
			return nil
		}
		add(rel.String())
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A source pruned from the old walk can become eligible at its final path.
	for _, plan := range plans {
		if !seen[obsidian.NormalizeForComparison(plan.source.rel)] {
			add(plan.source.rel)
		}
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].final < hosts[j].final })
	return hosts, nil
}

func namespaceHostVisibility(rel string, matcher *ignore.Matcher) (reachable, ignored bool) {
	for dir := filepath.ToSlash(filepath.Dir(rel)); dir != "." && dir != ""; dir = filepath.ToSlash(filepath.Dir(dir)) {
		if matcher.IsIgnoredShallow(dir, true) {
			return false, false
		}
	}
	return true, matcher.IsIgnoredShallow(rel, false)
}

func setNamespaceBasenameUniqueness(mappings []linkMapping, hosts []noteNamespaceHost, matcher *ignore.Matcher) {
	counts := make(map[string]int)
	for _, host := range hosts {
		reachable, ignored := namespaceHostVisibility(host.final, matcher)
		if reachable && !ignored && filepath.Ext(host.final) == ".md" {
			counts[obsidian.NormalizeForComparison(strings.TrimSuffix(filepath.Base(host.final), ".md"))]++
		}
	}
	for _, mapping := range mappings {
		oldBase := strings.TrimSuffix(filepath.Base(mapping.Old), filepath.Ext(mapping.Old))
		newBase := strings.TrimSuffix(filepath.Base(mapping.New), filepath.Ext(mapping.New))
		if strings.ToLower(filepath.Ext(mapping.Old)) == ".md" && obsidian.NormalizeForComparison(oldBase) != obsidian.NormalizeForComparison(newBase) {
			counts[obsidian.NormalizeForComparison(oldBase)]++
		}
	}
	for i := range mappings {
		oldBase := strings.TrimSuffix(filepath.Base(mappings[i].Old), filepath.Ext(mappings[i].Old))
		mappings[i].BasenameUnique = strings.ToLower(filepath.Ext(mappings[i].Old)) != ".md" || counts[obsidian.NormalizeForComparison(oldBase)] <= 1
	}
}

func namespaceHeadingPointers(hosts []noteNamespaceHost, target string) []HeadingPointer {
	target = string(paths.Normalize(obsidian.NormalizeWithDefaultExt(target, ".md")))
	base := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	var pointers []HeadingPointer
	for _, host := range hosts {
		if host.file == nil {
			continue
		}
		for _, link := range obsidian.ScanWikilinks(string(host.file.content), obsidian.DefaultWikilinkOptions) {
			raw, fragment := splitHeadingPointerFragment(link.Target)
			if fragment == "" || strings.HasPrefix(fragment, "^") {
				continue
			}
			normalized := string(paths.Normalize(obsidian.NormalizeWithDefaultExt(raw, ".md")))
			if normalized == target || raw == base {
				pointers = append(pointers, HeadingPointer{
					SourceNote: host.final, TargetNote: target, Fragment: fragment,
					Command: fmt.Sprintf("rzm note rename-heading %q %q <new-heading>", target, fragment),
				})
			}
		}
	}
	return pointers
}
