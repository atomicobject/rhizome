package notemeta

import (
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func projectionNoteRow(entry projectedNoteEntry, indexedAt int64) semdb.NoteMetadataRow {
	row := semdb.NoteMetadataRow{
		Path:        entry.Source.Path().String(),
		Title:       entry.Entry.Title,
		ContentHash: entry.Source.ContentHash(),
		Mtime:       entry.Source.Mtime(),
		Size:        entry.Source.Size(),
		IndexedAt:   indexedAt,
		FormatID:    string(entry.Source.Format()),
	}
	row.Projection = projectionState(entry.Source, entry.Projection, indexedAt)
	return row
}

func projectionState(source noteformat.AuthoredSource, projection noteformat.Projection, indexedAt int64) semdb.NoteProjectionState {
	state := semdb.NoteProjectionState{
		ProviderVersion:   projection.ProviderVersion,
		ProjectionVersion: projection.ProjectionVersion,
		SourceContentHash: source.ContentHash(),
		UpdatedAt:         indexedAt,
	}
	switch projection.Status {
	case noteformat.ProjectionStatusCurrent:
		state.Status = semdb.NoteProjectionStatusCurrent
	case noteformat.ProjectionStatusFatal:
		state.Status = semdb.NoteProjectionStatusFatal
		for _, diagnostic := range orderedProjectionDiagnostics(projection.Diagnostics) {
			if diagnostic.Blocking {
				state.DiagnosticCode = diagnostic.Code
				state.DiagnosticDetail = diagnostic.Message
				break
			}
		}
	default:
		state.Status = semdb.NoteProjectionStatusStale
		state.ProviderVersion = ""
		state.ProjectionVersion = ""
		state.SourceContentHash = ""
	}
	return state
}

func projectionPropertyRows(entry projectedNoteEntry) []semdb.NotePropertyValueRow {
	rows := make([]semdb.NotePropertyValueRow, 0, len(entry.RootMetadata)+len(entry.InlineProperties))
	hasAliasMetadata := false
	for _, fact := range entry.RootMetadata {
		rows = append(rows, metadataValueRows(entry.Source.Path().String(), fact.Key, semdb.NotePropertySourceFrontmatter, fact.Value)...)
		if strings.EqualFold(strings.TrimSpace(fact.Key), "aliases") {
			hasAliasMetadata = true
		}
	}
	// Alias facts are canonical projection evidence in their own right. Most
	// Markdown projections also expose aliases as root metadata, but a provider
	// need not model aliases as a metadata key for shared candidate matching.
	if !hasAliasMetadata {
		aliases := projectionAliasValues(entry)
		for ordinal, alias := range aliases {
			rows = append(rows, semdb.NotePropertyValueRow{
				NotePath: entry.Source.Path().String(), PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter,
				ValueText: alias, ValueNorm: normalizePropertyValue(alias), ValueKind: semdb.NotePropertyValueString,
				IsList: len(aliases) > 1, ListOrdinal: ordinal,
			})
		}
	}
	for key, values := range inlinePropertiesFromFacts(entry.InlineProperties) {
		rows = append(rows, inlineRows(entry.Source.Path().String(), key, values)...)
	}
	return rows
}

func projectionTagRows(entry projectedNoteEntry) []semdb.NoteTagRow {
	values := make([]string, 0, len(entry.Tags))
	for _, tag := range entry.Tags {
		values = append(values, tag.Value)
	}
	values = dedupeNormalizedTags(values)
	rows := make([]semdb.NoteTagRow, 0, len(values))
	for _, value := range values {
		rows = append(rows, semdb.NoteTagRow{NotePath: entry.Source.Path().String(), TagNorm: value})
	}
	return rows
}

func projectionTargetRows(entry projectedNoteEntry) []semdb.NoteFragmentTargetRow {
	rows := make([]semdb.NoteFragmentTargetRow, 0, len(entry.FragmentTargets))
	for _, target := range entry.FragmentTargets {
		kind := semdb.NoteFragmentTargetKind(target.Kind)
		if kind != semdb.NoteFragmentTargetHeading && kind != semdb.NoteFragmentTargetBlock &&
			kind != semdb.NoteFragmentTargetElementID && kind != semdb.NoteFragmentTargetLegacyName {
			continue
		}
		rows = append(rows, semdb.NoteFragmentTargetRow{
			NotePath: entry.Source.Path().String(), Kind: kind, Target: target.Text,
			TargetNorm: target.NormalizedText, Ordinal: target.Ordinal,
		})
	}
	return rows
}

func projectionDiagnosticRows(entry projectedNoteEntry) []semdb.NoteProjectionDiagnosticRow {
	diagnostics := orderedProjectionDiagnostics(entry.Projection.Diagnostics)
	rows := make([]semdb.NoteProjectionDiagnosticRow, 0, len(diagnostics))
	for ordinal, diagnostic := range diagnostics {
		category := diagnostic.Category
		if category == "" {
			category = noteformat.DiagnosticCategoryProjection
		}
		operation := diagnostic.AffectedOperation
		if operation == "" {
			operation = noteformat.DiagnosticOperationProjection
		}
		row := semdb.NoteProjectionDiagnosticRow{
			NotePath: entry.Source.Path().String(), Ordinal: ordinal, Code: diagnostic.Code,
			Category: string(category), Message: diagnostic.Message, Blocking: diagnostic.Blocking,
			AffectedOperation: string(operation), RangePresent: diagnostic.Range.Present,
		}
		if diagnostic.Range.Present {
			row.StartByte = diagnostic.Range.Range.StartByte
			row.EndByte = diagnostic.Range.Range.EndByte
		}
		rows = append(rows, row)
	}
	return rows
}

func orderedProjectionDiagnostics(diagnostics []noteformat.Diagnostic) []noteformat.Diagnostic {
	type ordered struct {
		diagnostic noteformat.Diagnostic
		occurrence int
	}
	values := make([]ordered, len(diagnostics))
	for index, diagnostic := range diagnostics {
		values[index] = ordered{diagnostic: diagnostic, occurrence: index}
	}
	sort.SliceStable(values, func(left, right int) bool {
		l, r := values[left], values[right]
		if l.diagnostic.Range.Present != r.diagnostic.Range.Present {
			return l.diagnostic.Range.Present
		}
		if l.diagnostic.Range.Present && l.diagnostic.Range.Range.StartByte != r.diagnostic.Range.Range.StartByte {
			return l.diagnostic.Range.Range.StartByte < r.diagnostic.Range.Range.StartByte
		}
		if l.diagnostic.Code != r.diagnostic.Code {
			return l.diagnostic.Code < r.diagnostic.Code
		}
		return l.occurrence < r.occurrence
	})
	out := make([]noteformat.Diagnostic, len(values))
	for index := range values {
		out[index] = values[index].diagnostic
	}
	return out
}

func projectionSearchRegionRows(entry projectedNoteEntry) []semdb.NoteSearchRegionRow {
	regions := entry.Projection.Facts.SearchRegions
	rows := make([]semdb.NoteSearchRegionRow, 0, len(regions))
	for ordinal, region := range regions {
		row := semdb.NoteSearchRegionRow{
			NotePath: entry.Source.Path().String(), Ordinal: ordinal, Origin: string(region.Origin),
			Kind: string(region.Kind), Text: region.Text, MediaType: region.MediaType, RangePresent: region.Range.Present,
		}
		if region.Range.Present {
			row.StartByte = region.Range.Range.StartByte
			row.EndByte = region.Range.Range.EndByte
		}
		rows = append(rows, row)
	}
	return rows
}

func projectionAliases(entries []projectedNoteEntry) map[string][]string {
	aliases := make(map[string][]string, len(entries))
	for _, entry := range entries {
		if values := projectionAliasValues(entry); len(values) > 0 {
			aliases[entry.Source.Path().String()] = values
		}
	}
	return aliases
}

func projectionLinkRows(vaultDef obsidian.VaultDefinition, cache *obsidian.NotePathCache, entry projectedNoteEntry) []semdb.GraphDocEdgeRow {
	if cache == nil {
		return nil
	}
	rows := make([]semdb.GraphDocEdgeRow, 0, len(entry.Links)*2)
	seen := make(map[string]struct{}, len(entry.Links)*2)
	for _, link := range entry.Links {
		if !resolutionSupported(vaultDef, link.Resolution) {
			continue
		}
		target, ok := resolveProjectionLink(cache, entry.Source.Path().String(), entry.Projection.Facts.DocumentBase, link)
		if !ok || target == entry.Source.Path().String() {
			continue
		}
		for _, kind := range projectionEdgeKinds(link) {
			key := entry.Source.Path().String() + "\x00" + target + "\x00" + kind
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			confidence, score := semdb.GraphDocEdgeConfidenceDefaults(kind)
			rows = append(rows, semdb.GraphDocEdgeRow{SrcPath: entry.Source.Path().String(), DstPath: target, Kind: kind, Confidence: confidence, ConfidenceScore: score})
		}
	}
	return rows
}

func resolutionSupported(vaultDef obsidian.VaultDefinition, resolution noteformat.LinkResolution) bool {
	switch resolution {
	case noteformat.LinkResolutionNoteReference:
		return vaultDef.SupportsWikilinks()
	case noteformat.LinkResolutionRelativePath:
		return vaultDef.SupportsMarkdownLinks()
	case noteformat.LinkResolutionURI:
		return true
	default:
		return false
	}
}

func resolveProjectionLink(cache *obsidian.NotePathCache, fromPath string, base *noteformat.DocumentBaseFact, link noteformat.UnresolvedAuthoredLinkFact) (string, bool) {
	return ResolveProjectedLink(cache, fromPath, base, link)
}

func projectionEdgeKinds(link noteformat.UnresolvedAuthoredLinkFact) []string {
	var coarse string
	switch link.Resolution {
	case noteformat.LinkResolutionNoteReference:
		coarse = semdb.GraphDocEdgeKindWikilink
	case noteformat.LinkResolutionRelativePath:
		coarse = semdb.GraphDocEdgeKindMarkdownLink
	case noteformat.LinkResolutionURI:
		subtype := strings.TrimSpace(string(link.Subtype))
		if subtype == "" {
			subtype = "basic"
		}
		return []string{semdb.NoteLinkKind("uri", subtype)}
	default:
		return nil
	}
	detail := semdb.NoteLinkKind(coarse, string(link.Subtype))
	if detail == "" {
		return []string{coarse}
	}
	return []string{coarse, detail}
}
