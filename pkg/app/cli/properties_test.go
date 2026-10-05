package actions

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestProperties(t *testing.T) {
	notes := map[string]string{
		"alpha.md": `---
office: AOGR
reviewed: true
links: ["[[Alpha]]", "[[Beta|B]]"]
url: https://example.com
count: 5
---`,
		"beta.md": `---
office: AORD
reviewed: false
links:
  - "[[Alpha]]"
  - "[[Gamma]]"
count: 7
date: 2024-05-02
---`,
		"gamma.md": `---
office: AOGR
note: some text
---`,
	}

	vaultManager := &mocks.VaultManager{}
	noteManager := &mocks.NoteReader{}

	vaultManager.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)

	var notesList []string
	for notePath := range notes {
		notesList = append(notesList, notePath)
	}
	noteManager.On("GetNotesList", mock.Anything).Return(notesList, nil)
	for path, content := range notes {
		noteManager.On("GetContents", mock.Anything, path).Return(content, nil)
	}

	result, err := Properties(vaultManager, newFactReader(noteManager, notes), PropertiesOptions{
		ExcludeTags: false,
		ValueLimit:  5,
		MaxValues:   50,
		Notes:       []string{"alpha.md", "beta.md", "gamma.md"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []PropertySummary{
		{
			Name:               "office",
			NoteCount:          3,
			Shape:              "scalar",
			ValueType:          "string",
			EnumValues:         []string{"AOGR", "AORD"},
			DistinctValueCount: 2,
		},
		{
			Name:               "count",
			NoteCount:          2,
			Shape:              "scalar",
			ValueType:          "int",
			EnumValues:         []string{"5", "7"},
			DistinctValueCount: 2,
		},
		{
			Name:               "links",
			NoteCount:          2,
			Shape:              "list",
			ValueType:          "wikilink",
			EnumValues:         []string{"[[Alpha]]", "[[Beta|B]]", "[[Gamma]]"},
			DistinctValueCount: 3,
		},
		{
			Name:               "reviewed",
			NoteCount:          2,
			Shape:              "scalar",
			ValueType:          "bool",
			EnumValues:         []string{"false", "true"},
			DistinctValueCount: 2,
		},
		{
			Name:               "date",
			NoteCount:          1,
			Shape:              "scalar",
			ValueType:          "date",
			EnumValues:         nil,
			DistinctValueCount: 1,
		},
		{
			Name:               "note",
			NoteCount:          1,
			Shape:              "scalar",
			ValueType:          "string",
			EnumValues:         []string{"some text"},
			DistinctValueCount: 1,
		},
		{
			Name:               "url",
			NoteCount:          1,
			Shape:              "scalar",
			ValueType:          "url",
			EnumValues:         nil,
			DistinctValueCount: 1,
		},
	}

	if len(result) != len(expected) {
		t.Fatalf("expected %d summaries, got %d", len(expected), len(result))
	}

	for i, got := range result {
		want := expected[i]
		if got.Name != want.Name || got.NoteCount != want.NoteCount || got.Shape != want.Shape || got.ValueType != want.ValueType {
			t.Fatalf("summary %d mismatch: got %+v want %+v", i, got, want)
		}
		if !reflect.DeepEqual(got.EnumValues, want.EnumValues) {
			t.Fatalf("enum values mismatch for %s: got %v want %v", got.Name, got.EnumValues, want.EnumValues)
		}
		if got.DistinctValueCount != want.DistinctValueCount {
			t.Fatalf("distinct count mismatch for %s: got %d want %d", got.Name, got.DistinctValueCount, want.DistinctValueCount)
		}
	}
}

func TestPropertiesExcludeTags(t *testing.T) {
	notes := map[string]string{
		"note.md": `---
tags: [project, work]
custom: yes
---`,
	}

	vaultManager := &mocks.VaultManager{}
	noteManager := &mocks.NoteReader{}

	vaultManager.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)
	noteManager.On("GetNotesList", mock.Anything).Return([]string{"note.md"}, nil)
	noteManager.On("GetContents", mock.Anything, "note.md").Return(notes["note.md"], nil)

	result, err := Properties(vaultManager, newFactReader(noteManager, notes), PropertiesOptions{
		ExcludeTags: true,
		ValueLimit:  10,
		MaxValues:   10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	require.Len(t, result, 1)
	require.Equal(t, "custom", result[0].Name)
	require.Equal(t, 1, result[0].NoteCount)
}

func TestPropertiesIncludesTagsByDefault(t *testing.T) {
	notes := map[string]string{
		"note.md": `---
tags: [project, work]
---`,
	}

	vaultManager := &mocks.VaultManager{}
	noteManager := &mocks.NoteReader{}

	vaultManager.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)
	noteManager.On("GetNotesList", mock.Anything).Return([]string{"note.md"}, nil)
	noteManager.On("GetContents", mock.Anything, "note.md").Return(notes["note.md"], nil)

	result, err := Properties(vaultManager, newFactReader(noteManager, notes), PropertiesOptions{
		ValueLimit: 10,
		MaxValues:  10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundTags := false
	for _, s := range result {
		if s.Name == "tags" && len(s.EnumValues) == 2 {
			foundTags = true
			break
		}
	}
	if !foundTags {
		t.Fatalf("expected tags property to be included by default")
	}
}

func TestPropertiesTrustReadySessionStoreWhenSnapshotHasAdvanced(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte("placeholder\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Launch.md"), []byte("placeholder\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "TestVault", Path: root},
		name: "TestVault",
	}
	initial := &metadataSnapshotNote{
		entries: []cache.Entry{
			{
				Path:        "notes/Roadmap.md",
				Content:     "---\nstatus: active\n---\n",
				Frontmatter: map[string]any{"status": "active"},
				ModTime:     time.Unix(10, 0),
				Size:        24,
			},
		},
	}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), vault.def, initial, store)
	require.NoError(t, err)

	stale := &metadataSnapshotNote{
		entries: []cache.Entry{
			{
				Path:        "notes/Roadmap.md",
				Content:     "---\nstatus: active\n---\n",
				Frontmatter: map[string]any{"status": "active"},
				ModTime:     time.Unix(10, 0),
				Size:        24,
			},
			{
				Path:        "notes/Launch.md",
				Content:     "---\npriority: high\n---\n",
				Frontmatter: map[string]any{"priority": "high"},
				ModTime:     time.Unix(20, 0),
				Size:        25,
			},
		},
	}

	retained, err := Properties(vault, stale, PropertiesOptions{
		SessionStore: store,
		Only:         []string{"status"},
		ValueLimit:   5,
		MaxValues:    10,
	})
	require.NoError(t, err)
	require.Len(t, retained, 1)
	require.Equal(t, "status", retained[0].Name)
	require.Equal(t, 1, retained[0].NoteCount)
	require.Equal(t, []string{"active"}, retained[0].EnumValues)

	summaries, err := Properties(vault, stale, PropertiesOptions{
		SessionStore: store,
		Only:         []string{"priority"},
	})
	require.NoError(t, err)
	require.Empty(t, summaries)
}

func TestPropertiesWithSubset(t *testing.T) {
	notes := map[string]string{
		"one.md": `---
prop: a
---`,
		"two.md": `---
prop: b
---`,
	}

	vaultManager := &mocks.VaultManager{}
	noteManager := &mocks.NoteReader{}

	vaultManager.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)
	noteManager.On("GetNotesList", mock.Anything).Return([]string{"one.md", "two.md"}, nil)
	noteManager.On("GetContents", mock.Anything, "one.md").Return(notes["one.md"], nil)
	noteManager.On("GetContents", mock.Anything, "two.md").Return(notes["two.md"], nil)

	result, err := Properties(vaultManager, newFactReader(noteManager, notes), PropertiesOptions{
		Notes:      []string{"one.md"},
		ValueLimit: 10,
		MaxValues:  10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 || result[0].Name != "prop" || result[0].EnumValues[0] != "a" {
		t.Fatalf("expected only subset property, got %+v", result)
	}
}

func TestPropertiesOnlyFiltersProperties(t *testing.T) {
	notes := map[string]string{
		"note.md": `---
keep: front
drop: skip
---
keep:: inline
drop:: nope
`,
	}

	vaultManager := &mocks.VaultManager{}
	noteManager := &mocks.NoteReader{}

	vaultManager.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)
	noteManager.On("GetNotesList", mock.Anything).Return([]string{"note.md"}, nil)
	noteManager.On("GetContents", mock.Anything, "note.md").Return(notes["note.md"], nil)

	result, err := Properties(vaultManager, newFactReader(noteManager, notes), PropertiesOptions{
		ValueLimit: 10,
		MaxValues:  10,
		Only:       []string{"keep"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 property, got %d", len(result))
	}
	if result[0].Name != "keep" {
		t.Fatalf("expected only 'keep' property, got %s", result[0].Name)
	}
	if result[0].DistinctValueCount != 2 {
		t.Fatalf("expected two values for keep, got %d", result[0].DistinctValueCount)
	}
}

func TestPropertiesMaxValuesFollowsValueLimit(t *testing.T) {
	notes := map[string]string{
		"one.md": `---
prop: a
---`,
		"two.md": `---
prop: b
---`,
		"three.md": `---
prop: c
---`,
		"four.md": `---
prop: d
---`,
	}

	vaultManager := &mocks.VaultManager{}
	noteManager := &mocks.NoteReader{}

	vaultManager.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)
	noteManager.On("GetNotesList", mock.Anything).Return([]string{"one.md", "two.md", "three.md", "four.md"}, nil)
	for path, content := range notes {
		noteManager.On("GetContents", mock.Anything, path).Return(content, nil)
	}

	result, err := Properties(vaultManager, newFactReader(noteManager, notes), PropertiesOptions{
		ValueLimit: 4,
		MaxValues:  3, // deliberately low; should be raised to valueLimit+1 internally
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, summary := range result {
		if summary.Name == "prop" {
			if summary.TruncatedValueSet {
				t.Fatalf("expected prop values not to be truncated")
			}
			if len(summary.EnumValues) != 4 {
				t.Fatalf("expected 4 enum values, got %d", len(summary.EnumValues))
			}
			return
		}
	}
	t.Fatalf("expected prop summary to be present")
}

func TestPropertiesAutomaticallyEnumeratesSmallMixedValues(t *testing.T) {
	notes := map[string]string{
		"one.md": `---
prop: a
---`,
		"two.md": `---
prop: 1
---`,
	}

	vaultManager := &mocks.VaultManager{}
	noteManager := &mocks.NoteReader{}

	vaultManager.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)
	noteManager.On("GetNotesList", mock.Anything).Return([]string{"one.md", "two.md"}, nil)
	noteManager.On("GetContents", mock.Anything, "one.md").Return(notes["one.md"], nil)
	noteManager.On("GetContents", mock.Anything, "two.md").Return(notes["two.md"], nil)

	result, err := Properties(vaultManager, newFactReader(noteManager, notes), PropertiesOptions{
		ValueLimit: 10,
		MaxValues:  10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, s := range result {
		if s.Name == "prop" {
			found = true
			if len(s.EnumValues) != 2 {
				t.Fatalf("expected enum values for mixed property, got %+v", s.EnumValues)
			}
		}
	}
	if !found {
		t.Fatalf("expected prop property to be present")
	}
}

func TestPropertiesEnumValueCounts(t *testing.T) {
	notes := map[string]string{
		"one.md": `---
office: AOG
---`,
		"two.md": `---
office: AOG
---`,
	}

	vaultManager := &mocks.VaultManager{}
	noteManager := &mocks.NoteReader{}

	vaultManager.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)
	noteManager.On("GetNotesList", mock.Anything).Return([]string{"one.md", "two.md"}, nil)
	noteManager.On("GetContents", mock.Anything, "one.md").Return(notes["one.md"], nil)
	noteManager.On("GetContents", mock.Anything, "two.md").Return(notes["two.md"], nil)

	result, err := Properties(vaultManager, newFactReader(noteManager, notes), PropertiesOptions{
		ValueLimit:         5,
		MaxValues:          10,
		IncludeValueCounts: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, s := range result {
		if s.Name == "office" {
			found = true
			if s.EnumValueCounts["AOG"] != 2 {
				t.Fatalf("expected count 2 for AOG, got %d", s.EnumValueCounts["AOG"])
			}
		}
	}
	if !found {
		t.Fatalf("expected office property")
	}
}

func TestPropertiesUsesCachedEntries(t *testing.T) {
	vault := &mocks.VaultManager{}
	vault.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)

	note := &cachedNoteReader{
		entries: []cache.Entry{
			{
				Path:        "Alpha.md",
				Frontmatter: map[string]interface{}{"office": "AOGR"},
				InlineProps: map[string][]string{"Status": {"Ready"}},
			},
			{
				Path:        "Beta.md",
				Frontmatter: map[string]interface{}{"office": "AORD"},
				InlineProps: map[string][]string{"Status": {"Ready"}},
			},
		},
	}

	result, err := Properties(vault, note, PropertiesOptions{ExcludeTags: true, IncludeValueCounts: true})
	require.NoError(t, err)

	require.Len(t, result, 2)

	props := make(map[string]PropertySummary)
	for _, p := range result {
		props[p.Name] = p
	}

	office := props["office"]
	require.ElementsMatch(t, []string{"AOGR", "AORD"}, office.EnumValues)
	require.Equal(t, map[string]int{"AOGR": 1, "AORD": 1}, office.EnumValueCounts)

	status := props["Status"]
	require.ElementsMatch(t, []string{"Ready"}, status.EnumValues)
	require.Equal(t, 0, note.contentsCalls, "cache should avoid GetContents calls")
	require.Equal(t, 0, note.notesListCalls, "cache should avoid GetNotesList calls")
}

func TestPropertiesCachedSubsetPreservesAuthoredExtensions(t *testing.T) {
	vault := &mocks.VaultManager{}
	vault.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)

	note := &cachedNoteReader{
		entries: []cache.Entry{
			{Path: "Notes/Decision.MD", Frontmatter: map[string]interface{}{"kind": "markdown"}},
			{Path: "Notes/Reference.html", Frontmatter: map[string]interface{}{"kind": "html"}},
			{Path: "Notes/Excluded.md", Frontmatter: map[string]interface{}{"kind": "excluded"}},
		},
	}

	result, err := Properties(vault, note, PropertiesOptions{
		Notes:      []string{"Notes/Decision.MD", "Notes/Reference.html"},
		ValueLimit: 10,
		MaxValues:  10,
	})
	require.NoError(t, err)
	require.Equal(t, []PropertySummary{{
		Name:               "kind",
		NoteCount:          2,
		Shape:              "scalar",
		ValueType:          "string",
		EnumValues:         []string{"html", "markdown"},
		DistinctValueCount: 2,
	}}, result)
	require.Equal(t, 0, note.contentsCalls, "cache should avoid GetContents calls")
	require.Equal(t, 0, note.notesListCalls, "cache should avoid GetNotesList calls")
}
