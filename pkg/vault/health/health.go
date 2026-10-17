package health

import (
	"fmt"
	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"regexp"

	"strings"

	"time"
)

// BrokenLinksOptions configures broken link detection behavior.
type BrokenLinksOptions struct {
	markdownsyntax.WikilinkOptions
	IncludeImages bool // If false (default), exclude links to image files
}

// NoteReadError preserves which note read failed while allowing callers to
// inspect the underlying error with errors.Is/errors.As.
type NoteReadError struct {
	Source string
	Target string
	Err    error
}

func (e *NoteReadError) Error() string {
	if e.Target != "" {
		return fmt.Sprintf("read note target %q linked from %q: %v", e.Target, e.Source, e.Err)
	}
	return fmt.Sprintf("read note source %q: %v", e.Source, e.Err)
}

func (e *NoteReadError) Unwrap() error { return e.Err }

// NoteReadErrors retains all note read failures from a scan. The scan can
// still return valid broken-link findings alongside this error.
type NoteReadErrors []*NoteReadError

func (e NoteReadErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	messages := make([]string, len(e))
	for i, readErr := range e {
		messages[i] = readErr.Error()
	}
	return strings.Join(messages, "; ")
}

func (e NoteReadErrors) Unwrap() []error {
	causes := make([]error, len(e))
	for i, readErr := range e {
		causes[i] = readErr
	}
	return causes
}

// DefaultBrokenLinksOptions provides standard options for broken link detection.
var DefaultBrokenLinksOptions = BrokenLinksOptions{
	WikilinkOptions: markdownsyntax.DefaultWikilinkOptions,
	IncludeImages:   false, // Exclude images by default
}

// BrokenLinkReason classifies why a wikilink is broken.
type BrokenLinkReason string

const (
	// BrokenLinkReasonNoteMissing means the target note does not exist.
	BrokenLinkReasonNoteMissing BrokenLinkReason = "note_missing"
	// BrokenLinkReasonBlockMissing means the target note exists but the
	// `^block-id` fragment does not resolve to any anchor in that note.
	BrokenLinkReasonBlockMissing BrokenLinkReason = "block_missing"
	// BrokenLinkReasonHeadingMissing means the target note exists but the
	// heading-text fragment does not match any heading in that note.
	BrokenLinkReasonHeadingMissing BrokenLinkReason = "heading_missing"
)

// BrokenLink represents a wikilink whose target cannot be fully resolved.
// Reason discriminates between missing notes (the original case) and
// fragment-level mismatches (block ID or heading text typos against an
// otherwise-resolvable note).
//
// WHY: SPEC-0023's orphan-block-id check needs to distinguish "this anchor
// has no inbound references" from "this anchor has a typo'd inbound
// reference"; the latter must be retained as a soft hold. Surfacing
// fragment-level breakage here is the substrate the orphan check consumes.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
type BrokenLink struct {
	Source   string                      `json:"source"`
	Target   string                      `json:"target"`
	LinkType markdownsyntax.BacklinkType `json:"linkType"`
	Alias    string                      `json:"alias,omitempty"`
	Fragment string                      `json:"fragment,omitempty"`
	Reason   BrokenLinkReason            `json:"reason,omitempty"`
	// Line and Raw locate the authored occurrence; Raw is the complete link
	// syntax so repairs can keep its display text.
	Line int    `json:"line,omitempty"`
	Raw  string `json:"raw,omitempty"`
	// Markdown marks a [text](path) link rather than a wikilink.
	Markdown bool `json:"markdown,omitempty"`
}

// DeadEndNote represents a note with inbound links but no outbound links.
type DeadEndNote struct {
	Path         string `json:"path"`
	InboundLinks int    `json:"inboundLinks"`
}

// StaleNote represents a note not modified within the threshold.
type StaleNote struct {
	Path                  string    `json:"path"`
	LastModified          time.Time `json:"lastModified"`
	DaysSinceModification int       `json:"daysSinceModification"`
}

// MergeSuggestion represents two notes that may be consolidation candidates.
type MergeSuggestion struct {
	Note1      string  `json:"note1"`
	Note2      string  `json:"note2"`
	Similarity float64 `json:"similarity"`
	Reason     string  `json:"reason"`
}

// HealthStats provides summary counts for vault health analysis.
type HealthStats struct {
	TotalNotes                int `json:"totalNotes"`
	BrokenLinkCount           int `json:"brokenLinkCount"`
	StaleNoteCount            int `json:"staleNoteCount"`
	DeadEndCount              int `json:"deadEndCount"`
	MergeSuggestionCount      int `json:"mergeSuggestionCount"`
	OntologyIssueCount        int `json:"ontologyIssueCount,omitempty"`
	SurprisingConnectionCount int `json:"surprisingConnectionCount,omitempty"`
}

// SurprisingConnectionEntry is a single surprising graph edge in the health report.
type SurprisingConnectionEntry struct {
	SrcPath    string   `json:"srcPath"`
	DstPath    string   `json:"dstPath"`
	EdgeKind   string   `json:"edgeKind"`
	Score      float64  `json:"score"`
	Confidence string   `json:"confidence"`
	Reasons    []string `json:"reasons,omitempty"`
}

// HealthReport aggregates all vault health findings.
type HealthReport struct {
	Vault                 string                      `json:"vault"`
	VaultPath             string                      `json:"vaultPath"`
	AnalyzedAt            time.Time                   `json:"analyzedAt"`
	Stats                 HealthStats                 `json:"stats"`
	BrokenLinks           []BrokenLink                `json:"brokenLinks"`
	StaleNotes            []StaleNote                 `json:"staleNotes"`
	DeadEnds              []DeadEndNote               `json:"deadEnds"`
	SuggestedMerges       []MergeSuggestion           `json:"suggestedMerges"`
	OntologyIssues        []ontology.ValidationIssue  `json:"ontologyIssues,omitempty"`
	SurprisingConnections []SurprisingConnectionEntry `json:"surprisingConnections,omitempty"`
}

// punctuationRegex matches punctuation characters for removal during normalization.
var punctuationRegex = regexp.MustCompile(`[^\w\s]`)

// whitespaceRegex matches multiple whitespace characters for collapsing.
var whitespaceRegex = regexp.MustCompile(`\s+`)

// NormalizeName converts a note name to a normalized form for comparison:
// - Converts to lowercase
// - Removes punctuation
// - Collapses multiple whitespace to single space
// - Trims leading/trailing whitespace
func NormalizeName(name string) string {
	// Remove .md extension if present
	name = obsidian.RemoveMdSuffix(name)

	// Convert to lowercase
	name = strings.ToLower(name)

	// Remove punctuation
	name = punctuationRegex.ReplaceAllString(name, "")

	// Collapse whitespace
	name = whitespaceRegex.ReplaceAllString(name, " ")

	// Trim
	name = strings.TrimSpace(name)

	return name
}

// FindDeadEnds identifies notes with inbound links but no outbound links.
// Self-links are excluded from the outbound count.
func FindDeadEnds(analysis *vaultgraph.GraphAnalysis) []DeadEndNote {
	var deadEnds []DeadEndNote

	for path, node := range analysis.Nodes {
		if node.Outbound == 0 && node.Inbound > 0 {
			deadEnds = append(deadEnds, DeadEndNote{
				Path:         path,
				InboundLinks: node.Inbound,
			})
		}
	}

	return deadEnds
}

// FindStaleNotes returns notes that haven't been modified within the threshold.
func FindStaleNotes(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, thresholdDays int) ([]StaleNote, error) {
	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	threshold := now.AddDate(0, 0, -thresholdDays)
	var staleNotes []StaleNote

	for _, notePath := range allNotes {
		modTime, err := note.GetModTime(vaultDef, notePath)
		if err != nil {
			// Skip files we can't stat
			continue
		}

		if modTime.Before(threshold) {
			daysSince := int(now.Sub(modTime).Hours() / 24)
			staleNotes = append(staleNotes, StaleNote{
				Path:                  notePath,
				LastModified:          modTime,
				DaysSinceModification: daysSince,
			})
		}
	}

	return staleNotes, nil
}

// FindMergeSuggestions identifies notes with matching normalized names.
func FindMergeSuggestions(allNotes []string) []MergeSuggestion {
	// Build a map of normalized names to original paths
	normMap := make(map[string][]string)
	for _, notePath := range allNotes {
		// Get just the filename without directory
		baseName := strings.TrimSuffix(notePath, ".md")
		fileName := baseName
		if idx := strings.LastIndex(baseName, "/"); idx >= 0 {
			fileName = baseName[idx+1:]
		}
		normalized := NormalizeName(fileName)
		normMap[normalized] = append(normMap[normalized], notePath)
	}

	var suggestions []MergeSuggestion
	// Find all groups with more than one note
	for _, paths := range normMap {
		if len(paths) > 1 {
			// Generate pairs
			for i := 0; i < len(paths); i++ {
				for j := i + 1; j < len(paths); j++ {
					suggestions = append(suggestions, MergeSuggestion{
						Note1:      paths[i],
						Note2:      paths[j],
						Similarity: 1.0,
						Reason:     "normalized name match",
					})
				}
			}
		}
	}

	return suggestions
}
