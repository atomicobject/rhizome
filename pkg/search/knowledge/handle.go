// Docs: [Search (Hub)](docs/hubs/Search (Hub).md)
package knowledge

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// Kind identifies a canonical entity type for unified search.
// Kinds are stable, lowercase identifiers (e.g. "notechunk").
type Kind string

const (
	KindNote      Kind = "note"
	KindNoteChunk Kind = "notechunk"
	KindNodeChunk Kind = "nodechunk"
	KindAnchor    Kind = "anchor"
	KindCodeChunk Kind = "codechunk"
	KindFile      Kind = "file"
	KindConcept   Kind = "concept"
)

// Handle is a stable, string-addressable reference to an entity in the unified search system.
//
// Handles serve as the primary identifier throughout the search pipeline: retrievers produce
// candidates with handles, rankers merge evidence by handle, and packers render results
// using handle metadata. The Owner() method enables diversity limiting (MaxPerOwner) by
// grouping chunk handles under their owning entity.
//
// Canonical format:
//
//	kind:id[#fragment...]
//
// Examples:
//
//	note:notes/Cache (Hub).md
//	notechunk:notes/Cache (Hub).md#3
//	anchor:anchor-123
//	codechunk:anchor-123#symbol#0
//	file:pkg/app/agentapi/agentapi.go
//
// Fragments are used for chunk handles: notechunk uses a single numeric fragment (chunk index),
// while codechunk uses granularity (e.g., "symbol") followed by chunk index.
//
// See [Search (Hub)](docs/hubs/Search (Hub).md) for how handles flow through retrieval → ranking → packing.
type Handle struct {
	Kind      Kind
	ID        string
	Fragments []string
}

// Owner returns the owning entity for chunk-like handles, enabling diversity limiting.
//
// For chunk handles (KindNoteChunk, KindNodeChunk, KindCodeChunk), Owner returns the parent entity:
// - notechunk → note (via NoteHandle)
// - nodechunk → note (via first fragment, if present)
// - codechunk → anchor (via AnchorHandle)
//
// For non-chunk handles, Owner returns the handle itself. This allows rankers to enforce
// MaxPerOwner limits: multiple chunks from the same note/anchor are grouped together.
func (h Handle) Owner() Handle {
	switch h.Kind {
	case KindNoteChunk:
		return NoteHandle(h.ID)
	case KindNodeChunk:
		if len(h.Fragments) > 0 && strings.TrimSpace(h.Fragments[0]) != "" {
			return NoteHandle(h.Fragments[0])
		}
		return h
	case KindCodeChunk:
		return AnchorHandle(h.ID)
	default:
		return h
	}
}

// String returns the canonical string representation of the handle.
// Returns an empty string if Kind or ID is empty (invalid handle).
func (h Handle) String() string {
	if h.Kind == "" || h.ID == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(string(h.Kind))
	b.WriteByte(':')
	b.WriteString(h.ID)
	for _, frag := range h.Fragments {
		if frag == "" {
			continue
		}
		b.WriteByte('#')
		b.WriteString(frag)
	}
	return b.String()
}

// ParseHandle parses a handle string into a Handle struct.
//
// Supports canonical format (kind:id#fragment...).
//
// Validation:
// - Kind must be lowercase, start with a letter, and contain only lowercase letters/digits
// - ID must be non-empty after trimming
// - Fragments are split on '#' and empty fragments are ignored
//
// Returns an error if the handle format is invalid or missing required components.
func ParseHandle(raw string) (Handle, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Handle{}, fmt.Errorf("empty handle")
	}

	kindPart, rest, ok := strings.Cut(raw, ":")
	if !ok {
		return Handle{}, fmt.Errorf("invalid handle %q: missing ':'", raw)
	}
	kind := Kind(strings.ToLower(strings.TrimSpace(kindPart)))
	if err := validateKind(kind); err != nil {
		return Handle{}, err
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return Handle{}, fmt.Errorf("invalid handle %q: missing id", raw)
	}

	id, fragStr, _ := strings.Cut(rest, "#")
	id = strings.TrimSpace(id)
	if id == "" {
		return Handle{}, fmt.Errorf("invalid handle %q: missing id", raw)
	}

	h := Handle{Kind: kind, ID: id}
	if kind == KindFile {
		rel, err := paths.CleanRelPath(id)
		if err != nil {
			return Handle{}, fmt.Errorf("invalid file handle %q: %w", raw, err)
		}
		h.ID = rel.String()
	}
	if fragStr != "" {
		for _, f := range strings.Split(fragStr, "#") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			h.Fragments = append(h.Fragments, f)
		}
	}
	return h, nil
}

// NoteHandle constructs a handle for a markdown note.
// The noteID should be the normalized note path (typically with .md suffix).
func NoteHandle(noteID string) Handle {
	return Handle{Kind: KindNote, ID: noteID}
}

// NoteChunkHandle constructs a handle for a specific chunk within a note.
// chunkIndex is the zero-based index of the chunk within the note's chunked content.
func NoteChunkHandle(noteID string, chunkIndex int) Handle {
	return Handle{Kind: KindNoteChunk, ID: noteID, Fragments: []string{strconv.Itoa(chunkIndex)}}
}

// NodeChunkHandle constructs a handle for a chunk owned by an ontology NodeRef.
// noteID is stored as the first fragment so diversity limiting can group embedded
// nodes under their owning note without parsing the NodeRef payload.
func NodeChunkHandle(nodeID, noteID, granularity string, chunkIndex int) Handle {
	fragments := []string{}
	if strings.TrimSpace(noteID) != "" {
		fragments = append(fragments, strings.TrimSpace(noteID))
	}
	if strings.TrimSpace(granularity) != "" {
		fragments = append(fragments, strings.TrimSpace(granularity))
	}
	fragments = append(fragments, strconv.Itoa(chunkIndex))
	return Handle{Kind: KindNodeChunk, ID: nodeID, Fragments: fragments}
}

// AnchorHandle constructs a handle for a code anchor (symbol, function, etc.).
// anchorID is the stable identifier from the intel store (typically a hash-based ID).
func AnchorHandle(anchorID string) Handle {
	return Handle{Kind: KindAnchor, ID: anchorID}
}

// CodeChunkHandle constructs a handle for a specific chunk within a code anchor.
// granularity indicates the chunking granularity (e.g., "symbol", "file", "function").
// chunkIndex is the zero-based index of the chunk within that granularity level.
func CodeChunkHandle(anchorID, granularity string, chunkIndex int) Handle {
	fragments := []string{}
	if strings.TrimSpace(granularity) != "" {
		fragments = append(fragments, strings.TrimSpace(granularity))
	}
	fragments = append(fragments, strconv.Itoa(chunkIndex))
	return Handle{Kind: KindCodeChunk, ID: anchorID, Fragments: fragments}
}

// FileHandle constructs a handle for a code file.
// path must be a normalized vault-relative path.
func FileHandle(path string) Handle {
	rel, err := paths.CleanRelPath(path)
	if err != nil {
		panic(fmt.Errorf("invalid file handle path %q: %w", path, err))
	}
	if rel == "" {
		panic(fmt.Errorf("invalid file handle path: empty"))
	}
	return Handle{Kind: KindFile, ID: rel.String()}
}

// validateKind ensures a kind string follows the canonical format:
// - Must start with a lowercase letter
// - Remaining characters must be lowercase letters or digits
// This ensures handles are stable, parseable identifiers.
func validateKind(kind Kind) error {
	if kind == "" {
		return fmt.Errorf("invalid handle kind: empty")
	}
	for i, r := range kind {
		if i == 0 {
			if !unicode.IsLetter(r) || !unicode.IsLower(r) {
				return fmt.Errorf("invalid handle kind %q", kind)
			}
			continue
		}
		if !(unicode.IsLower(r) || unicode.IsDigit(r)) {
			return fmt.Errorf("invalid handle kind %q", kind)
		}
	}
	return nil
}
