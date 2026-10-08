package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/qualityeval"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func main() {
	corpusPath := flag.String("corpus", "", "frozen corpus JSON")
	runPath := flag.String("run", "", "search result artifact JSON")
	baselinePath := flag.String("baseline-run", "", "optional baseline run artifact to compare")
	rescoreFromCorpusPath := flag.String("rescore-from-corpus", "", "original corpus used by a preserved run; permits judgment-only rescoring with provenance")
	replayAnswers := flag.Bool("replay-answers", false, "rebuild answers from captured assessment snapshots without rerunning retrieval")
	reassessAnswers := flag.Bool("reassess-captured-answers", false, "reassess captured ranked evidence and rebuild answers without rerunning retrieval")
	execute := flag.Bool("execute", false, "execute corpus requests through unified search")
	vaultPath := flag.String("vault", ".", "vault root used with -execute")
	runOut := flag.String("run-out", "", "write executed run artifact to this path")
	fast := flag.Bool("fast", true, "disable network embedding, graph, and refs lanes")
	format := flag.String("format", "json", "json or markdown")
	split := flag.String("split", "", "optional corpus split, such as development or held-out")
	corpusNames := flag.String("corpus-name", "", "optional comma-separated corpus names")
	corpusRootsPath := flag.String("corpus-roots", "", "optional JSON map of corpus names to physical roots and source prefixes")
	families := flag.String("family", "", "optional comma-separated query families")
	profileName := flag.String("profile", string(unifiedsearch.ProfileAgent), "evaluation profile: agent or interactive")
	isolate := flag.Bool("isolate", false, "stage and freshly index each selected physical corpus before execution")
	indexerBinary := flag.String("indexer-binary", "", "current checkout rzm binary used with -isolate")
	embeddingProvider := flag.String("embedding-provider", "", "optional provider override for isolated live-provider evaluation")
	embeddingModel := flag.String("embedding-model", "", "optional model override for isolated live-provider evaluation")
	embeddingEndpoint := flag.String("embedding-endpoint", "", "optional endpoint override for isolated live-provider evaluation")
	embeddingDimensions := flag.Int("embedding-dimensions", 0, "optional vector dimensions override for isolated live-provider evaluation")
	embeddingCache := flag.String("embedding-cache", "", "SQLite file that caches provider embeddings by content hash so repeated live runs are deterministic and cheaper")
	rerankProvider := flag.String("rerank-provider", "", "optional cross-encoder rerank provider for live runs (voyage)")
	rerankModel := flag.String("rerank-model", "", "optional cross-encoder rerank model override")
	flag.Parse()
	if *corpusPath == "" || (*runPath == "" && !*execute) {
		fail("-corpus and either -run or -execute are required")
	}
	if *execute && *rescoreFromCorpusPath != "" {
		fail("-rescore-from-corpus requires a preserved -run, not -execute")
	}
	if *replayAnswers && *reassessAnswers {
		fail("-replay-answers and -reassess-captured-answers are mutually exclusive")
	}
	if (*replayAnswers || *reassessAnswers) && (*execute || *runOut == "") {
		fail("-replay-answers requires a preserved -run and -run-out")
	}
	if *replayAnswers || *reassessAnswers {
		same, err := sameResolvedPath(*runPath, *runOut)
		if err != nil {
			fail(err.Error())
		}
		if same {
			fail("-run-out must not overwrite the input run")
		}
	}
	var corpus qualityeval.Corpus
	readJSON(*corpusPath, &corpus)
	var run qualityeval.Run
	selection := qualityeval.Selection{Split: *split, Corpora: commaValues(*corpusNames), Families: commaValues(*families)}
	if *execute {
		profile, err := evaluationProfile(*profileName)
		if err != nil {
			fail(err.Error())
		}
		corpusRoots, err := readCorpusRoots(*corpusRootsPath)
		if err != nil {
			fail(err.Error())
		}
		if err := applyEmbeddingCache(*embeddingCache, *fast, *embeddingProvider); err != nil {
			fail(err.Error())
		}
		if err := applyRerank(*rerankProvider, *rerankModel, *fast); err != nil {
			fail(err.Error())
		}
		embeddingOverride, err := evaluationEmbeddingOverride(*embeddingProvider, *embeddingModel, *embeddingEndpoint, *embeddingDimensions, *isolate, *fast)
		if err != nil {
			fail(err.Error())
		}
		run, err = executeCorpus(context.Background(), corpus, selection, *vaultPath, *fast, profile, corpusRoots, isolationOptions{Enabled: *isolate, IndexerBinary: *indexerBinary, EmbeddingOverride: embeddingOverride})
		if err != nil {
			fail(err.Error())
		}
		if *runOut != "" {
			writeJSON(*runOut, run)
		}
	} else {
		runBytes, err := os.ReadFile(*runPath)
		if err != nil {
			fail(err.Error())
		}
		if err := json.Unmarshal(runBytes, &run); err != nil {
			fail(err.Error())
		}
		if *replayAnswers || *reassessAnswers {
			if *reassessAnswers {
				run, err = reassessCapturedAnswers(corpus, run, runBytes)
			} else {
				run, err = replayCapturedAnswers(corpus, run, runBytes)
			}
			if err != nil {
				fail(err.Error())
			}
			writeJSON(*runOut, run)
		}
	}
	var report qualityeval.Report
	var evalErr error
	if *rescoreFromCorpusPath != "" {
		var originalCorpus qualityeval.Corpus
		readJSON(*rescoreFromCorpusPath, &originalCorpus)
		report, evalErr = qualityeval.EvaluateJudgmentRescore(originalCorpus, corpus, run, selection)
	} else {
		report, evalErr = qualityeval.EvaluateSelection(corpus, run, selection)
	}
	if evalErr != nil {
		fail(evalErr.Error())
	}
	if *format == "markdown" {
		if *baselinePath != "" {
			var baselineRun qualityeval.Run
			readJSON(*baselinePath, &baselineRun)
			baseline, baselineErr := qualityeval.EvaluateSelection(corpus, baselineRun, selection)
			if baselineErr != nil {
				fail(baselineErr.Error())
			}
			renderMarkdownComparison(baseline, report)
			return
		}
		renderMarkdown(report)
		return
	}
	if *baselinePath != "" {
		var baselineRun qualityeval.Run
		readJSON(*baselinePath, &baselineRun)
		baseline, baselineErr := qualityeval.EvaluateSelection(corpus, baselineRun, selection)
		if baselineErr != nil {
			fail(baselineErr.Error())
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			Baseline  qualityeval.Report `json:"baseline"`
			Candidate qualityeval.Report `json:"candidate"`
		}{Baseline: baseline, Candidate: report}); err != nil {
			fail(err.Error())
		}
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fail(err.Error())
	}
}

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
			def, loadErr := obsidian.LoadDefinitionFromPath(root)
			if loadErr != nil {
				return qualityeval.Run{}, fmt.Errorf("load vault %s: %w", root, loadErr)
			}
			embCfg, loadErr := obsidian.LoadEmbeddingsConfig(root)
			if loadErr != nil {
				return qualityeval.Run{}, fmt.Errorf("load embeddings %s: %w", root, loadErr)
			}
			embCfg.IndexPath = obsidian.UnifiedIndexPath(root, embCfg.IndexPath)
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
			Query: primary, Queries: queries, QueryInputs: corpusQueryInputs(c), Seeds: c.Seeds, IntentInput: c.Intent,
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

func captureAnswerInputSnapshot(result unifiedsearch.ApplicationResult, sourcePrefix string) (json.RawMessage, error) {
	snapshot := answerInputSnapshot{
		Version: answerInputSnapshotVersion, SourcePrefix: sourcePrefix, Intent: result.Request.Intent, Query: unifiedsearch.JoinQueryInputs(result.Request.Queries), Queries: append([]unifiedsearch.QueryInput(nil), result.Request.Queries...),
		Target: result.Target, Warnings: append([]search.Warning(nil), result.Warnings...), Availability: result.Availability, Sources: append([]unifiedsearch.SourceAssessment(nil), result.Sources...), RemainingEvidenceLimit: answerEvidenceLimitSignals(result.Answer),
	}
	return json.Marshal(snapshot)
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

const answerInputSnapshotVersion = "searchquality-answer-input-v1"

type answerInputSnapshot struct {
	Version                string                           `json:"version"`
	SourcePrefix           string                           `json:"sourcePrefix,omitempty"`
	Intent                 search.Intent                    `json:"intent"`
	Query                  string                           `json:"query"`
	Queries                []unifiedsearch.QueryInput       `json:"queries,omitempty"`
	Target                 unifiedsearch.TargetResolution   `json:"target"`
	Warnings               []search.Warning                 `json:"warnings,omitempty"`
	Availability           unifiedsearch.Availability       `json:"availability"`
	Sources                []unifiedsearch.SourceAssessment `json:"sources"`
	RemainingEvidenceLimit []string                         `json:"remainingEvidenceLimit,omitempty"`
}

func replayCapturedAnswers(corpus qualityeval.Corpus, run qualityeval.Run, sourceRun []byte) (qualityeval.Run, error) {
	return transformCapturedAnswers(corpus, run, sourceRun, false)
}

func reassessCapturedAnswers(corpus qualityeval.Corpus, run qualityeval.Run, sourceRun []byte) (qualityeval.Run, error) {
	return transformCapturedAnswers(corpus, run, sourceRun, true)
}

func transformCapturedAnswers(corpus qualityeval.Corpus, run qualityeval.Run, sourceRun []byte, reassess bool) (qualityeval.Run, error) {
	if run.AnswerTransform != nil {
		return qualityeval.Run{}, fmt.Errorf("run already contains an answer transform")
	}
	corpusFingerprint, err := qualityeval.Fingerprint(corpus)
	if err != nil {
		return qualityeval.Run{}, err
	}
	if run.CorpusFingerprint == "" || run.CorpusFingerprint != corpusFingerprint {
		return qualityeval.Run{}, fmt.Errorf("answer replay corpus fingerprint mismatch: run=%q corpus=%q", run.CorpusFingerprint, corpusFingerprint)
	}
	started := time.Now()
	cases := make(map[string]qualityeval.Case, len(corpus.Cases))
	for _, c := range corpus.Cases {
		cases[c.ID] = c
	}
	out := run
	out.AnswerSelector = "current"
	out.Results = append([]qualityeval.QueryResult(nil), run.Results...)
	for index := range out.Results {
		c, ok := cases[out.Results[index].ID]
		if !ok {
			return qualityeval.Run{}, fmt.Errorf("answer replay result %s is absent from corpus", out.Results[index].ID)
		}
		if len(out.Results[index].AnswerInputSnapshot) == 0 {
			return qualityeval.Run{}, fmt.Errorf("answer replay result %s lacks %s snapshot", out.Results[index].ID, answerInputSnapshotVersion)
		}
		var snapshot answerInputSnapshot
		if err := json.Unmarshal(out.Results[index].AnswerInputSnapshot, &snapshot); err != nil {
			return qualityeval.Run{}, fmt.Errorf("decode answer snapshot for %s: %w", out.Results[index].ID, err)
		}
		if snapshot.Version != answerInputSnapshotVersion {
			return qualityeval.Run{}, fmt.Errorf("answer replay result %s has snapshot version %q; want %q", out.Results[index].ID, snapshot.Version, answerInputSnapshotVersion)
		}
		if err := validateAnswerSnapshotContract(c, snapshot); err != nil {
			return qualityeval.Run{}, fmt.Errorf("answer replay result %s: %w", out.Results[index].ID, err)
		}
		if err := validateCapturedAnswerTrace(c, snapshot, out.Results[index]); err != nil {
			return qualityeval.Run{}, fmt.Errorf("answer replay result %s: %w", out.Results[index].ID, err)
		}
		if reassess {
			results := make([]search.RankedResult, len(snapshot.Sources))
			for sourceIndex := range snapshot.Sources {
				results[sourceIndex] = snapshot.Sources[sourceIndex].Result
			}
			snapshot.Sources = searchapplication.AssessQuery(snapshot.Query, snapshot.Intent, results)
			updatedSnapshot, marshalErr := json.Marshal(snapshot)
			if marshalErr != nil {
				return qualityeval.Run{}, fmt.Errorf("encode reassessed answer snapshot for %s: %w", out.Results[index].ID, marshalErr)
			}
			out.Results[index].AnswerInputSnapshot = updatedSnapshot
		}
		packet := unifiedsearch.BuildAssessedAnswerForFacets(snapshot.Intent, snapshot.Query, snapshot.Queries, snapshot.Target, snapshot.Warnings, snapshot.Availability, snapshot.Sources)
		packet = unifiedsearch.ApplyAnswerEvidenceLimits(snapshot.Intent, packet, snapshot.RemainingEvidenceLimit)
		applyAnswerPacketToQueryResult(c, snapshot.SourcePrefix, snapshot.Sources, packet, &out.Results[index])
	}
	executionFingerprint, binaryFingerprint, err := currentAnswerReplayIdentity()
	if err != nil {
		return qualityeval.Run{}, err
	}
	sourceDigest := sha256.Sum256(sourceRun)
	method := "captured-assessment-answer-replay-v1"
	if reassess {
		method = "captured-ranked-evidence-query-reassessment-v1"
	}
	out.AnswerTransform = &qualityeval.AnswerTransformProvenance{
		Method: method, SourceAnswerSelector: run.AnswerSelector, TargetAnswerSelector: "current", SourceRunSHA256: hex.EncodeToString(sourceDigest[:]), SourceExecutionFingerprint: run.ExecutionFingerprint,
		TransformerExecutionFingerprint: executionFingerprint, TransformerBinarySHA256: strings.TrimPrefix(binaryFingerprint, "sha256:"), TransformedAt: time.Now().UTC().Format(time.RFC3339Nano), TransformDurationMS: float64(time.Since(started).Microseconds()) / 1000,
	}
	return out, nil
}

func validateAnswerSnapshotContract(c qualityeval.Case, snapshot answerInputSnapshot) error {
	expected := corpusQueryInputs(c)
	if len(expected) == 0 {
		primary := ""
		extras := append([]string(nil), c.Queries...)
		if len(extras) > 0 {
			primary, extras = extras[0], extras[1:]
		}
		expected = unifiedsearch.NormalizeQueryInputs(primary, extras, c.Intent)
	}
	expectedIntent := search.Intent(strings.TrimSpace(c.Intent))
	if expectedIntent == "" {
		expectedIntent = search.IntentSearch
	}
	expected = unifiedsearch.NormalizeExplicitQueryInputs(expected, string(expectedIntent))
	if snapshot.Intent != expectedIntent || !reflect.DeepEqual(snapshot.Queries, expected) || snapshot.Query != unifiedsearch.JoinQueryInputs(expected) {
		return fmt.Errorf("captured request does not match corpus execution contract")
	}
	return nil
}

func validateCapturedAnswerTrace(c qualityeval.Case, snapshot answerInputSnapshot, result qualityeval.QueryResult) error {
	if result.Status != string(snapshot.Target.Status) {
		return fmt.Errorf("captured target status does not match preserved result trace")
	}
	expectedTargets := evaluationTargetIdentities(snapshot.SourcePrefix, snapshot.Target.Selected)
	if !reflect.DeepEqual(result.TargetIdentities, expectedTargets) {
		return fmt.Errorf("captured target identity does not match preserved result trace")
	}
	var expectedWarnings []string
	for _, warning := range snapshot.Warnings {
		expectedWarnings = append(expectedWarnings, warning.Code)
	}
	if !reflect.DeepEqual(result.Warnings, expectedWarnings) {
		return fmt.Errorf("captured warnings do not match preserved result trace")
	}

	targetIdentity := ""
	if len(expectedTargets) > 0 {
		targetIdentity = expectedTargets[0]
	}
	expectedFirstPage := make([]qualityeval.CandidateTrace, 0, len(snapshot.Sources))
	for _, assessed := range snapshot.Sources {
		ranked := assessed.Result
		source, identity := evaluationRankedSource(c, snapshot.SourcePrefix, ranked)
		if source == "" {
			source = ranked.Handle.String()
		}
		trace := qualityeval.CandidateTrace{
			Source: source, Identity: identity, Handle: ranked.Handle.String(), Page: 1,
			Eligibility: string(assessed.Eligibility), Relationship: assessed.Relationship,
			TargetIdentity: targetIdentity, Score: ranked.FinalScore,
		}
		for _, evidence := range ranked.Evidence {
			trace.Evidence = append(trace.Evidence, evidence.Type)
		}
		expectedFirstPage = append(expectedFirstPage, trace)
	}
	actualFirstPage := make([]qualityeval.CandidateTrace, 0, len(expectedFirstPage))
	for _, trace := range result.Candidates {
		if trace.Page == 1 {
			actualFirstPage = append(actualFirstPage, trace)
		}
	}
	if !reflect.DeepEqual(actualFirstPage, expectedFirstPage) {
		return fmt.Errorf("captured ranked sources do not match preserved first-page candidate trace")
	}

	expectedSources := make([]string, 0, len(result.Candidates))
	for _, trace := range result.Candidates {
		expectedSources = appendUnique(expectedSources, trace.Source)
	}
	if !reflect.DeepEqual(result.Sources, expectedSources) {
		return fmt.Errorf("preserved sources do not match deduplicated candidate page trace")
	}
	return nil
}

func sameResolvedPath(left, right string) (bool, error) {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo) {
		return true, nil
	}
	if leftErr != nil && !os.IsNotExist(leftErr) {
		return false, leftErr
	}
	if rightErr != nil && !os.IsNotExist(rightErr) {
		return false, rightErr
	}
	resolve := func(path string) (string, error) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, filepath.Base(absolute)), nil
	}
	resolvedLeft, err := resolve(left)
	if err != nil {
		return false, err
	}
	resolvedRight, err := resolve(right)
	if err != nil {
		return false, err
	}
	return resolvedLeft == resolvedRight, nil
}

func currentAnswerReplayIdentity() (string, string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	repoRoot, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", "", fmt.Errorf("resolve transformer repository root: %w", err)
	}
	root = strings.TrimSpace(string(repoRoot))
	_, executionFingerprint, err := currentExecutionIdentity(root)
	if err != nil {
		return "", "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	binaryFingerprint, err := fileFingerprint(executable)
	if err != nil {
		return "", "", err
	}
	return executionFingerprint, binaryFingerprint, nil
}

func answerEvidenceLimitSignals(packet answer.Response) []string {
	var out []string
	for _, missing := range packet.Coverage.Missing {
		if strings.HasSuffix(missing, ":more_results") {
			out = appendUnique(out, missing)
		}
	}
	sort.Strings(out)
	return out
}

func applyAnswerPacketToQueryResult(c qualityeval.Case, prefix string, sources []unifiedsearch.SourceAssessment, packet answer.Response, result *qualityeval.QueryResult) {
	result.MustRead = nil
	result.Roles = nil
	result.Relationships = nil
	result.Facets = nil
	selected := map[string]struct{}{}
	for _, item := range packet.MustRead {
		source := evaluationAnswerSource(c, prefix, item)
		result.MustRead = appendUnique(result.MustRead, source)
		selected[source] = struct{}{}
		roles := item.SupportedRoles
		if len(roles) == 0 {
			roles = []string{item.Role}
		}
		for _, role := range roles {
			result.Roles = appendUniqueRole(result.Roles, qualityeval.RoleAssignment{Role: evaluationRole(role, source), Source: source})
		}
	}
	for _, assessed := range sources {
		source, _ := evaluationRankedSource(c, prefix, assessed.Result)
		if source == "" {
			source = assessed.Result.Handle.String()
		}
		if _, ok := selected[source]; !ok {
			continue
		}
		if assessed.Relevance == unifiedsearch.RelevanceStrong && assessed.Eligibility == unifiedsearch.EvidencePrimary && assessed.Relationship != "" {
			result.Relationships = appendUniqueEvidence(result.Relationships, qualityeval.EvidenceAssignment{Value: assessed.Relationship, Source: source})
		}
		for _, facet := range assessed.FacetSupport {
			if !facet.Covers() {
				continue
			}
			result.Facets = appendUniqueEvidence(result.Facets, qualityeval.EvidenceAssignment{Value: strings.TrimSpace(facet.Text), Source: source})
		}
	}
	result.Confidence = packet.Confidence.Level
}

func appendUniqueRole(values []qualityeval.RoleAssignment, value qualityeval.RoleAssignment) []qualityeval.RoleAssignment {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueEvidence(values []qualityeval.EvidenceAssignment, value qualityeval.EvidenceAssignment) []qualityeval.EvidenceAssignment {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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

func commaValues(raw string) []string {
	var out []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func caseSelected(c qualityeval.Case, selection qualityeval.Selection) bool {
	if selection.Split != "" && c.Split != selection.Split {
		return false
	}
	// Retired cases reference sources their fixture cannot index; they run only when asked for by name.
	if selection.Split == "" && c.Split == "retired" {
		return false
	}
	contains := func(values []string, target string) bool {
		if len(values) == 0 {
			return true
		}
		for _, value := range values {
			if value == target {
				return true
			}
		}
		return false
	}
	return contains(selection.Corpora, c.Corpus) && contains(selection.Families, c.Family)
}

type corpusRoot struct {
	Root         string `json:"root"`
	SourcePrefix string `json:"sourcePrefix"`
	// Excludes lists root-relative paths left out of an isolated copy; when
	// empty, the repository corpus uses its default exclusions.
	Excludes []string `json:"excludes,omitempty"`
}

var defaultCorpusRoots = map[string]corpusRoot{
	"rhizome-repository":    {Root: "."},
	"polyglot-code-fixture": {Root: "testdata/integration/python-app/vault", SourcePrefix: "testdata/integration/python-app/vault"},
	"typed-note-fixture":    {Root: "testdata/search-quality/typed-note-vault", SourcePrefix: "testdata/search-quality/typed-note-vault"},
	"prose-knowledge-vault": {Root: "testdata/search-quality/prose-vault", SourcePrefix: "testdata/search-quality/prose-vault"},
	"title-phrase-vault":    {Root: "testdata/search-quality/title-phrase-vault", SourcePrefix: "testdata/search-quality/title-phrase-vault"},
	"linked-topics-vault":   {Root: "testdata/search-quality/linked-topics-vault", SourcePrefix: "testdata/search-quality/linked-topics-vault"},
}

func readCorpusRoots(path string) (map[string]corpusRoot, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	var roots map[string]corpusRoot
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open corpus roots: %w", err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&roots); err != nil {
		return nil, fmt.Errorf("decode corpus roots: %w", err)
	}
	for name, mapping := range roots {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(mapping.Root) == "" {
			return nil, fmt.Errorf("corpus root mappings require non-empty names and roots")
		}
		prefix := filepath.ToSlash(filepath.Clean(strings.TrimSpace(mapping.SourcePrefix)))
		if filepath.IsAbs(mapping.SourcePrefix) || prefix == ".." || strings.HasPrefix(prefix, "../") {
			return nil, fmt.Errorf("corpus %q sourcePrefix must be repository-relative", name)
		}
	}
	return roots, nil
}

func corpusVault(base, corpus string, overrides map[string]corpusRoot) (root, prefix string, err error) {
	mapping, ok := overrides[corpus]
	if !ok {
		mapping, ok = defaultCorpusRoots[corpus]
	}
	if !ok {
		return "", "", fmt.Errorf("no physical root mapping for corpus %q; provide -corpus-roots", corpus)
	}
	root = strings.TrimSpace(mapping.Root)
	if !filepath.IsAbs(root) {
		root = filepath.Join(base, filepath.FromSlash(root))
	}
	prefix = filepath.ToSlash(strings.Trim(strings.TrimSpace(mapping.SourcePrefix), "/"))
	return filepath.Clean(root), prefix, nil
}

func evaluationProfile(raw string) (unifiedsearch.Profile, error) {
	profile := unifiedsearch.Profile(strings.ToLower(strings.TrimSpace(raw)))
	if profile != unifiedsearch.ProfileAgent && profile != unifiedsearch.ProfileInteractive {
		return "", fmt.Errorf("unknown evaluation profile %q (want agent or interactive)", raw)
	}
	return profile, nil
}

func prefixedSource(prefix, source string) string {
	source = filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(source), "./"))
	if source == "" || prefix == "" || strings.HasPrefix(source, prefix+"/") {
		return source
	}
	return filepath.ToSlash(filepath.Join(prefix, source))
}

func evaluationRankedSource(c qualityeval.Case, prefix string, ranked search.RankedResult) (source, identity string) {
	source = prefixedSource(prefix, ranked.Path)
	copy := ranked
	copy.Path = source
	if copy.NodeRef != nil {
		node := *copy.NodeRef
		node.NotePath = prefixedSource(prefix, node.NotePath)
		copy.NodeRef = &node
	}
	identity = searchapplication.CanonicalSourceIdentity(copy)
	if _, explicitlyJudged := c.Judgments[identity]; explicitlyJudged {
		return identity, identity
	}
	return source, identity
}

func evaluationAnswerSource(c qualityeval.Case, prefix string, item answer.Item) string {
	path := prefixedSource(prefix, item.Path)
	identity := ""
	if item.NodeRef != nil && item.NodeRef.Kind != "" && item.NodeRef.Kind != "NOTE" {
		identity = strings.Join([]string{"node", prefixedSource(prefix, item.NodeRef.NotePath), item.NodeRef.NodeID, item.NodeRef.Fragment, item.NodeRef.StructuralFingerprint}, "\x00")
	} else if item.FQN != "" {
		identity = strings.Join([]string{"fqn", item.FQN, path}, "\x00")
	} else if path != "" {
		identity = "path\x00" + path
	}
	if _, explicitlyJudged := c.Judgments[identity]; explicitlyJudged {
		return identity
	}
	return path
}

func evaluationTargetIdentities(prefix string, candidate *search.TargetCandidate) []string {
	if candidate == nil {
		return nil
	}
	var identities []string
	for _, value := range []string{candidate.FQN, prefixedSource(prefix, candidate.Path), candidate.Symbol} {
		if strings.TrimSpace(value) != "" {
			identities = appendUnique(identities, value)
		}
	}
	return identities
}

// evaluationRole maps presentation roles onto the judged corpus vocabulary.
func evaluationRole(role, path string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "overview", "documentation", "decision", "doc":
		return "documentation"
	case "entrypoint", "entry_point", "implementation", "impl":
		return "implementation"
	case "test":
		return "test"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".html", ".htm":
		return "documentation"
	default:
		return strings.ToLower(strings.TrimSpace(role))
	}
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, prior := range values {
		if prior == value {
			return values
		}
	}
	return append(values, value)
}

func writeJSON(path string, value any) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail(err.Error())
	}
	f, err := os.Create(path)
	if err != nil {
		fail(err.Error())
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fail(err.Error())
	}
}

func readJSON(path string, target any) {
	f, err := os.Open(path)
	if err != nil {
		fail(err.Error())
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(target); err != nil {
		fail(err.Error())
	}
}
func fail(message string) { _, _ = fmt.Fprintln(os.Stderr, message); os.Exit(2) }
func renderMarkdown(report qualityeval.Report) {
	fmt.Println("---")
	fmt.Println("type: ReferenceDoc")
	fmt.Println("reference-kind: analysis")
	fmt.Println("summary: \"Frozen search quality evaluation report with metric denominators and retained failure evidence.\"")
	fmt.Printf("last-verified: %s\n", time.Now().UTC().Format("2006-01-02"))
	fmt.Println("---")
	fmt.Printf("\n# Search quality report\n\nRevision: `%s`  \nCorpus fingerprint: `%s`  \nMode: `%s`  \nSplit: `%s`\n\n", report.Revision, report.CorpusFingerprint, report.Mode, report.Split)
	fmt.Println("| Slice | Cases | nDCG@10 | Recall@20 | Nav@1 | Nav@3 | Must-read precision | Role coverage | High-confidence precision | High-confidence coverage | Unjudged top 10 |")
	fmt.Println("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	printRow("overall", report.Overall)
	keys := make([]string, 0, len(report.ByCorpus))
	for key := range report.ByCorpus {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		printRow("corpus: "+key, report.ByCorpus[key])
	}
	keys = keys[:0]
	for key := range report.ByFamily {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		printRow("family: "+key, report.ByFamily[key])
	}
	fmt.Printf("\n## Retained failures\n\nTotal: %d. Complete per-case output is retained in the adjacent JSON artifact.\n", len(report.Failures))
	for i, failure := range report.Failures {
		if i >= 25 {
			fmt.Printf("\n%d additional failures are retained in JSON.\n", len(report.Failures)-i)
			break
		}
		fmt.Printf("\n- `%s` `%s`: %s\n", failure.ID, failure.Stage, failure.Reason)
	}
}

func renderMarkdownComparison(baseline, candidate qualityeval.Report) {
	fmt.Println("---")
	fmt.Println("type: ReferenceDoc")
	fmt.Println("reference-kind: analysis")
	fmt.Println("summary: \"Frozen search quality baseline and candidate comparison.\"")
	fmt.Printf("last-verified: %s\n", time.Now().UTC().Format("2006-01-02"))
	fmt.Println("---")
	fmt.Printf("\n# Search quality comparison\n\nCorpus fingerprint: `%s`  \nSplit: `%s`\n\n", candidate.CorpusFingerprint, candidate.Split)
	fmt.Println("| Run | Cases | nDCG@10 | Recall@20 | Nav@1 | Nav@3 | Must-read precision | Role coverage | High-confidence precision | High-confidence coverage | Unjudged top 10 |")
	fmt.Println("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	printRow("baseline", baseline.Overall)
	printRow("candidate", candidate.Overall)
}
func printRow(label string, m qualityeval.Metrics) {
	fmt.Printf("| %s | %d | %.3f (%d) | %.3f (%d) | %.3f (%d) | %.3f (%d) | %.3f (%d) | %.3f (%d) | %.3f (%d) | %.3f (%d) | %d |\n", label, m.Cases, m.NDCG10, m.NDCG10Cases, m.RequiredRecall20, m.RequiredRecall20Cases, m.NavigationSuccess1, m.NavigationCases, m.NavigationSuccess3, m.NavigationCases, m.MustReadPrecision, m.MustReadPrecisionCases, m.RequiredRoleCoverage, m.RequiredRoleCoverageCases, m.HighConfidencePrecision, m.HighConfidenceCases, m.HighConfidenceCoverage, m.AnswerableCases, m.UnjudgedTop10)
}
