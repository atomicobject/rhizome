package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/tools/searchquality/internal/qualityeval"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// applyEmbeddingCache exports RHIZOME_EMBEDDING_CACHE before any provider is built,
// for this process and for the indexer subprocess that inherits the environment.
func applyEmbeddingCache(path string, fast bool, provider string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if fast || strings.TrimSpace(provider) == "" {
		return fmt.Errorf("-embedding-cache only applies to live-provider runs; it requires -fast=false and -embedding-provider")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return os.Setenv("RHIZOME_EMBEDDING_CACHE", absolute)
}

// applyRerank exports RHIZOME_RERANK_PROVIDER/RHIZOME_RERANK_MODEL before any
// search runs. Reranking is a network lane, so it requires -fast=false.
func applyRerank(provider, model string, fast bool) error {
	provider, model = strings.TrimSpace(provider), strings.TrimSpace(model)
	if provider == "" {
		if model != "" {
			return fmt.Errorf("-rerank-model requires -rerank-provider")
		}
		return nil
	}
	if fast {
		return fmt.Errorf("-rerank-provider only applies to live-provider runs; it requires -fast=false")
	}
	if err := os.Setenv("RHIZOME_RERANK_PROVIDER", provider); err != nil {
		return err
	}
	if model == "" {
		return nil
	}
	return os.Setenv("RHIZOME_RERANK_MODEL", model)
}

func evaluationEmbeddingOverride(provider, model, endpoint string, dimensions int, isolated, fast bool) (*embeddings.Config, error) {
	provider, model, endpoint = strings.TrimSpace(provider), strings.TrimSpace(model), strings.TrimSpace(endpoint)
	if provider == "" && model == "" && endpoint == "" && dimensions == 0 {
		return nil, nil
	}
	if !isolated || fast {
		return nil, fmt.Errorf("embedding overrides require -isolate and -fast=false")
	}
	if provider == "" {
		return nil, fmt.Errorf("-embedding-provider is required when overriding evaluation embeddings")
	}
	if dimensions < 0 {
		return nil, fmt.Errorf("-embedding-dimensions must be positive")
	}
	cfg := embeddings.Config{Enabled: true, Provider: strings.ToLower(provider), Model: model, Endpoint: endpoint, Dimensions: dimensions}
	cfg = embeddings.ApplyProviderDefaults(cfg)
	return &cfg, nil
}

func executeCorpus(ctx context.Context, corpus qualityeval.Corpus, selection qualityeval.Selection, vaultPath string, fast bool, profile unifiedsearch.Profile, corpusRoots map[string]corpusRoot, isolation isolationOptions) (qualityeval.Run, error) {
	formats, err := builtin.NewRuntime()
	if err != nil {
		return qualityeval.Run{}, err
	}

	base, err := filepath.Abs(vaultPath)
	if err != nil {
		return qualityeval.Run{}, err
	}
	revision, executionFingerprint, err := currentExecutionIdentity(base)
	if err != nil {
		return qualityeval.Run{}, err
	}
	isolatedInventories := map[string]semdb.IndexedPopulation{}
	isolatedIndexerFingerprint := ""
	var originalSnapshots []originalSourceSnapshot
	if isolation.Enabled {
		var cleanup func()
		corpusRoots, isolatedInventories, isolatedIndexerFingerprint, originalSnapshots, cleanup, err = prepareIsolatedCorpora(ctx, base, corpus, selection, corpusRoots, isolation, fast)
		if err != nil {
			return qualityeval.Run{}, err
		}
		defer cleanup()
	}
	fingerprint, err := qualityeval.Fingerprint(corpus)
	if err != nil {
		return qualityeval.Run{}, err
	}
	run := qualityeval.Run{Revision: revision, CorpusSourceRevision: corpus.SourceRevision, ExecutionFingerprint: executionFingerprint, CorpusFingerprint: fingerprint, Mode: "live-provider", Profile: string(profile), AnswerSelector: "current", Vault: base, StartedAt: time.Now().UTC().Format(time.RFC3339), Corpora: map[string]qualityeval.CorpusRunProvenance{}}
	if fast {
		run.Mode = "deterministic-lexical"
	}
	type loadedVault struct {
		root              string
		prefix            string
		def               obsidian.VaultDefinition
		emb               embeddings.Config
		sourceFingerprint string
	}
	loaded := map[string]loadedVault{}
	for _, c := range corpus.Cases {
		if !caseSelected(c, selection) {
			continue
		}
		root, prefix, resolveErr := corpusVault(base, c.Corpus, corpusRoots)
		if resolveErr != nil {
			return qualityeval.Run{}, resolveErr
		}
		lv, ok := loaded[root]
		if !ok {
			def, loadErr := repoconfig.LoadDefinitionFromPath(root)
			if loadErr != nil {
				return qualityeval.Run{}, fmt.Errorf("load vault %s: %w", root, loadErr)
			}
			resolvedConfig1, configErr1 := repoconfig.Load(root)
			embCfg, loadErr := resolvedConfig1.NoteEmbeddings, configErr1
			if loadErr != nil {
				return qualityeval.Run{}, fmt.Errorf("load embeddings %s: %w", root, loadErr)
			}
			embCfg.IndexPath = repoconfig.UnifiedIndexPath(root, embCfg.IndexPath)
			sourceFingerprint, fingerprintErr := sourceFingerprintForCorpus(base, root, executionFingerprint)
			if fingerprintErr != nil {
				return qualityeval.Run{}, fmt.Errorf("fingerprint corpus %s: %w", c.Corpus, fingerprintErr)
			}
			lv = loadedVault{root: root, prefix: prefix, def: def, emb: embCfg, sourceFingerprint: sourceFingerprint}
			loaded[root] = lv
			if !fast && run.EmbeddingFingerprint == "" {
				run.EmbeddingFingerprint = strings.Join([]string{embCfg.Provider, embCfg.Model, fmt.Sprintf("%d", embCfg.Dimensions)}, ":")
			}
		}
		if _, exists := run.Corpora[c.Corpus]; !exists {
			embeddingFingerprint := ""
			if !fast {
				embeddingFingerprint = strings.Join([]string{lv.emb.Provider, lv.emb.Model, fmt.Sprintf("%d", lv.emb.Dimensions)}, ":")
			}
			provenance := qualityeval.CorpusRunProvenance{PhysicalVault: lv.root, SourcePrefix: lv.prefix, SourceFingerprint: lv.sourceFingerprint, EmbeddingProvider: lv.emb.Provider, EmbeddingModel: lv.emb.Model, EmbeddingDimensions: lv.emb.Dimensions, EmbeddingFingerprint: embeddingFingerprint}
			if inventory, ok := isolatedInventories[lv.root]; ok {
				provenance.IndexGeneration = inventory.IndexGeneration
				provenance.InventoryFingerprint = inventory.Fingerprint
				provenance.IndexedFiles, provenance.IndexedAnchors = inventory.Files, inventory.Anchors
				provenance.IndexedDocSections, provenance.IndexedOntologyNodes = inventory.DocSections, inventory.OntologyNodes
				provenance.IndexedChunks, provenance.IndexedGraphEdges = inventory.Chunks, inventory.GraphEdges
				provenance.IndexedIntelEdges = inventory.IntelEdges
				provenance.UnresolvedGraphSources, provenance.UnresolvedGraphTargets = inventory.UnresolvedGraphSource, inventory.UnresolvedGraphTarget
				provenance.IndexerBinaryFingerprint = isolatedIndexerFingerprint
			}
			run.Corpora[c.Corpus] = provenance
		}
		queries := append([]string(nil), c.Queries...)
		primary := ""
		if len(queries) > 0 {
			primary, queries = queries[0], queries[1:]
		}
		appOpts := unifiedsearch.ApplicationOptions{Profile: profile, Diagnostics: true, Runtime: unifiedsearch.Options{
			NoteFormats: formats, Query: primary, Queries: queries, QueryInputs: corpusQueryInputs(c), Seeds: c.Seeds, IntentInput: c.Intent,
			SeedLimit: 5, UseVector: !fast,
			UseIntel: true, UseGraph: !fast, UseRefs: !fast, VaultPath: lv.root,
			VaultDef: lv.def, EmbCfg: lv.emb,
		}}
		if inventory, ok := isolatedInventories[lv.root]; ok {
			if missing := missingRequiredIdentities(c, lv.prefix, inventory); len(missing) > 0 {
				recordMissingRequiredIdentity(&run, c.Corpus, run.Corpora[c.Corpus], c.ID, missing)
				continue
			}
		}
		pages := executeEvaluationPages(ctx, profile, appOpts, unifiedsearch.Execute)
		result, runErr := pages[0].result, pages[0].err
		firstDuration := pages[0].durationMS
		qr := qualityeval.QueryResult{ID: c.ID, DurationMS: firstDuration, PageDurationsMS: []float64{firstDuration}}
		if runErr != nil {
			run.FailureCount++
			qr.Status = "error"
			qr.Warnings = []string{runErr.Error()}
			run.Results = append(run.Results, qr)
			continue
		}
		if provenance := run.Corpora[c.Corpus]; provenance.IndexGeneration == "" && result.IndexGeneration != "" {
			provenance.IndexGeneration = result.IndexGeneration
			run.Corpora[c.Corpus] = provenance
		}
		appendPageSources := func(page int, pageResult unifiedsearch.ApplicationResult) {
			targetIdentities := evaluationTargetIdentities(lv.prefix, pageResult.Target.Selected)
			targetIdentity := ""
			if len(targetIdentities) > 0 {
				targetIdentity = targetIdentities[0]
			}
			if page == 1 {
				for _, identity := range targetIdentities {
					qr.TargetIdentities = appendUnique(qr.TargetIdentities, identity)
				}
			}
			for _, assessed := range pageResult.Sources {
				ranked := assessed.Result
				source, identity := evaluationRankedSource(c, lv.prefix, ranked)
				if source == "" {
					source = ranked.Handle.String()
				}
				qr.Sources = appendUnique(qr.Sources, source)
				trace := qualityeval.CandidateTrace{Source: source, Identity: identity, Handle: ranked.Handle.String(), Page: page, Eligibility: string(assessed.Eligibility), Relationship: assessed.Relationship, TargetIdentity: targetIdentity, Score: ranked.FinalScore}
				for _, ev := range ranked.Evidence {
					trace.Evidence = append(trace.Evidence, ev.Type)
				}
				qr.Candidates = append(qr.Candidates, trace)
				if page != 1 {
					continue
				}
			}
		}
		appendPageSources(1, result)
		answerPacket := result.Answer
		applyAnswerPacketToQueryResult(c, lv.prefix, result.Sources, answerPacket, &qr)
		qr.AnswerInputSnapshot, err = captureAnswerInputSnapshot(result, lv.prefix)
		if err != nil {
			return qualityeval.Run{}, fmt.Errorf("capture answer inputs for %s: %w", c.ID, err)
		}
		qr.Status = string(result.Target.Status)
		for _, event := range result.Timings {
			qr.Stages = append(qr.Stages, qualityeval.StageTrace{Name: event.Name, Kind: event.Kind, DurationMS: float64(event.Duration.Microseconds()) / 1000, Status: event.Status})
		}
		for _, warning := range result.Warnings {
			qr.Warnings = append(qr.Warnings, warning.Code)
		}
		if len(pages) == 2 {
			qr.PageDurationsMS = append(qr.PageDurationsMS, pages[1].durationMS)
			if pages[1].err != nil {
				run.FailureCount++
				qr.ContinuationError = pages[1].err.Error()
			} else {
				appendPageSources(2, pages[1].result)
			}
		}
		run.Results = append(run.Results, qr)
	}
	finalRevision, finalExecutionFingerprint, err := currentExecutionIdentity(base)
	if err != nil {
		return qualityeval.Run{}, err
	}
	if finalRevision != revision || finalExecutionFingerprint != executionFingerprint {
		return qualityeval.Run{}, fmt.Errorf("execution sources changed during measurement (revision %s -> %s, fingerprint %s -> %s)", revision, finalRevision, executionFingerprint, finalExecutionFingerprint)
	}
	if isolation.Enabled {
		finalBinaryFingerprint, fingerprintErr := fileFingerprint(isolation.IndexerBinary)
		if fingerprintErr != nil {
			return qualityeval.Run{}, fingerprintErr
		}
		if finalBinaryFingerprint != isolatedIndexerFingerprint {
			return qualityeval.Run{}, fmt.Errorf("indexer binary changed during measurement")
		}
	}
	for _, lv := range loaded {
		finalFingerprint, fingerprintErr := sourceFingerprintForCorpus(base, lv.root, finalExecutionFingerprint)
		if fingerprintErr != nil {
			return qualityeval.Run{}, fingerprintErr
		}
		if finalFingerprint != lv.sourceFingerprint {
			return qualityeval.Run{}, fmt.Errorf("corpus source %s changed during measurement", lv.root)
		}
	}
	for _, snapshot := range originalSnapshots {
		finalFingerprint, fingerprintErr := fingerprintOriginalSource(base, snapshot.Root)
		if fingerprintErr != nil {
			return qualityeval.Run{}, fingerprintErr
		}
		if finalFingerprint != snapshot.Fingerprint {
			return qualityeval.Run{}, fmt.Errorf("original corpus source %s changed during measurement", snapshot.Root)
		}
	}
	return run, nil
}

func recordMissingRequiredIdentity(run *qualityeval.Run, corpusName string, provenance qualityeval.CorpusRunProvenance, caseID string, missing []string) {
	if run.Corpora == nil {
		run.Corpora = map[string]qualityeval.CorpusRunProvenance{}
	}
	if _, exists := run.Corpora[corpusName]; !exists {
		run.Corpora[corpusName] = provenance
	}
	run.FailureCount++
	run.Results = append(run.Results, qualityeval.QueryResult{ID: caseID, Status: "missing_required_identity", MissingRequiredIdentities: missing, Warnings: []string{"fresh index omits a required canonical source identity"}})
}

type evaluationPage struct {
	result     unifiedsearch.ApplicationResult
	durationMS float64
	err        error
}

func executeEvaluationPages(ctx context.Context, profile unifiedsearch.Profile, opts unifiedsearch.ApplicationOptions, execute func(context.Context, unifiedsearch.ApplicationOptions) (unifiedsearch.ApplicationResult, error)) []evaluationPage {
	run := func(options unifiedsearch.ApplicationOptions) evaluationPage {
		started := time.Now()
		result, err := execute(ctx, options)
		return evaluationPage{result: result, durationMS: float64(time.Since(started).Microseconds()) / 1000, err: err}
	}
	pages := []evaluationPage{run(opts)}
	if pages[0].err != nil || profile != unifiedsearch.ProfileInteractive || pages[0].result.Continuation == "" {
		return pages
	}
	opts.Continuation = pages[0].result.Continuation
	return append(pages, run(opts))
}

func corpusQueryInputs(c qualityeval.Case) []unifiedsearch.QueryInput {
	if len(c.Facets) == 0 {
		return nil
	}
	out := make([]unifiedsearch.QueryInput, 0, len(c.Facets))
	for _, facet := range c.Facets {
		out = append(out, unifiedsearch.QueryInput{Text: facet.Text, Mode: facet.Mode})
	}
	return out
}

func currentExecutionIdentity(root string) (string, string, error) {
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", "", fmt.Errorf("resolve execution revision: %w", err)
	}
	diff, err := exec.Command("git", "-C", root, "diff", "--binary", "HEAD").Output()
	if err != nil {
		return "", "", fmt.Errorf("fingerprint execution diff: %w", err)
	}
	untracked, err := exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return "", "", fmt.Errorf("list untracked execution sources: %w", err)
	}
	h := sha256.New()
	_, _ = h.Write([]byte(strings.TrimSpace(string(head))))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(diff)
	for _, name := range strings.Split(string(untracked), "\x00") {
		if name == "" || strings.HasPrefix(name, ".rhizome/") || strings.Contains(name, "/.rhizome/") || strings.HasSuffix(name, ".sqlite") {
			continue
		}
		_, _ = h.Write([]byte(name))
		content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if readErr != nil {
			return "", "", fmt.Errorf("read untracked execution source %s: %w", name, readErr)
		}
		_, _ = h.Write(content)
	}
	return strings.TrimSpace(string(head)), fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

func sourceFingerprintForCorpus(repoRoot, physicalRoot, executionFingerprint string) (string, error) {
	if filepath.Clean(repoRoot) == filepath.Clean(physicalRoot) {
		return executionFingerprint, nil
	}
	h := sha256.New()
	err := filepath.WalkDir(physicalRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(physicalRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".rhizome" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		_, _ = h.Write([]byte(filepath.ToSlash(rel)))
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = h.Write(content)
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}
