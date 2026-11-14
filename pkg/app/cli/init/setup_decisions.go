package init

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/textdiff"
)

// SetupDecision is a generated update that needs the user's choice.
// Path is the stable choice key; From is a legacy source being replaced.
type SetupDecision struct {
	Path   string   `json:"path"`
	Kind   string   `json:"kind"`
	From   string   `json:"from,omitempty"`
	Copies []string `json:"copies,omitempty"`
	Diff   string   `json:"diff,omitempty"`
}

func needsDecision(c *change) bool { return c.state == stateEdited || c.state == stateUnknown }

// A shared decision may replace only copies with the same reviewable change.
func splitDecisionGroups(changes []*change) {
	type variant struct {
		group, current, desired, from string
		removal                       bool
	}
	groups := map[variant]string{}
	for _, c := range changes {
		if !needsDecision(c) {
			continue
		}
		key := variant{c.group, string(c.current), string(c.desired), c.from, c.removal}
		group, ok := groups[key]
		if !ok {
			group = c.key
			groups[key] = group
		}
		c.group = group
	}
}

func (p *preparedFiles) decisions() []SetupDecision {
	out := []SetupDecision{}
	groups := map[string]int{}
	for _, c := range p.changes {
		if !needsDecision(c) {
			continue
		}
		if _, chosen := p.choices[c.group]; chosen {
			continue
		}
		if i, ok := groups[c.group]; ok {
			out[i].Copies = append(out[i].Copies, c.key)
			continue
		}
		kind := "edited"
		if c.state == stateUnknown {
			kind = "unknown"
		}
		if c.removal {
			kind = "remove"
		}
		diff, _, _ := textdiff.GenerateUnifiedDiff(string(c.current), string(c.desired), c.key)
		const maxDiff = 200 * 1024
		if len(diff) > maxDiff {
			diff = string([]rune(diff[:maxDiff])) + "\n[diff truncated]\n"
		}
		groups[c.group] = len(out)
		out = append(out, SetupDecision{Path: c.key, Kind: kind, From: c.from, Diff: diff})
	}
	return out
}

func (p *preparedFiles) setChoices(take, keep []string) error {
	choices := map[string]bool{}
	for _, list := range []struct {
		paths []string
		take  bool
	}{{take, true}, {keep, false}} {
		for _, input := range list.paths {
			groups := map[string]bool{}
			for _, c := range p.changes {
				if needsDecision(c) && (input == c.key || input == c.rel) {
					groups[c.group] = true
				}
			}
			if len(groups) != 1 {
				return fmt.Errorf("file decision %q is stale, unknown, or ambiguous; run rzm init --check again", input)
			}
			for group := range groups {
				if previous, exists := choices[group]; exists && previous != list.take {
					return fmt.Errorf("conflicting --take and --keep choices for %q", input)
				}
				choices[group] = list.take
			}
		}
	}
	p.choices = choices
	return nil
}

func (p *preparedFiles) resolve(ui ownershipUI) {
	unresolved := make([]*change, 0, len(p.changes))
	for _, c := range p.changes {
		if take, ok := p.choices[c.group]; ok && needsDecision(c) {
			c.take, c.asked = take, true
		} else {
			unresolved = append(unresolved, c)
		}
	}
	resolveChanges(unresolved, ui)
}

// Preflight all whole-file inputs before any generated or support file writes.
func (p *preparedFiles) preflight() error {
	byKey := map[string]*change{}
	for _, c := range p.changes {
		byKey[c.key] = c
	}
	for _, file := range p.plan.files {
		c := byKey[file.rel]
		if c == nil {
			continue
		}
		if file.blocked != nil && (c.take || p.choices[c.group]) {
			return fmt.Errorf("cannot replace %s with %s: %w; keep %s and move its identifier contracts by hand", c.from, c.rel, file.blocked, c.from)
		}
		rel := firstNonEmpty(c.from, c.rel)
		current, exists, err := readIfExists(filepath.Join(p.root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if exists != (c.state != stateMissing) || !bytes.Equal(current, c.current) {
			return fmt.Errorf("%s changed while setup was being prepared; retry setup", rel)
		}
		if c.from != "" {
			if _, exists, err := readIfExists(filepath.Join(p.root, filepath.FromSlash(c.rel))); err != nil {
				return err
			} else if exists {
				return fmt.Errorf("%s appeared while setup was being prepared; retry setup", c.rel)
			}
		}
	}
	for i, doc := range p.plan.docs {
		if err := checkDocUnchanged(p.root, doc, p.docStates[i]); err != nil {
			return err
		}
	}
	for _, removal := range p.plan.removals {
		c := byKey[removal.rel]
		if c == nil {
			continue
		}
		current, exists, err := readIfExists(filepath.Join(p.root, filepath.FromSlash(c.rel)))
		if err != nil {
			return err
		}
		if !exists || !bytes.Equal(current, c.current) {
			return fmt.Errorf("%s changed while setup was being prepared; retry setup", c.rel)
		}
	}
	return nil
}

func pendingSummary(decisions []SetupDecision) string {
	if len(decisions) == 1 {
		return "1 file decision still needs review."
	}
	return fmt.Sprintf("%d file decisions still need review.", len(decisions))
}

func (p *preparedFiles) remainingDecisions() ([]SetupDecision, error) {
	record, _ := loadGeneratedFiles(p.root)
	remaining, err := classifyFilePlan(p.root, p.plan, record)
	if err != nil {
		return nil, err
	}
	return remaining.decisions(), nil
}
