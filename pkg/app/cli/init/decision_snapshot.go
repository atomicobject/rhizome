package init

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// ErrDecisionsChanged rejects choices made against a different set of file versions.
var ErrDecisionsChanged = errors.New("file decisions changed since review; run rzm init --check --json again")

// decisionSnapshot covers all reviewable changes, including deferred choices.
// Group membership is canonical so traversal order cannot change the token.
func (p *preparedFiles) decisionSnapshot() string {
	type member struct {
		Key     string
		Path    string
		From    string
		Kind    fileState
		Removal bool
		Current string
		Desired string
		Copies  []string
	}
	groups := map[string][]string{}
	for _, c := range p.changes {
		if needsDecision(c) {
			groups[c.group] = append(groups[c.group], c.key)
		}
	}
	for _, copies := range groups {
		sort.Strings(copies)
	}
	members := []member{}
	for _, c := range p.changes {
		if needsDecision(c) {
			members = append(members, member{
				Key: c.key, Path: c.rel, From: c.from, Kind: c.state, Removal: c.removal,
				Current: snapshotHash(c.current), Desired: snapshotHash(c.desired), Copies: groups[c.group],
			})
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Key < members[j].Key })
	data, _ := json.Marshal(members)
	return snapshotHash(data)
}

func snapshotHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
