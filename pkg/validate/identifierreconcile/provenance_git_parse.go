package identifierreconcile

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func gitRenameDetectionSkipped(stderr []byte) bool {
	message := strings.ToLower(string(stderr))
	return strings.Contains(message, "rename detection was skipped") ||
		strings.Contains(message, "inexact rename detection was skipped") ||
		(strings.Contains(message, "renamelimit") && strings.Contains(message, "rename"))
}

type gitHistoryEvent struct {
	oid     string
	author  int64
	parents []string
	path    string
	status  byte
	blob    []byte
}

type gitBlobRequest struct {
	repoPath   string
	eventIndex int
	spec       string
}

func parseGitHistory(output []byte, currentPath string) ([]gitHistoryEvent, error) {
	chunks := bytes.Split(output, []byte{0x1e})
	events := make([]gitHistoryEvent, 0, len(chunks)-1)
	activePath := currentPath
	for _, chunk := range chunks[1:] {
		fields := bytes.SplitN(chunk, []byte{0}, 4)
		if len(fields) != 4 {
			return nil, fmt.Errorf("git history record malformed")
		}
		oid := strings.TrimSpace(string(fields[0]))
		if !validFullOID(oid) {
			return nil, fmt.Errorf("git history OID malformed")
		}
		author, err := strconv.ParseInt(strings.TrimSpace(string(fields[1])), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("git history author date malformed")
		}
		parents := strings.Fields(strings.TrimSpace(string(fields[2])))
		if len(parents) > 1 {
			return nil, fmt.Errorf("git introduction history ambiguous at merge")
		}
		tokens := nonEmptyNULTokens(bytes.TrimLeft(fields[3], "\x00\r\n"))
		if len(tokens) < 2 {
			return nil, fmt.Errorf("git history path record malformed")
		}
		statusText := string(tokens[0])
		status := statusText[0]
		eventPath := activePath
		stopAfterEvent := false
		switch status {
		case 'A', 'M':
			if len(tokens) != 2 || string(tokens[1]) != activePath {
				return nil, fmt.Errorf("git history path chain incomplete")
			}
		case 'R':
			if len(tokens) != 3 || string(tokens[2]) != activePath {
				return nil, fmt.Errorf("git rename history ambiguous")
			}
			eventPath = string(tokens[2])
			activePath = string(tokens[1])
		case 'C':
			if len(tokens) != 3 || string(tokens[2]) != activePath {
				return nil, fmt.Errorf("git copy history ambiguous")
			}
			status = 'A'
			stopAfterEvent = true
		default:
			return nil, fmt.Errorf("git history status %q unsupported", statusText)
		}
		events = append(events, gitHistoryEvent{oid: oid, author: author, parents: parents, path: eventPath, status: status})
		if stopAfterEvent {
			break
		}
	}
	return events, nil
}

func parseGitBlobBatch(output []byte, requests []gitBlobRequest, maxBlobBytes int) ([][]byte, error) {
	reader := bufio.NewReader(bytes.NewReader(output))
	blobs := make([][]byte, 0, len(requests))
	for range requests {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("git blob batch header incomplete")
		}
		parts := strings.Fields(strings.TrimSpace(header))
		if len(parts) == 2 && parts[1] == "missing" {
			return nil, fmt.Errorf("git blob history missing")
		}
		if len(parts) != 3 || parts[1] != "blob" {
			return nil, fmt.Errorf("git blob batch header malformed")
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil || size < 0 || size > maxBlobBytes {
			return nil, fmt.Errorf("git history blob exceeds limit or has invalid size")
		}
		blob := make([]byte, size)
		if _, err := io.ReadFull(reader, blob); err != nil {
			return nil, fmt.Errorf("git history blob incomplete")
		}
		if delimiter, err := reader.ReadByte(); err != nil || delimiter != '\n' {
			return nil, fmt.Errorf("git blob batch delimiter malformed")
		}
		blobs = append(blobs, blob)
	}
	return blobs, nil
}

func validateGitBlobBatchSizes(output []byte, requestCount, maxBlobBytes, maxBatchBytes int) error {
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != requestCount {
		return fmt.Errorf("git blob size inventory incomplete")
	}
	total := int64(0)
	for _, line := range lines {
		parts := strings.Fields(string(line))
		if len(parts) == 2 && parts[1] == "missing" {
			return fmt.Errorf("git blob history missing")
		}
		if len(parts) != 3 || parts[1] != "blob" {
			return fmt.Errorf("git blob size inventory malformed")
		}
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || size < 0 || size > int64(maxBlobBytes) {
			return fmt.Errorf("git history blob exceeds limit or has invalid size")
		}
		total += size
		if total > int64(maxBatchBytes) {
			return fmt.Errorf("git history blob batch exceeds limit")
		}
	}
	return nil
}

func repositoryRelativePath(repoRoot, vaultRoot, vaultPath string) (string, error) {
	vaultPaths, err := paths.NewVaultPaths(vaultRoot)
	if err != nil {
		return "", err
	}
	rel, err := vaultPaths.RelStrict(vaultPath)
	if err != nil {
		return "", err
	}
	abs, err := vaultPaths.Abs(rel)
	if err != nil {
		return "", err
	}
	repoPaths, err := paths.NewVaultPaths(repoRoot)
	if err != nil {
		return "", err
	}
	repoRel, err := repoPaths.RelStrict(abs.String())
	if err != nil {
		return "", fmt.Errorf("path outside repository: %w", err)
	}
	return repoRel.String(), nil
}

func vaultRelativeRepoPath(repoRoot, vaultRoot, repoPath string) (string, error) {
	repoPaths, err := paths.NewVaultPaths(repoRoot)
	if err != nil {
		return "", err
	}
	rel, err := repoPaths.RelStrict(repoPath)
	if err != nil {
		return "", err
	}
	abs, err := repoPaths.Abs(rel)
	if err != nil {
		return "", err
	}
	vaultPaths, err := paths.NewVaultPaths(vaultRoot)
	if err != nil {
		return "", err
	}
	vaultRel, err := vaultPaths.RelStrict(abs.String())
	if err != nil {
		return "", fmt.Errorf("historical path outside vault: %w", err)
	}
	return vaultRel.String(), nil
}

func parseTrackedRegularFiles(output []byte) map[string]bool {
	tracked := make(map[string]bool)
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		parts := bytes.SplitN(record, []byte{'\t'}, 2)
		if len(parts) != 2 {
			continue
		}
		metadata := strings.Fields(string(parts[0]))
		if len(metadata) != 3 || (metadata[0] != "100644" && metadata[0] != "100755") || metadata[2] != "0" {
			continue
		}
		tracked[string(parts[1])] = true
	}
	return tracked
}

func nonEmptyNULTokens(value []byte) [][]byte {
	raw := bytes.Split(value, []byte{0})
	out := make([][]byte, 0, len(raw))
	for _, token := range raw {
		token = bytes.Trim(token, "\r\n")
		if len(token) > 0 {
			out = append(out, token)
		}
	}
	return out
}

func validFullOID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func markClaimsIncomplete(target map[string]ProvenanceEvidence, claims []Claim, reason string) {
	for _, claim := range claims {
		target[claim.ID()] = ProvenanceEvidence{Reason: reason}
	}
}
