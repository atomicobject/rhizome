package actions

import (
	"context"
	"runtime"
	"sort"
	"strings"
	"sync"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// PropertySummary describes a vault property, note count, and inferred value details.
type PropertySummary struct {
	Name               string         `json:"name"`
	NoteCount          int            `json:"noteCount"`
	Shape              string         `json:"shape"`                // scalar, list, object, mixed, unknown
	ValueType          string         `json:"valueType"`            // bool, int, float, date, datetime, url, wikilink, string, object, mixed, unknown
	EnumValues         []string       `json:"enumValues,omitempty"` // enumerated values when small and suitable
	EnumValueCounts    map[string]int `json:"enumValueCounts,omitempty"`
	DistinctValueCount int            `json:"distinctValueCount"` // count of unique values encountered (lists flattened)
	TruncatedValueSet  bool           `json:"truncatedValueSet"`  // true if values exceeded maxValues cap
}

// PropertySource specifies which property sources to scan.
type PropertySource string

const (
	PropertySourceAll         PropertySource = "all"         // both frontmatter and inline
	PropertySourceFrontmatter PropertySource = "frontmatter" // YAML frontmatter only
	PropertySourceInline      PropertySource = "inline"      // dataview-style inline only (Key:: Value)
)

// PropertiesOptions controls scanning and enum detection behavior.
type PropertiesOptions struct {
	ExcludeTags           bool
	ValueLimit            int // max distinct values to emit as enum/value list
	MaxValues             int // cap stored values to avoid unbounded memory
	Notes                 []string
	Only                  []string
	ForceEnumMixed        bool
	Source                PropertySource // which property sources to scan (default: all)
	IncludeValueCounts    bool
	SessionStore          *semdb.Store
	MetadataStoreFallback MetadataStoreFallbackPolicy
}

type propertyCounts struct {
	noteCount   int
	shapes      map[string]int
	types       map[string]int
	values      map[string]struct{}
	valueCounts map[string]int
	overflow    bool
}

// Properties returns summaries for all frontmatter properties in the vault.
func Properties(vault obsidian.VaultManager, note obsidian.NoteReader, opts PropertiesOptions) ([]PropertySummary, error) {
	valueLimit := opts.ValueLimit
	if valueLimit <= 0 {
		valueLimit = 10
	}
	maxValues := opts.MaxValues
	if maxValues <= 0 {
		maxValues = 500
	}
	if maxValues < valueLimit+1 {
		maxValues = valueLimit + 1
	}

	vaultDef, err := vault.Definition()
	if err != nil {
		return nil, err
	}

	var scanNotes []string
	var filter map[string]struct{}
	var cachedEntries []cache.Entry

	if provider, ok := note.(interface {
		EntriesSnapshot(context.Context) ([]cache.Entry, error)
	}); ok {
		entries, err := provider.EntriesSnapshot(context.Background())
		if err != nil {
			return nil, err
		}
		cachedEntries = entries
	}

	if len(opts.Notes) > 0 {
		scanNotes = opts.Notes
		filter = make(map[string]struct{})
		for _, n := range scanNotes {
			filter[string(paths.NormalizeNotePath(n))] = struct{}{}
		}
	} else if cachedEntries == nil {
		allNotes, err := note.GetNotesList(vaultDef)
		if err != nil {
			return nil, err
		}
		scanNotes = allNotes
	}

	numWorkers := runtime.NumCPU()
	if len(scanNotes) < numWorkers {
		numWorkers = len(scanNotes)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	filterProps := make(map[string]struct{})
	for _, name := range opts.Only {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		filterProps[name] = struct{}{}
	}

	allowProperty := func(name string) bool {
		if len(filterProps) == 0 {
			return true
		}
		_, ok := filterProps[name]
		return ok
	}

	store := opts.SessionStore
	var cleanup func()
	if store == nil && opts.MetadataStoreFallback.allowsStoreOpen() {
		store, cleanup, err = openMetadataStore(vaultDef)
		if cleanup != nil {
			defer cleanup()
		}
	}
	if err == nil && metadataStoreReady(context.Background(), vaultDef, note, store) {
		if summaries, err := propertiesFromStore(context.Background(), store, opts, allowProperty, valueLimit, maxValues, filter); err == nil {
			return summaries, nil
		}
	}

	if cachedEntries != nil {
		return propertiesFromEntries(cachedEntries, opts, allowProperty, valueLimit, maxValues, filter), nil
	}
	facts := NoteFactsFromReader(note)

	batchSize := (len(scanNotes) + numWorkers - 1) / numWorkers
	results := make(chan map[string]*propertyCounts, numWorkers)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		start := i * batchSize
		end := start + batchSize
		if end > len(scanNotes) {
			end = len(scanNotes)
		}
		if start >= len(scanNotes) {
			continue
		}

		batch := scanNotes[start:end]
		wg.Add(1)
		go func(files []string) {
			defer wg.Done()
			local := make(map[string]*propertyCounts)
			for _, notePath := range files {
				fact, ok := facts.LookupFact(notePath)
				if !ok {
					continue
				}
				frontmatter, inline := fact.Frontmatter, fact.InlineProps
				if opts.Source == PropertySourceInline {
					frontmatter = nil
				}
				if opts.Source == PropertySourceFrontmatter {
					inline = nil
				}

				perNote := make(map[string]*propertyCounts)

				addPerNote := func(key string, info obsidian.PropertyValueInfo) {
					if key == "tags" && opts.ExcludeTags {
						return
					}
					if !allowProperty(key) {
						return
					}
					np, ok := perNote[key]
					if !ok {
						np = &propertyCounts{
							shapes:      make(map[string]int),
							types:       make(map[string]int),
							values:      make(map[string]struct{}),
							valueCounts: make(map[string]int),
						}
						perNote[key] = np
					}
					np.shapes[info.Shape]++
					np.types[info.ValueType]++
					for _, v := range info.Values {
						v = strings.TrimSpace(v)
						if v == "" {
							continue
						}
						if _, seen := np.values[v]; seen {
							continue
						}
						np.values[v] = struct{}{}
						np.valueCounts[v]++
					}
				}

				for key, raw := range frontmatter {
					if !allowProperty(key) {
						continue
					}
					info := obsidian.AnalyzePropertyValue(raw)
					addPerNote(key, info)
				}

				for key, values := range inline {
					if !allowProperty(key) {
						continue
					}
					info := obsidian.AnalyzePropertyValue(values)
					info.Shape = "scalar" // inline treated as scalar entries per line
					addPerNote(key, info)
				}

				for key, noteCounts := range perNote {
					pc, ok := local[key]
					if !ok {
						pc = &propertyCounts{
							shapes:      make(map[string]int),
							types:       make(map[string]int),
							values:      make(map[string]struct{}),
							valueCounts: make(map[string]int),
						}
						local[key] = pc
					}
					pc.noteCount++
					for s, c := range noteCounts.shapes {
						pc.shapes[s] += c
					}
					for t, c := range noteCounts.types {
						pc.types[t] += c
					}
					for v := range noteCounts.values {
						if key == "tags" {
							pc.values[v] = struct{}{}
							pc.valueCounts[v]++
							continue
						}
						if len(pc.values) < maxValues {
							pc.values[v] = struct{}{}
						} else {
							pc.overflow = true
							continue
						}
						pc.valueCounts[v]++
					}
				}
			}
			results <- local
		}(batch)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	merged := make(map[string]*propertyCounts)
	for res := range results {
		for key, pc := range res {
			target, ok := merged[key]
			if !ok {
				merged[key] = pc
				continue
			}
			target.noteCount += pc.noteCount
			for s, c := range pc.shapes {
				target.shapes[s] += c
			}
			for t, c := range pc.types {
				target.types[t] += c
			}
			for v := range pc.values {
				if key == "tags" {
					target.values[v] = struct{}{}
					continue
				}
				if len(target.values) < maxValues {
					target.values[v] = struct{}{}
				} else {
					target.overflow = true
					break
				}
			}
			if pc.overflow && key != "tags" {
				target.overflow = true
			}
			for v, c := range pc.valueCounts {
				target.valueCounts[v] += c
			}
		}
	}

	return summarizePropertyCounts(merged, opts, valueLimit), nil
}

func propertiesFromStore(ctx context.Context, store *semdb.Store, opts PropertiesOptions, allowProperty func(string) bool, valueLimit int, maxValues int, filter map[string]struct{}) ([]PropertySummary, error) {
	var notePaths []string
	if len(filter) > 0 {
		notePaths = make([]string, 0, len(filter))
		for path := range filter {
			notePaths = append(notePaths, path)
		}
		sort.Strings(notePaths)
	}

	source := semdb.NotePropertySource(0)
	switch opts.Source {
	case PropertySourceFrontmatter:
		source = semdb.NotePropertySourceFrontmatter
	case PropertySourceInline:
		source = semdb.NotePropertySourceInline
	}

	propRows, err := store.CurrentNotePropertyValues(ctx, notePaths, opts.Only, source)
	if err != nil {
		return nil, err
	}
	perNote := make(map[string]map[string]*propertyCounts)
	addPerNote := func(notePath, key string, shape, valueType string, values []string) {
		if key == "tags" && opts.ExcludeTags {
			return
		}
		if !allowProperty(key) {
			return
		}
		byProperty := perNote[notePath]
		if byProperty == nil {
			byProperty = make(map[string]*propertyCounts)
			perNote[notePath] = byProperty
		}
		pc := byProperty[key]
		if pc == nil {
			pc = &propertyCounts{
				shapes:      make(map[string]int),
				types:       make(map[string]int),
				values:      make(map[string]struct{}),
				valueCounts: make(map[string]int),
			}
			byProperty[key] = pc
		}
		pc.shapes[shape]++
		pc.types[valueType]++
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := pc.values[value]; ok {
				continue
			}
			pc.values[value] = struct{}{}
			pc.valueCounts[value]++
		}
	}

	for _, row := range propRows {
		shape := "scalar"
		if row.IsList {
			shape = "list"
		}
		addPerNote(row.NotePath, row.PropertyName, shape, propertyValueTypeFromRow(row), nonEmptyValues(row.ValueText))
	}
	merged := make(map[string]*propertyCounts)
	for _, byProperty := range perNote {
		for key, noteCounts := range byProperty {
			target := merged[key]
			if target == nil {
				target = &propertyCounts{
					shapes:      make(map[string]int),
					types:       make(map[string]int),
					values:      make(map[string]struct{}),
					valueCounts: make(map[string]int),
				}
				merged[key] = target
			}
			target.noteCount++
			for shape, count := range noteCounts.shapes {
				target.shapes[shape] += count
			}
			for kind, count := range noteCounts.types {
				target.types[kind] += count
			}
			for value := range noteCounts.values {
				if key == "tags" {
					target.values[value] = struct{}{}
					continue
				}
				if len(target.values) < maxValues {
					target.values[value] = struct{}{}
				} else {
					target.overflow = true
				}
			}
			for value, count := range noteCounts.valueCounts {
				target.valueCounts[value] += count
			}
		}
	}

	return summarizePropertyCounts(merged, opts, valueLimit), nil
}

func propertiesFromEntries(entries []cache.Entry, opts PropertiesOptions, allowProperty func(string) bool, valueLimit int, maxValues int, filter map[string]struct{}) []PropertySummary {
	merged := make(map[string]*propertyCounts)

	for _, entry := range entries {
		if filter != nil {
			if _, ok := filter[string(paths.NormalizeNotePath(entry.Path))]; !ok {
				continue
			}
		}

		perNote := make(map[string]*propertyCounts)

		addPerNote := func(key string, info obsidian.PropertyValueInfo) {
			if key == "tags" && opts.ExcludeTags {
				return
			}
			if !allowProperty(key) {
				return
			}
			np, ok := perNote[key]
			if !ok {
				np = &propertyCounts{
					shapes:      make(map[string]int),
					types:       make(map[string]int),
					values:      make(map[string]struct{}),
					valueCounts: make(map[string]int),
				}
				perNote[key] = np
			}
			np.shapes[info.Shape]++
			np.types[info.ValueType]++
			for _, v := range info.Values {
				v = strings.TrimSpace(v)
				if v == "" {
					continue
				}
				if _, seen := np.values[v]; seen {
					continue
				}
				np.values[v] = struct{}{}
				np.valueCounts[v]++
			}
		}

		if opts.Source != PropertySourceInline {
			for key, raw := range entry.Frontmatter {
				if !allowProperty(key) {
					continue
				}
				info := obsidian.AnalyzePropertyValue(raw)
				addPerNote(key, info)
			}
		}

		if opts.Source != PropertySourceFrontmatter {
			for key, values := range entry.InlineProps {
				if !allowProperty(key) {
					continue
				}
				info := obsidian.AnalyzePropertyValue(values)
				info.Shape = "scalar"
				addPerNote(key, info)
			}
		}

		for key, noteCounts := range perNote {
			target, ok := merged[key]
			if !ok {
				target = &propertyCounts{
					shapes:      make(map[string]int),
					types:       make(map[string]int),
					values:      make(map[string]struct{}),
					valueCounts: make(map[string]int),
				}
				merged[key] = target
			}

			target.noteCount++
			for s, c := range noteCounts.shapes {
				target.shapes[s] += c
			}
			for t, c := range noteCounts.types {
				target.types[t] += c
			}
			for v := range noteCounts.values {
				if key == "tags" {
					target.values[v] = struct{}{}
					continue
				}
				if len(target.values) < maxValues {
					target.values[v] = struct{}{}
				} else {
					target.overflow = true
					continue
				}
			}
			if noteCounts.overflow && key != "tags" {
				target.overflow = true
			}
			for v, c := range noteCounts.valueCounts {
				target.valueCounts[v] += c
			}
		}
	}

	return summarizePropertyCounts(merged, opts, valueLimit)
}

func pickOrMixed(counts map[string]int) string {
	if len(counts) == 0 {
		return "unknown"
	}
	if len(counts) == 1 {
		for k := range counts {
			return k
		}
	}
	return "mixed"
}

func isEnumCandidate(valueType string) bool {
	switch valueType {
	case "url", "datetime", "date", "object", "mixed", "unknown":
		return false
	default:
		return true
	}
}

func summarizePropertyCounts(merged map[string]*propertyCounts, opts PropertiesOptions, valueLimit int) []PropertySummary {
	summaries := make([]PropertySummary, 0, len(merged))
	for key, pc := range merged {
		shape := pickOrMixed(pc.shapes)
		valueType := pickOrMixed(pc.types)

		var enumValues []string
		shouldEnumerate := (isEnumCandidate(valueType) || (valueType == "mixed" && len(pc.values) <= valueLimit)) && len(pc.values) > 0 && len(pc.values) <= valueLimit && !pc.overflow
		if key == "tags" && len(pc.values) > 0 {
			shouldEnumerate = true
		}
		if shouldEnumerate {
			enumValues = make([]string, 0, len(pc.values))
			for value := range pc.values {
				enumValues = append(enumValues, value)
			}
			sort.Strings(enumValues)
		}

		var enumValueCounts map[string]int
		if shouldEnumerate && opts.IncludeValueCounts && len(pc.valueCounts) > 0 {
			enumValueCounts = make(map[string]int, len(pc.valueCounts))
			for value, count := range pc.valueCounts {
				enumValueCounts[value] = count
			}
		}

		summaries = append(summaries, PropertySummary{
			Name:               key,
			NoteCount:          pc.noteCount,
			Shape:              shape,
			ValueType:          valueType,
			EnumValues:         enumValues,
			EnumValueCounts:    enumValueCounts,
			DistinctValueCount: len(pc.values),
			TruncatedValueSet:  pc.overflow,
		})
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].NoteCount == summaries[j].NoteCount {
			return summaries[i].Name < summaries[j].Name
		}
		return summaries[i].NoteCount > summaries[j].NoteCount
	})
	return summaries
}

func propertyValueTypeFromRow(row semdb.NotePropertyValueRow) string {
	switch row.ValueKind {
	case semdb.NotePropertyValueBool:
		return "bool"
	case semdb.NotePropertyValueInt:
		return "int"
	case semdb.NotePropertyValueFloat:
		return "float"
	case semdb.NotePropertyValueDate:
		return "date"
	case semdb.NotePropertyValueDateTime:
		return "datetime"
	case semdb.NotePropertyValueURL:
		return "url"
	case semdb.NotePropertyValueWikilink:
		return "wikilink"
	case semdb.NotePropertyValueString:
		return "string"
	default:
		return "unknown"
	}
}

func nonEmptyValues(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}
