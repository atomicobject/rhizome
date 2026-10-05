package noteownership

import (
	"fmt"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// SelectorInput composes the immutable ownership rules for one vault.
type SelectorInput struct {
	VaultDefinition obsidian.VaultDefinition
	Registry        noteformat.Registry
	CodeRoots       []paths.AbsPath
	CodeLanguage    func(paths.CodePathRef) codeanchor.Lang
}

// Selection is the provider-aware exclusive decision for one canonical path.
// CodeCandidate reports language eligibility before note/code exclusivity.
type Selection struct {
	Path          paths.RelPath
	Owner         notediscovery.Owner
	Provider      noteformat.FormatID
	Language      codeanchor.Lang
	CodeCandidate bool
	Eligible      bool
}

// Selector is an immutable, concurrency-safe single-path view of the same
// plan used by Discover. It does not inspect the filesystem for file content.
type Selector struct {
	vaultPaths paths.VaultPaths
	plan       *notediscovery.Plan
	matcher    *ignore.Matcher
	codeRoots  map[string]struct{}
	language   func(paths.CodePathRef) codeanchor.Lang
	ignored    map[string]bool
}

func CompileSelector(input SelectorInput) (Selector, error) {
	vaultPaths, err := paths.NewVaultPaths(input.VaultDefinition.BasePath())
	if err != nil || vaultPaths.Root() == "" {
		return Selector{}, fmt.Errorf("ownership selector vault base path is required")
	}
	mode := notediscovery.ClassicVault
	if input.VaultDefinition.IsCollection() {
		mode = notediscovery.CollectionVault
	}
	matcher := obsidian.LoadVaultIgnoreMatcher(vaultPaths.Root(), input.VaultDefinition.Excludes)
	plan, err := notediscovery.Compile(notediscovery.Config{
		Mode: mode, Includes: input.VaultDefinition.Includes, Registry: input.Registry,
		Ignore: func(rel paths.RelPath) bool { return matcher.IsIgnored(rel.String(), false) },
	})
	if err != nil {
		return Selector{}, err
	}
	return Selector{vaultPaths: vaultPaths, plan: plan, matcher: matcher, codeRoots: codeRoots(input.CodeRoots), language: input.CodeLanguage, ignored: defaultIgnoredDirs()}, nil
}

func (s Selector) Select(raw paths.RelPath) (Selection, error) {
	path, err := paths.CleanRelPath(raw.String())
	if err != nil || path == "" || path != raw {
		return Selection{}, fmt.Errorf("ownership selector path must be canonical and vault-relative: %q", raw)
	}
	if !s.traversalEligible(path) {
		return Selection{Path: path, Owner: notediscovery.Unowned}, nil
	}
	initial, err := s.plan.Classify(path, false)
	if err != nil {
		return Selection{}, err
	}
	if initial.Owner == notediscovery.Ignored {
		return Selection{Path: initial.Path, Owner: initial.Owner, Eligible: true}, nil
	}
	absPath, err := s.vaultPaths.Abs(path)
	if err != nil || absPath == "" {
		return Selection{}, fmt.Errorf("resolve ownership selector path %q: %w", path, err)
	}
	codePath, err := s.vaultPaths.RelCodeStrict(path.String())
	if err != nil {
		return Selection{}, err
	}
	language := codeanchor.Lang("")
	if s.language != nil && inCodeRoots(paths.NormalizeAbsPathForCompare(absPath.String()), s.codeRoots) {
		language = codeanchor.Lang(strings.TrimSpace(string(s.language(paths.CodePathRef{Rel: codePath, Abs: absPath}))))
	}
	decision, err := s.plan.Classify(path, language != "")
	if err != nil {
		return Selection{}, err
	}
	codeCandidate := language != ""
	if decision.Owner != notediscovery.Code {
		language = ""
	}
	return Selection{Path: decision.Path, Owner: decision.Owner, Provider: decision.Provider, Language: language, CodeCandidate: codeCandidate, Eligible: true}, nil
}

func (s Selector) traversalEligible(path paths.RelPath) bool {
	parts := strings.Split(path.String(), "/")
	for index, part := range parts {
		if strings.HasPrefix(part, ".") {
			return false
		}
		if index < len(parts)-1 && s.ignored[part] {
			return false
		}
	}
	return true
}

func (s Selector) skipDirectory(abs string, name string) (bool, error) {
	if abs != s.vaultPaths.Root() && (strings.HasPrefix(name, ".") || s.ignored[name]) {
		return true, nil
	}
	rel, err := s.vaultPaths.RelStrict(filepath.Clean(abs))
	if err != nil {
		return false, err
	}
	return rel != "" && s.matcher.IsIgnoredShallow(rel.String(), true), nil
}

func defaultIgnoredDirs() map[string]bool {
	result := make(map[string]bool)
	for _, name := range ignore.DefaultIgnoreDirnames() {
		if name = strings.TrimSpace(name); name != "" {
			result[name] = true
		}
	}
	return result
}

func codeRoots(roots []paths.AbsPath) map[string]struct{} {
	result := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if path := paths.NormalizeAbsPathForCompare(root.String()); path != "" {
			result[path] = struct{}{}
		}
	}
	return result
}

func inCodeRoots(abs string, roots map[string]struct{}) bool {
	for root := range roots {
		if abs == root || strings.HasPrefix(abs, root+"/") {
			return true
		}
	}
	return false
}
