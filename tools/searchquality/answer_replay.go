package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/tools/searchquality/internal/qualityeval"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

func captureAnswerInputSnapshot(result unifiedsearch.ApplicationResult, sourcePrefix string) (json.RawMessage, error) {
	snapshot := answerInputSnapshot{
		Version: answerInputSnapshotVersion, SourcePrefix: sourcePrefix, Intent: result.Request.Intent, Query: unifiedsearch.JoinQueryInputs(result.Request.Queries), Queries: append([]unifiedsearch.QueryInput(nil), result.Request.Queries...),
		Target: result.Target, Warnings: append([]search.Warning(nil), result.Warnings...), Availability: result.Availability, Sources: append([]unifiedsearch.SourceAssessment(nil), result.Sources...), RemainingEvidenceLimit: answerEvidenceLimitSignals(result.Answer),
	}
	return json.Marshal(snapshot)
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
