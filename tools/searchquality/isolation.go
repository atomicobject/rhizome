package main

import (
	"bytes"
	"context"

	"fmt"

	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/tools/searchquality/internal/qualityeval"
)

type isolationOptions struct {
	Enabled           bool
	IndexerBinary     string
	EmbeddingOverride *embeddings.Config
}

type originalSourceSnapshot struct {
	Root        string
	Fingerprint string
}

var repositoryIsolationExcludes = []string{
	".git", ".agents", ".claude", ".codex", "bin", "vendor", "node_modules", "web/node_modules", "pkg/app/web/assets/dist",
	"testdata",
	"docs/reference/analysis/assets/search-engine-quality-2026-09-12",
	"docs/reference/analysis/search-application-integration-review.md",
	"docs/reference/analysis/search-engine-evaluation-2026-09-12.md",
	"docs/reference/analysis/search-engine-quality-baseline-2026-09-12.md",
	"docs/reference/analysis/search-engine-quality-development-comparison-2026-09-12.md",
	"docs/reference/analysis/search-engine-quality-development-v2-2026-09-12.md",
	"docs/reference/analysis/search-engine-quality-manifest-2026-09-12.md",
	"docs/reference/analysis/search-quality-corpus-review.md",
	"docs/reference/analysis/search-quality-curated-development-review.md",
	"docs/specs/product/search-engine-quality.md",
	"docs/efforts/2026-09-12-15-26-search-engine-excellence.md",
}

var repositoryAnswerArtifactPrefixes = []string{
	"docs/reference/analysis/search-application-",
	"docs/reference/analysis/search-engine-evaluation-",
	"docs/reference/analysis/search-engine-quality-",
	"docs/reference/analysis/search-quality-",
}

func prepareIsolatedCorpora(ctx context.Context, base string, corpus qualityeval.Corpus, selection qualityeval.Selection, overrides map[string]corpusRoot, opts isolationOptions, deterministic bool) (map[string]corpusRoot, map[string]semdb.IndexedPopulation, string, []originalSourceSnapshot, func(), error) {
	if strings.TrimSpace(opts.IndexerBinary) == "" {
		return nil, nil, "", nil, nil, fmt.Errorf("-isolate requires -indexer-binary")
	}
	binary, err := filepath.Abs(opts.IndexerBinary)
	if err != nil {
		return nil, nil, "", nil, nil, err
	}
	if info, err := os.Stat(binary); err != nil || info.IsDir() {
		return nil, nil, "", nil, nil, fmt.Errorf("invalid indexer binary %q", binary)
	}
	binaryFingerprint, err := fileFingerprint(binary)
	if err != nil {
		return nil, nil, "", nil, nil, err
	}
	top, err := os.MkdirTemp("", "rzm-searchquality-isolated-")
	if err != nil {
		return nil, nil, "", nil, nil, err
	}
	cleanup := func() { _ = exec.Command("trash", top).Run() }
	result := map[string]corpusRoot{}
	inventories := map[string]semdb.IndexedPopulation{}
	var snapshots []originalSourceSnapshot
	names := map[string]struct{}{}
	for _, c := range corpus.Cases {
		if caseSelected(c, selection) {
			names[c.Corpus] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		if err := validateCorpusDirectoryName(name); err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		mapping, ok := overrides[name]
		if !ok {
			mapping, ok = defaultCorpusRoots[name]
		}
		if !ok {
			cleanup()
			return nil, nil, "", nil, nil, fmt.Errorf("no physical root mapping for corpus %q", name)
		}
		source := mapping.Root
		if !filepath.IsAbs(source) {
			source = filepath.Join(base, source)
		}
		originalFingerprint, err := fingerprintOriginalSource(base, source)
		if err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		destination := filepath.Join(top, name)
		excludes := mapping.Excludes
		if len(excludes) == 0 && name == "rhizome-repository" {
			excludes = repositoryIsolationExcludes
		}
		allowed, err := copyIsolatedPopulation(source, destination, excludes)
		if err != nil {
			cleanup()
			return nil, nil, "", nil, nil, fmt.Errorf("stage corpus %s: %w", name, err)
		}
		isolated := corpusRoot{Root: destination, SourcePrefix: mapping.SourcePrefix}
		if err := validateJudgedSourcesPresent(corpus, selection, name, isolated); err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		if err := configureIsolatedEmbeddings(destination, deterministic, opts.EmbeddingOverride); err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		command := exec.CommandContext(ctx, binary, "index", "--rebuild")
		command.Dir = destination
		// os.Environ() carries RHIZOME_EMBEDDING_CACHE when main exported it.
		command.Env = append(os.Environ(), "RZM_SKIP_REPO_DELEGATE=1")
		var output bytes.Buffer
		command.Stdout = &output
		command.Stderr = &output
		if err := command.Run(); err != nil {
			cleanup()
			return nil, nil, "", nil, nil, fmt.Errorf("fresh index %s: %w\n%s", name, err, output.String())
		}
		resolvedConfig1, configErr1 := repoconfig.Load(destination)
		codeConfig, err := resolvedConfig1.Code, configErr1
		if err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		store, err := semdb.OpenReadOnlyExisting(repoconfig.UnifiedIndexPath(destination, codeConfig.IndexPath), ctx, sqliteutil.Options{})
		if err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		inventory, inventoryErr := store.IndexedPopulation(ctx)
		if inventoryErr == nil {
			inventory.IndexGeneration, inventoryErr = unifiedsearch.IndexGeneration(ctx, store, nil, nil)
		}
		_ = store.Close()
		if inventoryErr != nil {
			cleanup()
			return nil, nil, "", nil, nil, inventoryErr
		}
		if err := validateInventoryResolution(name, inventory); err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		if err := validateInventoryPaths(name, inventory.Paths, excludes, allowed); err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		if err := validateUnresolvedGraphTargets(name, inventory.UnresolvedGraphTargets, excludes, allowed); err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		if err := validateJudgedPathsIndexed(corpus, selection, name, isolated, inventory.Paths); err != nil {
			cleanup()
			return nil, nil, "", nil, nil, err
		}
		afterFingerprint, err := fingerprintOriginalSource(base, source)
		if err != nil || afterFingerprint != originalFingerprint {
			cleanup()
			return nil, nil, "", nil, nil, fmt.Errorf("corpus source %s changed while staging", source)
		}
		snapshots = append(snapshots, originalSourceSnapshot{Root: source, Fingerprint: originalFingerprint})
		result[name] = isolated
		inventories[destination] = inventory
	}
	return result, inventories, binaryFingerprint, snapshots, cleanup, nil
}

func validateInventoryResolution(name string, inventory semdb.IndexedPopulation) error {
	switch {
	case inventory.UnresolvedChunkOwner != 0:
		return fmt.Errorf("corpus %s has %d unresolved chunk owners", name, inventory.UnresolvedChunkOwner)
	case inventory.UnresolvedIntelOwner != 0:
		return fmt.Errorf("corpus %s has %d unresolved internal intel edge owners", name, inventory.UnresolvedIntelOwner)
	case inventory.UnresolvedGraphSource != 0:
		return fmt.Errorf("corpus %s has %d unresolved graph sources", name, inventory.UnresolvedGraphSource)
	default:
		return nil
	}
}

func validateUnresolvedGraphTargets(name string, targets, excludes []string, allowed map[string]struct{}) error {
	for _, path := range targets {
		clean := filepath.ToSlash(filepath.Clean(path))
		if filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("corpus %s has escaping unresolved graph target %q", name, path)
		}
		if excludedPopulationPath(clean, excludes) {
			return fmt.Errorf("corpus %s graph references forbidden unresolved target %q", name, path)
		}
		if _, exists := allowed[clean]; exists {
			return fmt.Errorf("corpus %s graph target exists in staged population but is absent from index %q", name, path)
		}
	}
	return nil
}

func validateJudgedPathsIndexed(corpus qualityeval.Corpus, selection qualityeval.Selection, name string, mapping corpusRoot, indexedPaths []string) error {
	indexed := make(map[string]struct{}, len(indexedPaths))
	for _, path := range indexedPaths {
		indexed[filepath.ToSlash(filepath.Clean(path))] = struct{}{}
	}
	for _, c := range corpus.Cases {
		if c.Corpus != name || !caseSelected(c, selection) {
			continue
		}
		sources := map[string]struct{}{}
		for source := range c.Judgments {
			sources[source] = struct{}{}
		}
		for _, source := range c.RequiredSources {
			sources[source] = struct{}{}
		}
		for source := range sources {
			path := judgmentPhysicalSource(source)
			path = strings.TrimPrefix(path, strings.Trim(mapping.SourcePrefix, "/")+"/")
			path = filepath.ToSlash(filepath.Clean(path))
			if path == "." || path == "" {
				continue
			}
			if _, ok := indexed[path]; !ok {
				return fmt.Errorf("isolated corpus %s judged source for %s is absent from fresh index (possibly excluded or ignored): %s", name, c.ID, path)
			}
		}
	}
	return nil
}

func fingerprintOriginalSource(base, source string) (string, error) {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	if filepath.Clean(baseAbs) == filepath.Clean(sourceAbs) {
		revision, fingerprint, err := currentExecutionIdentity(baseAbs)
		if err != nil {
			return "", err
		}
		return revision + "\x00" + fingerprint, nil
	}
	return sourceFingerprintForCorpus(baseAbs, sourceAbs, "")
}

func missingRequiredIdentities(c qualityeval.Case, prefix string, inventory semdb.IndexedPopulation) []string {
	available := make(map[string]struct{}, len(inventory.Identities))
	for _, identity := range inventory.Identities {
		available[identity] = struct{}{}
	}
	var missing []string
	for _, identity := range c.RequiredSources {
		local := localCanonicalIdentity(prefix, identity)
		if !strings.HasPrefix(local, "fqn\x00") && !strings.HasPrefix(local, "node\x00") {
			continue
		}
		if _, ok := available[local]; !ok {
			missing = append(missing, identity)
		}
	}
	sort.Strings(missing)
	return missing
}

func localCanonicalIdentity(prefix, identity string) string {
	parts := strings.Split(identity, "\x00")
	pathIndex := -1
	switch parts[0] {
	case "fqn":
		pathIndex = 2
	case "node":
		pathIndex = 1
	}
	if pathIndex >= 0 && len(parts) > pathIndex {
		parts[pathIndex] = strings.TrimPrefix(parts[pathIndex], strings.Trim(prefix, "/")+"/")
	}
	return strings.Join(parts, "\x00")
}

func disableIsolatedEmbeddings(root string) error {
	resolvedConfig2, configErr2 := repoconfig.Load(root)
	cfg, err := resolvedConfig2.NoteEmbeddings, configErr2
	if err != nil {
		return err
	}
	cfg.Enabled = false
	if err := repoconfig.SaveEmbeddingsConfig(root, cfg); err != nil {
		return err
	}
	resolvedConfig3, configErr3 := repoconfig.Load(root)
	code, _, err := resolvedConfig3.CodeEmbeddings, resolvedConfig3.CodeEmbeddingsExplicit, configErr3
	if err != nil {
		return err
	}
	code.Enabled = false
	return repoconfig.SaveCodeEmbeddingsConfig(root, code)
}

func configureIsolatedEmbeddings(root string, deterministic bool, override *embeddings.Config) error {
	if deterministic {
		return disableIsolatedEmbeddings(root)
	}
	if override == nil {
		return nil
	}
	resolvedConfig4, configErr4 := repoconfig.Load(root)
	note, err := resolvedConfig4.NoteEmbeddings, configErr4
	if err != nil {
		return err
	}
	note.Enabled = true
	note.Provider = override.Provider
	note.Model = override.Model
	note.Endpoint = override.Endpoint
	note.Dimensions = override.Dimensions
	if err := repoconfig.SaveEmbeddingsConfig(root, note); err != nil {
		return err
	}
	resolvedConfig5, configErr5 := repoconfig.Load(root)
	code, _, err := resolvedConfig5.CodeEmbeddings, resolvedConfig5.CodeEmbeddingsExplicit, configErr5
	if err != nil {
		return err
	}
	code.Enabled = true
	code.Provider = override.Provider
	code.Model = override.Model
	code.Endpoint = override.Endpoint
	code.Dimensions = override.Dimensions
	return repoconfig.SaveCodeEmbeddingsConfig(root, code)
}

func validateJudgedSourcesPresent(corpus qualityeval.Corpus, selection qualityeval.Selection, name string, mapping corpusRoot) error {
	for _, c := range corpus.Cases {
		if c.Corpus != name || !caseSelected(c, selection) {
			continue
		}
		sources := map[string]struct{}{}
		for source := range c.Judgments {
			sources[source] = struct{}{}
		}
		for _, source := range c.RequiredSources {
			sources[source] = struct{}{}
		}
		for source := range sources {
			path := judgmentPhysicalSource(source)
			path = strings.TrimPrefix(path, strings.Trim(mapping.SourcePrefix, "/")+"/")
			if path == "" {
				continue
			}
			if info, err := os.Stat(filepath.Join(mapping.Root, filepath.FromSlash(path))); err != nil || info.IsDir() {
				return fmt.Errorf("isolated corpus %s omits judged source for %s: %s", name, c.ID, path)
			}
		}
	}
	return nil
}

func validateInventoryPaths(name string, paths, excludes []string, allowed map[string]struct{}) error {
	for _, path := range paths {
		clean := filepath.ToSlash(filepath.Clean(path))
		if filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("corpus %s indexed escaping path %q", name, path)
		}
		if excludedPopulationPath(clean, excludes) {
			return fmt.Errorf("corpus %s indexed forbidden path %q", name, path)
		}
		if _, ok := allowed[clean]; !ok {
			return fmt.Errorf("corpus %s indexed path outside captured staged population %q", name, path)
		}
	}
	return nil
}

func judgmentPhysicalSource(source string) string {
	parts := strings.Split(source, "\x00")
	switch parts[0] {
	case "path":
		if len(parts) >= 2 {
			return parts[1]
		}
	case "fqn":
		if len(parts) >= 3 {
			return parts[2]
		}
	case "node":
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return source
}
