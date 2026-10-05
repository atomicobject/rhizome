package identifierreconcile

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AmbiguousLinkProvenanceResult keeps independently blockable unresolved
// reasons beside opaque successful decisions, keyed by stable request binding.
type AmbiguousLinkProvenanceResult struct {
	Decisions  map[string]*AmbiguousLinkProvenanceDecision `json:"decisions"`
	Unresolved map[string]string                           `json:"unresolved"`
	Stats      GitProvenanceStats                          `json:"stats"`
}

// AmbiguousLinkGitResolver reuses the pinned local-only Git runner and bounded
// history limits from keeper provenance.
type AmbiguousLinkGitResolver struct {
	runner            gitCommandRunner
	schema            *ontology.Schema
	maxCommitsPerPath int
	maxBlobBytes      int
	maxBatchBytes     int
}

// Resolve resolves only uniquely ancestral bare-ID candidates. It never
// fetches and never uses filesystem timestamps.
func (r AmbiguousLinkGitResolver) Resolve(ctx context.Context, vaultRoot string, requests []AmbiguousLinkProvenanceRequest) (AmbiguousLinkProvenanceResult, error) {
	result := AmbiguousLinkProvenanceResult{Decisions: make(map[string]*AmbiguousLinkProvenanceDecision), Unresolved: make(map[string]string)}
	normalized := make([]normalizedAmbiguousLinkRequest, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for index, request := range requests {
		item, err := normalizeAmbiguousLinkRequest(request)
		if err != nil {
			return result, err
		}
		if _, duplicate := seen[item.key]; duplicate {
			return result, fmt.Errorf("ambiguous link provenance request is duplicated")
		}
		seen[item.key] = struct{}{}
		normalized[index] = item
	}
	if len(normalized) == 0 {
		return result, nil
	}
	if r.runner == nil {
		r.runner = execGitCommandRunner{}
	}
	if r.maxCommitsPerPath <= 0 {
		r.maxCommitsPerPath = defaultMaxHistoryCommits
	}
	if r.maxBlobBytes <= 0 {
		r.maxBlobBytes = defaultMaxHistoryBlob
	}
	if r.maxBatchBytes <= 0 {
		r.maxBatchBytes = defaultMaxHistoryBatch
	}

	claimsByID := make(map[string]Claim)
	for _, item := range normalized {
		for _, candidate := range item.request.Candidates {
			claimsByID[candidate.ID()] = candidate
		}
	}
	claims := make([]Claim, 0, len(claimsByID))
	for _, claim := range claimsByID {
		claims = append(claims, claim)
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].ID() < claims[j].ID() })
	candidateResult, err := (GitProvenanceResolver{
		runner: r.runner, schema: r.schema, maxCommitsPerPath: r.maxCommitsPerPath,
		maxBlobBytes: r.maxBlobBytes, maxBatchBytes: r.maxBatchBytes,
	}).Resolve(ctx, vaultRoot, claims)
	if err != nil {
		return result, err
	}
	result.Stats = candidateResult.Stats

	run := func(dir string, stdin []byte, args ...string) ([]byte, []byte, error) {
		result.Stats.Commands++
		return r.runner.Run(ctx, dir, stdin, []string{"GIT_NO_LAZY_FETCH=1", "LC_ALL=C"}, args...)
	}
	revOut, _, err := run(vaultRoot, nil, "rev-parse", "--show-toplevel", "--is-shallow-repository")
	if err != nil {
		return unresolvedAll(result, normalized, "git repository unavailable"), nil
	}
	revLines := strings.Split(strings.TrimSpace(string(revOut)), "\n")
	if len(revLines) < 2 || strings.EqualFold(strings.TrimSpace(revLines[len(revLines)-1]), "true") {
		return unresolvedAll(result, normalized, "git history unavailable or shallow"), nil
	}
	repoRoot := strings.TrimSpace(revLines[0])
	byRepoPath := make(map[string][]normalizedAmbiguousLinkRequest)
	for _, item := range normalized {
		repoPath, pathErr := repositoryRelativePath(repoRoot, vaultRoot, item.request.NotePath)
		if pathErr != nil || strings.ContainsAny(repoPath, "\r\n") {
			result.Unresolved[item.key] = "link source path is outside repository"
			continue
		}
		byRepoPath[repoPath] = append(byRepoPath[repoPath], item)
	}
	repoPaths := make([]string, 0, len(byRepoPath))
	for repoPath := range byRepoPath {
		repoPaths = append(repoPaths, repoPath)
	}
	sort.Strings(repoPaths)
	result.Stats.UniquePaths += len(repoPaths)
	lsArgs := append([]string{"--literal-pathspecs", "ls-files", "--stage", "-z", "--"}, repoPaths...)
	lsOut, _, lsErr := run(repoRoot, nil, lsArgs...)
	if lsErr != nil {
		return unresolvedAll(result, normalized, "git tracked-file inventory unavailable"), nil
	}
	tracked := parseTrackedRegularFiles(lsOut)

	introductions := make(map[string]string)
	for _, repoPath := range repoPaths {
		items := byRepoPath[repoPath]
		if !tracked[repoPath] {
			markLinkRequestsUnresolved(result.Unresolved, items, "link source is untracked")
			continue
		}
		events, reason, loadErr := r.loadLinkHistory(ctx, repoRoot, repoPath, run)
		if loadErr != nil {
			return result, loadErr
		}
		if reason != "" {
			markLinkRequestsUnresolved(result.Unresolved, items, reason)
			continue
		}
		result.Stats.HistoricalBlobsParsed += len(events)
		bareLinks := indexHistoricalBareLinks(events)
		result.Stats.StructuredBlobsScanned += len(events)
		for _, item := range items {
			oid, complete := linkIntroductionOID(item, events, bareLinks)
			if !complete {
				result.Unresolved[item.key] = "link introduction history incomplete"
				continue
			}
			introductions[item.key] = oid
		}
	}

	reachableBySource := make(map[string]map[string]struct{})
	for _, sourceOID := range uniqueStringValues(introductions) {
		graphOut, _, graphErr := run(repoRoot, nil, "rev-list", "--parents", "--topo-order", "--max-count="+strconv.Itoa(r.maxCommitsPerPath+1), sourceOID)
		if graphErr != nil {
			reachableBySource[sourceOID] = nil
			continue
		}
		reachable, complete := parseBoundedReachableOIDs(graphOut, sourceOID, r.maxCommitsPerPath)
		if !complete {
			reachableBySource[sourceOID] = nil
			continue
		}
		reachableBySource[sourceOID] = reachable
	}

	for _, item := range normalized {
		if _, already := result.Unresolved[item.key]; already {
			continue
		}
		sourceOID, ok := introductions[item.key]
		if !ok {
			result.Unresolved[item.key] = "link introduction unavailable"
			continue
		}
		reachable := reachableBySource[sourceOID]
		if reachable == nil {
			result.Unresolved[item.key] = "link ancestry history incomplete"
			continue
		}
		evidence := make([]LinkCandidateAncestry, len(item.request.Candidates))
		winnerIndex := -1
		complete := true
		for index, candidate := range item.request.Candidates {
			claimEvidence := candidateResult.Evidence[candidate.ID()]
			ancestor := false
			if claimEvidence.Complete && isFullGitOID(claimEvidence.FullOID) {
				_, ancestor = reachable[claimEvidence.FullOID]
			} else {
				complete = false
			}
			evidence[index] = LinkCandidateAncestry{Ref: item.candidateRefs[index], ClaimID: candidate.ID(), IntroductionOID: claimEvidence.FullOID, Ancestor: ancestor, Complete: claimEvidence.Complete}
			if ancestor {
				if winnerIndex >= 0 {
					winnerIndex = -2
				} else {
					winnerIndex = index
				}
			}
		}
		if !complete || winnerIndex < 0 {
			result.Unresolved[item.key] = "candidate ancestry is incomplete or non-unique"
			continue
		}
		payload := ambiguousLinkDecisionPayload{
			NotePath: item.request.NotePath, SourceHash: item.request.SourceHash, LinkIndex: item.request.LinkIndex, RawTarget: item.request.RawTarget,
			CandidateRefs: append([]ontology.NodeRef(nil), item.candidateRefs...), Winner: item.candidateRefs[winnerIndex],
			Evidence: AmbiguousLinkGitEvidence{SourceIntroductionOID: sourceOID, Candidates: evidence},
		}
		decision := sealAmbiguousLinkDecision(payload)
		if _, err := decision.ValidatedSnapshot(); err != nil {
			return result, err
		}
		result.Decisions[item.key] = decision
	}
	return result, nil
}

func (r AmbiguousLinkGitResolver) loadLinkHistory(ctx context.Context, repoRoot, repoPath string, run func(string, []byte, ...string) ([]byte, []byte, error)) ([]gitHistoryEvent, string, error) {
	logOut, stderr, err := run(repoRoot, nil,
		"--literal-pathspecs", "-c", "core.quotepath=false", "-c", "diff.renames=true", "-c", "diff.renameLimit=10000",
		"log", "--follow", "--find-renames=50%", "--topo-order", "--format=%x1e%H%x00%at%x00%P%x00", "--name-status", "-z",
		"-n", strconv.Itoa(r.maxCommitsPerPath+1), "--", repoPath,
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, "", ctxErr
		}
		return nil, "git link history unavailable", nil
	}
	if gitRenameDetectionSkipped(stderr) {
		return nil, "git rename detection limit reached", nil
	}
	events, err := parseGitHistory(logOut, repoPath)
	if err != nil || len(events) == 0 || len(events) > r.maxCommitsPerPath {
		return nil, "git link history incomplete", nil
	}
	requests := make([]gitBlobRequest, len(events))
	var stdin strings.Builder
	for index, event := range events {
		requests[index] = gitBlobRequest{repoPath: repoPath, eventIndex: index, spec: event.oid + ":" + event.path}
		stdin.WriteString(requests[index].spec + "\n")
	}
	batchInput := []byte(stdin.String())
	checkOut, _, checkErr := run(repoRoot, batchInput, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if checkErr != nil || validateGitBlobBatchSizes(checkOut, len(requests), r.maxBlobBytes, r.maxBatchBytes) != nil {
		return nil, "git link blob inventory incomplete", nil
	}
	blobOut, _, blobErr := run(repoRoot, batchInput, "cat-file", "--batch")
	if blobErr != nil {
		return nil, "git link blob batch unavailable", nil
	}
	blobs, parseErr := parseGitBlobBatch(blobOut, requests, r.maxBlobBytes)
	if parseErr != nil {
		return nil, parseErr.Error(), nil
	}
	for index := range events {
		events[index].blob = blobs[index]
	}
	return events, "", nil
}

func linkIntroductionOID(item normalizedAmbiguousLinkRequest, events []gitHistoryEvent, bareLinks []map[string]int) (string, bool) {
	if len(events) == 0 || obsidian.StructuredLinkSourceFingerprint(string(events[0].blob)) != item.request.SourceHash {
		return "", false
	}
	introduction := ""
	for index, event := range events {
		if !historicalBareLinkPresent(bareLinks[index], item.request.RawTarget, item.targetOrdinal) {
			break
		}
		introduction = event.oid
	}
	if introduction == "" {
		return "", false
	}
	oldest := events[len(events)-1]
	if historicalBareLinkPresent(bareLinks[len(events)-1], item.request.RawTarget, item.targetOrdinal) && oldest.status != 'A' {
		return "", false
	}
	return introduction, true
}

func indexHistoricalBareLinks(events []gitHistoryEvent) []map[string]int {
	out := make([]map[string]int, len(events))
	for index, event := range events {
		counts := make(map[string]int)
		for _, link := range obsidian.ScanStructuredLinks(string(event.blob)) {
			if link.Path == link.Target && link.Fragment == "" {
				counts[IdentifierComparisonKey(link.Target)]++
			}
		}
		out[index] = counts
	}
	return out
}

func historicalBareLinkPresent(index map[string]int, target string, ordinal int) bool {
	return ordinal == 0 && index[IdentifierComparisonKey(target)] == 1
}

func parseBoundedReachableOIDs(output []byte, sourceOID string, max int) (map[string]struct{}, bool) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || len(lines) > max {
		return nil, false
	}
	out := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || !isFullGitOID(fields[0]) {
			return nil, false
		}
		out[fields[0]] = struct{}{}
	}
	_, found := out[sourceOID]
	return out, found
}

func uniqueStringValues(input map[string]string) []string {
	seen := make(map[string]struct{}, len(input))
	for _, value := range input {
		seen[value] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func markLinkRequestsUnresolved(out map[string]string, items []normalizedAmbiguousLinkRequest, reason string) {
	for _, item := range items {
		out[item.key] = reason
	}
}

func unresolvedAll(result AmbiguousLinkProvenanceResult, items []normalizedAmbiguousLinkRequest, reason string) AmbiguousLinkProvenanceResult {
	markLinkRequestsUnresolved(result.Unresolved, items, reason)
	return result
}
