package structured

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestAdaptedValuesPreserveKindsOrderAndAddresses(t *testing.T) {
	root, issues := ParseYAML([]byte("z: null\na/b~: ['', [], {}, true, 123456789012345678901234567890, 2026-11-06, 2026-11-06T12:30:00+02:30]\n01: integer key\n'': empty key\n"))
	require.Empty(t, issues)
	facts, err := structureFacts(root)
	require.NoError(t, err)
	require.Len(t, facts.Values, 12)
	require.Len(t, facts.Owners, 12)
	require.Equal(t, facts.Root, facts.Owners[0].Key)
	require.Equal(t, []string{"z", "a/b~", "1", ""}, []string{fmt.Sprint(facts.Values[0].Entries[0].Key.Export()), fmt.Sprint(facts.Values[0].Entries[1].Key.Export()), fmt.Sprint(facts.Values[0].Entries[2].Key.Export()), fmt.Sprint(facts.Values[0].Entries[3].Key.Export())})
	require.Equal(t, noteformat.MetadataInteger, facts.Values[0].Entries[2].Key.Kind())
	want := []noteformat.MetadataValueKind{noteformat.MetadataObject, noteformat.MetadataNull, noteformat.MetadataArray, noteformat.MetadataString, noteformat.MetadataArray, noteformat.MetadataObject, noteformat.MetadataBoolean, noteformat.MetadataInteger, noteformat.MetadataDate, noteformat.MetadataTimestamp, noteformat.MetadataString, noteformat.MetadataString}
	for i, owner := range facts.Owners {
		require.Equal(t, want[i], facts.Values[owner.Value-1].Kind)
		require.Equal(t, noteformat.ValueID(i+1), owner.Value, "one value per source occurrence")
		require.False(t, owner.Range.Present)
		require.Empty(t, owner.OwnRanges)
		require.Nil(t, owner.Markdown)
	}
	require.Equal(t, "/a~1b~0/0", facts.Owners[3].Address)
	require.Equal(t, "123456789012345678901234567890", string(facts.Values[7].Scalar.CanonicalJSON()))
	require.Equal(t, `"2026-11-06"`, string(facts.Values[8].Scalar.CanonicalJSON()))
	require.Equal(t, `"2026-11-06T12:30:00+02:30"`, string(facts.Values[9].Scalar.CanonicalJSON()))
	copy := facts.Copy()
	copy.Owners[0].Properties[0].Key = "changed"
	copy.Values[0].Entries[0].Value = 0
	require.Equal(t, "z", facts.Owners[0].Properties[0].Key)
	require.NotZero(t, facts.Values[0].Entries[0].Value)
}

func TestAdapterOwnerWalkIsLinear(t *testing.T) {
	for _, rows := range []int{100, 1000} {
		root, issues := ParseJSON([]byte(`{"rows":[` + strings.TrimSuffix(strings.Repeat(`{"id":"A","nested":[null,{}]},`, rows), ",") + `]}`))
		require.Empty(t, issues)
		facts, err := structureFacts(root)
		require.NoError(t, err)
		require.Len(t, facts.Owners, 2+5*rows)
		require.Len(t, facts.Values, 2+5*rows)
	}
}

func TestProviderFatalDiagnosticsRetainHostAndBothStartLocations(t *testing.T) {
	provider := NewYAML()
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	text := "\ufeffé: 1\r\né: 2\r\n"
	source, err := noteformat.NewAuthoredSource(paths.NotePath("model/invalid.yml"), provider.Descriptor(), []byte(text), 0)
	require.NoError(t, err)
	projection, err := runtime.Project(source)
	require.NoError(t, err)
	require.Equal(t, noteformat.ProjectionStatusFatal, projection.Status)
	require.Nil(t, projection.Facts.Structure)
	require.Len(t, projection.Diagnostics, 1)
	issue := projection.Diagnostics[0]
	require.Equal(t, "yaml_duplicate_key", issue.Code)
	require.Equal(t, `model/invalid.yml:2:1 (byte 10) at "/é": duplicate key "é"; first occurrence model/invalid.yml:1:1 (byte 3)`, issue.Message)
	require.True(t, issue.Blocking)
	require.False(t, issue.Range.Present)
}
