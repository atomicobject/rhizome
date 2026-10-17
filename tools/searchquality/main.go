package main

import (
	"context"

	"encoding/json"
	"flag"
	"fmt"
	"os"

	"path/filepath"

	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"

	"github.com/atomicobject/rhizome/pkg/search"

	"github.com/atomicobject/rhizome/tools/searchquality/internal/qualityeval"
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
