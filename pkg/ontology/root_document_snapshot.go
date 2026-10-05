package ontology

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// SourceRepresentation describes the authored bytes exposed by a root
// snapshot. A valid UTF-8 snapshot preserves those bytes exactly.
type SourceRepresentation string

const SourceRepresentationUTF8 SourceRepresentation = "source_utf8"

// EvidenceRepresentation describes the format-owned projection carried with
// a root snapshot. It is intentionally distinct from authored source.
type EvidenceRepresentation string

const EvidenceRepresentationProviderProjection EvidenceRepresentation = "provider_projection"

// RootDocumentSnapshot is the provider-neutral source boundary for one note
// root. MarkdownStructure is present only when the selected provider is
// Markdown; root-only formats never acquire fabricated Markdown structure.
type RootDocumentSnapshot struct {
	NotePath               paths.NotePath
	Format                 noteformat.FormatID
	RawSource              []byte
	ContentHash            string
	Mtime                  int64
	SourceRepresentation   SourceRepresentation
	EvidenceRepresentation EvidenceRepresentation
	Projection             noteformat.Projection
	Title                  string
	Metadata               map[string]any
	InlineProperties       map[string][]string
	Tags                   []string
	Links                  []notemeta.ResolvedNoteLink
	Root                   NodeRef
	RootMetadata           []noteformat.RootMetadataFact
	SearchRegions          []noteformat.SearchRegionFact
	Diagnostics            []noteformat.Diagnostic
	MarkdownStructure      *DocumentSnapshot
}

// BuildRootDocumentSnapshot seals a provider-current source snapshot for root
// projection. Structural state is attached only by an explicit Markdown
// syntax boundary; provider-neutral consumers never select a concrete parser.
func BuildRootDocumentSnapshot(source notemeta.NoteSourceSnapshot) (*RootDocumentSnapshot, error) {
	path, err := paths.CleanNotePath(source.Path.String())
	if err != nil {
		return nil, fmt.Errorf("build root document snapshot: %w", err)
	}
	if source.Format == "" {
		return nil, fmt.Errorf("build root document snapshot: format is required")
	}
	raw := append([]byte(nil), source.RawSource...)
	if raw == nil {
		raw = []byte(source.Content)
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("build root document snapshot: source is not valid UTF-8")
	}
	digest := sha256.Sum256(raw)
	contentHash := source.ContentHash
	if contentHash == "" {
		contentHash = hex.EncodeToString(digest[:])
	}
	snapshot := &RootDocumentSnapshot{
		NotePath: path, Format: source.Format, RawSource: raw,
		ContentHash: contentHash, Mtime: source.Mtime,
		SourceRepresentation:   SourceRepresentationUTF8,
		EvidenceRepresentation: EvidenceRepresentationProviderProjection,
		Projection:             source.Projection.Copy(),
		Title:                  strings.TrimSpace(source.Title),
		Metadata:               cloneAnyMap(source.Frontmatter),
		InlineProperties:       cloneNoteInline(source.InlineProps),
		Tags:                   append([]string(nil), source.Tags...),
		Links:                  append([]notemeta.ResolvedNoteLink(nil), source.Links...),
		Root:                   NodeRef{NotePath: path.String(), Kind: NodeKindNote},
	}
	snapshot.RootMetadata = append([]noteformat.RootMetadataFact(nil), snapshot.Projection.Facts.RootMetadata...)
	snapshot.SearchRegions = append([]noteformat.SearchRegionFact(nil), snapshot.Projection.Facts.SearchRegions...)
	snapshot.Diagnostics = append([]noteformat.Diagnostic(nil), snapshot.Projection.Diagnostics...)
	if snapshot.Title == "" {
		snapshot.Title = titleFromNotePath(path.String())
	}
	return snapshot, nil
}
