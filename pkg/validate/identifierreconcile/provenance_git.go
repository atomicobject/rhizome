package identifierreconcile

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

const (
	defaultMaxHistoryCommits = 4096
	defaultMaxHistoryBlob    = 4 << 20
	defaultMaxHistoryBatch   = 64 << 20
)

// GitProvenanceStats makes subprocess scaling observable without affecting
// deterministic plan fingerprints.
type GitProvenanceStats struct {
	Commands               int `json:"commands"`
	UniquePaths            int `json:"uniquePaths"`
	HistoricalBlobsParsed  int `json:"historicalBlobsParsed"`
	StructuredBlobsScanned int `json:"structuredBlobsScanned"`
}

// GitProvenanceResult contains evidence keyed by Claim.ID.
type GitProvenanceResult struct {
	Evidence map[string]ProvenanceEvidence
	Stats    GitProvenanceStats
}

type gitCommandRunner interface {
	Run(context.Context, string, []byte, []string, ...string) ([]byte, []byte, error)
}

type execGitCommandRunner struct{}

func (execGitCommandRunner) Run(ctx context.Context, dir string, stdin []byte, env []string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-pager"}, args...)...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Env = gitCommandEnvironment(os.Environ(), env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func gitCommandEnvironment(base, overrides []string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, value := range overrides {
		if key, _, ok := strings.Cut(value, "="); ok {
			keys[strings.ToUpper(key)] = struct{}{}
		}
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, value := range base {
		key, _, ok := strings.Cut(value, "=")
		if ok {
			if _, replaced := keys[strings.ToUpper(key)]; replaced {
				continue
			}
		}
		out = append(out, value)
	}
	return append(out, overrides...)
}

// GitProvenanceResolver resolves the introducing commit for current identifier
// claims. It never fetches and never consults filesystem timestamps.
type GitProvenanceResolver struct {
	runner            gitCommandRunner
	schema            *ontology.Schema
	maxCommitsPerPath int
	maxBlobBytes      int
	maxBatchBytes     int
}

// ResolveGitProvenanceWithSchema resolves both note-level and embedded claims,
// including schema-declared source aliases. A schema is required for embedded
// claims because their field ownership is structural rather than file-global.
func ResolveGitProvenanceWithSchema(ctx context.Context, vaultRoot string, schema *ontology.Schema, claims []Claim) (GitProvenanceResult, error) {
	resolver := GitProvenanceResolver{
		runner:            execGitCommandRunner{},
		schema:            schema,
		maxCommitsPerPath: defaultMaxHistoryCommits,
		maxBlobBytes:      defaultMaxHistoryBlob,
		maxBatchBytes:     defaultMaxHistoryBatch,
	}
	return resolver.Resolve(ctx, vaultRoot, claims)
}

// Resolve returns local Git introduction evidence for every supplied claim.
func (r GitProvenanceResolver) Resolve(ctx context.Context, vaultRoot string, claims []Claim) (GitProvenanceResult, error) {
	normalized := make([]Claim, 0, len(claims))
	for _, raw := range claims {
		claim, err := normalizeClaim(raw)
		if err != nil {
			return GitProvenanceResult{}, err
		}
		normalized = append(normalized, claim)
	}
	claims = normalized
	result := GitProvenanceResult{Evidence: make(map[string]ProvenanceEvidence, len(claims))}
	if len(claims) == 0 {
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
	run := func(dir string, stdin []byte, args ...string) ([]byte, []byte, error) {
		result.Stats.Commands++
		return r.runner.Run(ctx, dir, stdin, []string{"GIT_NO_LAZY_FETCH=1", "LC_ALL=C"}, args...)
	}
	fallbackAll := func(reason string) GitProvenanceResult {
		for _, claim := range claims {
			result.Evidence[claim.ID()] = ProvenanceEvidence{Reason: reason}
		}
		return result
	}

	revOut, _, err := run(vaultRoot, nil, "rev-parse", "--show-toplevel", "--is-shallow-repository")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return fallbackAll("git repository unavailable"), nil
	}
	revLines := strings.Split(strings.TrimSpace(string(revOut)), "\n")
	if len(revLines) < 2 || strings.TrimSpace(revLines[0]) == "" {
		return fallbackAll("git repository discovery incomplete"), nil
	}
	repoRoot := strings.TrimSpace(revLines[0])
	if strings.EqualFold(strings.TrimSpace(revLines[len(revLines)-1]), "true") {
		return fallbackAll("shallow git history"), nil
	}
	if r.schema == nil {
		schema, schemaErr := ontology.LoadSchema(vaultRoot)
		if schemaErr != nil {
			return fallbackAll("ontology schema unavailable for provenance"), nil
		}
		r.schema = schema
	}

	claimsByRepoPath := make(map[string][]Claim)
	for _, claim := range claims {
		if !isHistoricalMarkdownCompatibilityPath(claim.Node.NotePath) {
			result.Evidence[claim.ID()] = ProvenanceEvidence{Reason: "identifier provenance supports historical Markdown claims only"}
			continue
		}
		repoPath, pathErr := repositoryRelativePath(repoRoot, vaultRoot, claim.Node.NotePath)
		if pathErr != nil || strings.ContainsAny(repoPath, "\r\n") {
			result.Evidence[claim.ID()] = ProvenanceEvidence{Reason: "identifier path is outside repository or unsupported"}
			continue
		}
		claimsByRepoPath[repoPath] = append(claimsByRepoPath[repoPath], claim)
	}
	repoPaths := make([]string, 0, len(claimsByRepoPath))
	for path := range claimsByRepoPath {
		repoPaths = append(repoPaths, path)
	}
	sort.Strings(repoPaths)
	result.Stats.UniquePaths = len(repoPaths)
	if len(repoPaths) == 0 {
		return result, nil
	}

	lsArgs := append([]string{"--literal-pathspecs", "ls-files", "--stage", "-z", "--"}, repoPaths...)
	lsOut, _, err := run(repoRoot, nil, lsArgs...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return fallbackAll("git tracked-file inventory unavailable"), nil
	}
	tracked := parseTrackedRegularFiles(lsOut)

	histories := make(map[string][]gitHistoryEvent)
	for _, repoPath := range repoPaths {
		if !tracked[repoPath] {
			for _, claim := range claimsByRepoPath[repoPath] {
				result.Evidence[claim.ID()] = ProvenanceEvidence{Reason: "identifier path is untracked"}
			}
			continue
		}
		logOut, logErrOut, logErr := run(repoRoot, nil,
			"--literal-pathspecs", "-c", "core.quotepath=false", "-c", "diff.renames=true", "-c", "diff.renameLimit=10000",
			"log", "--follow", "--find-renames=50%",
			"--topo-order",
			"--format=%x1e%H%x00%at%x00%P%x00", "--name-status", "-z",
			"-n", strconv.Itoa(r.maxCommitsPerPath+1), "--", repoPath,
		)
		if logErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			markClaimsIncomplete(result.Evidence, claimsByRepoPath[repoPath], "git history unavailable")
			continue
		}
		if gitRenameDetectionSkipped(logErrOut) {
			markClaimsIncomplete(result.Evidence, claimsByRepoPath[repoPath], "git rename detection limit reached")
			continue
		}
		events, parseErr := parseGitHistory(logOut, repoPath)
		if parseErr != nil || len(events) == 0 {
			reason := "git history incomplete"
			if parseErr != nil {
				reason = parseErr.Error()
			}
			markClaimsIncomplete(result.Evidence, claimsByRepoPath[repoPath], reason)
			continue
		}
		if len(events) > r.maxCommitsPerPath {
			markClaimsIncomplete(result.Evidence, claimsByRepoPath[repoPath], "git history limit reached")
			continue
		}
		histories[repoPath] = events
	}

	for _, repoPath := range repoPaths {
		events, ok := histories[repoPath]
		if !ok {
			continue
		}
		requests := make([]gitBlobRequest, 0, len(events))
		var stdin strings.Builder
		for eventIndex, event := range events {
			request := gitBlobRequest{repoPath: repoPath, eventIndex: eventIndex, spec: event.oid + ":" + event.path}
			requests = append(requests, request)
			stdin.WriteString(request.spec)
			stdin.WriteByte('\n')
		}
		batchInput := []byte(stdin.String())
		checkOut, _, checkErr := run(repoRoot, batchInput, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
		if checkErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			markClaimsIncomplete(result.Evidence, claimsByRepoPath[repoPath], "git blob size inventory unavailable")
			delete(histories, repoPath)
			continue
		} else if checkErr := validateGitBlobBatchSizes(checkOut, len(requests), r.maxBlobBytes, r.maxBatchBytes); checkErr != nil {
			markClaimsIncomplete(result.Evidence, claimsByRepoPath[repoPath], checkErr.Error())
			delete(histories, repoPath)
			continue
		}
		blobOut, _, blobErr := run(repoRoot, batchInput, "cat-file", "--batch")
		if blobErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			markClaimsIncomplete(result.Evidence, claimsByRepoPath[repoPath], "git blob batch unavailable")
			delete(histories, repoPath)
		} else if blobs, parseErr := parseGitBlobBatch(blobOut, requests, r.maxBlobBytes); parseErr != nil {
			markClaimsIncomplete(result.Evidence, claimsByRepoPath[repoPath], parseErr.Error())
			delete(histories, repoPath)
		} else {
			for index, request := range requests {
				events[request.eventIndex].blob = blobs[index]
			}
			documents := make([]historicalDocument, len(events))
			for index, event := range events {
				historicalPath, pathErr := vaultRelativeRepoPath(repoRoot, vaultRoot, event.path)
				if pathErr != nil {
					documents[index] = historicalDocument{absent: true}
					continue
				}
				documents[index] = parseHistoricalMarkdownDocument(historicalPath, event.blob)
				result.Stats.HistoricalBlobsParsed++
			}
			for _, claim := range claimsByRepoPath[repoPath] {
				result.Evidence[claim.ID()] = evidenceForClaim(claim, events, documents, r.schema)
			}
			delete(histories, repoPath)
		}
	}
	return result, nil
}

// isHistoricalMarkdownCompatibilityPath selects the legacy authored syntax
// supported by Git identifier provenance. Current provider ownership happens
// before this resolver; this check keeps direct callers from treating another
// provider's historical blobs as Markdown.
func isHistoricalMarkdownCompatibilityPath(notePath string) bool {
	return strings.EqualFold(filepath.Ext(strings.TrimSpace(notePath)), ".md")
}
