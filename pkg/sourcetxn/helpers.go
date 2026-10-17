package sourcetxn

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/atomicobject/rhizome/pkg/paths"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"sort"
	"strings"
)

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func canonicalRepairPath(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	rel, err := paths.CleanRelPath(value)
	if err != nil {
		return "", err
	}
	return rel.String(), nil
}

func repairCollisionKey(value string) string {
	return strings.ToLower(value)
}

func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validNamespaceHash(hash string) bool {
	return strings.HasPrefix(hash, "sha256:") && validRepairJournalDigest(strings.TrimPrefix(hash, "sha256:"))
}

func namespaceCaseCandidateKey(name string) string {
	return norm.NFD.String(cases.Fold().String(norm.NFD.String(name)))
}

func sortedUniqueRenames(values []Rename) []Rename {
	seen := make(map[string]struct{}, len(values))
	result := make([]Rename, 0, len(values))
	for _, value := range values {
		key := value.From + "\x00" + value.To
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].From != result[j].From {
			return result[i].From < result[j].From
		}
		return result[i].To < result[j].To
	})
	return result
}

type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}
