package notemeta

import (
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
)

// projectedNoteEntry keeps provider-owned source facts intact until the
// indexer adapts them into rows and resolved graph edges. Entry exists only for
// compatibility with the existing notemeta pipeline; consumers that need
// ranges, unresolved links, or precise metadata kinds must use the fact fields.
//
// WHY: notemeta owns the format-neutral translation boundary. Providers expose
// authored syntax but never choose candidates, apply vault policy, or persist
// rows.
type projectedNoteEntry struct {
	Entry noteEntry

	Source     noteformat.AuthoredSource
	Projection noteformat.Projection

	RootMetadata     []noteformat.RootMetadataFact
	InlineProperties []noteformat.InlinePropertyFact
	Tags             []noteformat.TagFact
	Aliases          []noteformat.AliasFact
	Links            []noteformat.UnresolvedAuthoredLinkFact
	FragmentTargets  []noteformat.FragmentTargetFact
}

// projectionEntryFrom adapts a source snapshot plus unresolved provider facts
// without parsing source bytes or branching on a provider identity.
func projectionEntryFrom(source noteformat.AuthoredSource, projection noteformat.Projection) (projectedNoteEntry, error) {
	if source.Path().String() == "" {
		return projectedNoteEntry{}, fmt.Errorf("authored source path is required")
	}
	if strings.TrimSpace(string(source.Format())) == "" {
		return projectedNoteEntry{}, fmt.Errorf("authored source format is required")
	}

	copy := projection.Copy()
	entry := noteEntry{
		Path:       source.Path().String(),
		Content:    string(source.Bytes()),
		Source:     source,
		Projection: copy,
		Mtime:      source.Mtime(),
		Size:       source.Size(),
	}
	if copy.Status == noteformat.ProjectionStatusFatal {
		// Fatal source identity must retain the same fallback title used by the
		// ownership transition. This keeps repeated identical fatal projections
		// idempotent after metadata publication.
		entry.Title = projectedTitle(source, nil)
		return projectedNoteEntry{Entry: entry, Source: source, Projection: copy}, nil
	}

	facts := copy.Facts
	entry.Title = projectedTitle(source, facts.Title)
	entry.Frontmatter = legacyFrontmatterFromFacts(facts.RootMetadata)
	entry.InlineProps = inlinePropertiesFromFacts(facts.InlineProperties)
	entry.Tags = tagValuesFromFacts(facts.Tags)
	entry.Aliases = aliasValuesFromFacts(facts.Aliases)
	entry.Links = append([]noteformat.UnresolvedAuthoredLinkFact(nil), facts.Links...)
	entry.FragmentTargets = append([]noteformat.FragmentTargetFact(nil), facts.FragmentTargets...)
	if len(entry.Aliases) > 0 {
		// Candidate matching historically reads aliases from the compatibility
		// frontmatter map. Alias facts remain separately available for the new
		// source-range-aware path.
		entry.Frontmatter["aliases"] = append([]string(nil), entry.Aliases...)
	}

	return projectedNoteEntry{
		Entry:            entry,
		Source:           source,
		Projection:       copy,
		RootMetadata:     append([]noteformat.RootMetadataFact(nil), facts.RootMetadata...),
		InlineProperties: append([]noteformat.InlinePropertyFact(nil), facts.InlineProperties...),
		Tags:             append([]noteformat.TagFact(nil), facts.Tags...),
		Aliases:          append([]noteformat.AliasFact(nil), facts.Aliases...),
		Links:            append([]noteformat.UnresolvedAuthoredLinkFact(nil), facts.Links...),
		FragmentTargets:  append([]noteformat.FragmentTargetFact(nil), facts.FragmentTargets...),
	}, nil
}

func projectedTitle(source noteformat.AuthoredSource, fact *noteformat.TitleFact) string {
	if fact != nil && strings.TrimSpace(fact.Value) != "" {
		return strings.TrimSpace(fact.Value)
	}
	base := path.Base(source.Path().String())
	return strings.TrimSuffix(base, path.Ext(base))
}

func legacyFrontmatterFromFacts(facts []noteformat.RootMetadataFact) map[string]any {
	out := make(map[string]any, len(facts))
	for _, fact := range facts {
		key := strings.TrimSpace(fact.Key)
		if key == "" {
			continue
		}
		// Projection facts are source-ordered. Last-write-wins maintains the
		// old map-shaped compatibility view without discarding the occurrence
		// list held by projectedNoteEntry.RootMetadata.
		out[key] = metadataValueLegacyExport(fact.Value)
	}
	return out
}

func metadataValueLegacyExport(value noteformat.MetadataValue) any {
	switch value.Kind() {
	case noteformat.MetadataNull:
		return nil
	case noteformat.MetadataBoolean, noteformat.MetadataString:
		return value.Export()
	case noteformat.MetadataInteger:
		number, _ := value.Export().(json.Number)
		parsed, err := number.Int64()
		if err != nil {
			return number
		}
		if strconv.IntSize == 64 || (parsed >= -1<<31 && parsed <= 1<<31-1) {
			return int(parsed)
		}
		return parsed
	case noteformat.MetadataFloat:
		number, _ := value.Export().(json.Number)
		parsed, err := number.Float64()
		if err != nil {
			return number
		}
		return parsed
	case noteformat.MetadataDate:
		text, _ := value.Export().(string)
		parsed, err := time.Parse(time.DateOnly, text)
		if err != nil {
			return text
		}
		return parsed
	case noteformat.MetadataTimestamp:
		text, _ := value.Export().(string)
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return text
		}
		return parsed
	case noteformat.MetadataArray:
		items := value.Array()
		out := make([]any, len(items))
		for index := range items {
			out[index] = metadataValueLegacyExport(items[index])
		}
		return out
	case noteformat.MetadataObject:
		entries := value.Object()
		out := make(map[string]any, len(entries))
		for _, entry := range entries {
			out[entry.Key] = metadataValueLegacyExport(entry.Value)
		}
		return out
	default:
		return value.Export()
	}
}

func inlinePropertiesFromFacts(facts []noteformat.InlinePropertyFact) map[string][]string {
	out := make(map[string][]string)
	for _, fact := range facts {
		key := strings.TrimSpace(fact.Key)
		if key == "" {
			continue
		}
		out[key] = append(out[key], fact.Value)
	}
	return out
}

func tagValuesFromFacts(facts []noteformat.TagFact) []string {
	values := make([]string, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		value := strings.TrimSpace(fact.Value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func aliasValuesFromFacts(facts []noteformat.AliasFact) []string {
	values := make([]string, 0, len(facts))
	for _, fact := range facts {
		if value := strings.TrimSpace(fact.Value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

// metadataValueRows maps a canonical provider value to the existing durable
// property-row vocabulary. It deliberately uses MetadataValue.Kind rather than
// reparsing its string export: date-only and RFC3339Nano timestamps remain
// distinct, and integers/floats retain their exact canonical spellings.
func metadataValueRows(notePath, propertyName string, source semdb.NotePropertySource, value noteformat.MetadataValue) []semdb.NotePropertyValueRow {
	propertyName = strings.ToLower(strings.TrimSpace(propertyName))
	if propertyName == "" {
		return nil
	}
	if value.Kind() != noteformat.MetadataArray {
		return []semdb.NotePropertyValueRow{metadataValueRow(notePath, propertyName, source, value, false, 0)}
	}

	items := value.Array()
	if len(items) == 0 {
		return []semdb.NotePropertyValueRow{{
			NotePath: notePath, PropertyName: propertyName, Source: source,
			ValueKind: semdb.NotePropertyValueUnknown, IsList: true,
		}}
	}
	rows := make([]semdb.NotePropertyValueRow, 0, len(items))
	for ordinal, item := range items {
		rows = append(rows, metadataValueRow(notePath, propertyName, source, item, true, ordinal))
	}
	return rows
}

func metadataValueRow(notePath, propertyName string, source semdb.NotePropertySource, value noteformat.MetadataValue, isList bool, ordinal int) semdb.NotePropertyValueRow {
	text, kind := metadataValueTextAndKind(value)
	return semdb.NotePropertyValueRow{
		NotePath:     notePath,
		PropertyName: propertyName,
		Source:       source,
		ValueText:    text,
		ValueNorm:    normalizePropertyValue(text),
		ValueKind:    kind,
		IsList:       isList,
		ListOrdinal:  ordinal,
	}
}

func metadataValueTextAndKind(value noteformat.MetadataValue) (string, semdb.NotePropertyValueKind) {
	switch value.Kind() {
	case noteformat.MetadataNull, noteformat.MetadataArray, noteformat.MetadataObject:
		return "", semdb.NotePropertyValueUnknown
	case noteformat.MetadataBoolean:
		return fmt.Sprint(value.Export()), semdb.NotePropertyValueBool
	case noteformat.MetadataInteger:
		return fmt.Sprint(value.Export()), semdb.NotePropertyValueInt
	case noteformat.MetadataFloat:
		return fmt.Sprint(value.Export()), semdb.NotePropertyValueFloat
	case noteformat.MetadataDate:
		return fmt.Sprint(value.Export()), semdb.NotePropertyValueDate
	case noteformat.MetadataTimestamp:
		return fmt.Sprint(value.Export()), semdb.NotePropertyValueDateTime
	case noteformat.MetadataString:
		return strings.TrimSpace(fmt.Sprint(value.Export())), semdb.NotePropertyValueString
	default:
		return "", semdb.NotePropertyValueUnknown
	}
}
