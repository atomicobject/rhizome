package notediscovery_test

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/stretchr/testify/require"
)

func TestStructuredSourcesRequireExplicitIncludes(t *testing.T) {
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		mode     notediscovery.Mode
		includes []string
		path     string
		want     notediscovery.Owner
	}{
		{"classic YAML", notediscovery.ClassicVault, nil, "model/data.yaml", notediscovery.Code},
		{"classic JSON", notediscovery.ClassicVault, nil, "generated/data.json", notediscovery.Code},
		{"default", notediscovery.CollectionVault, nil, "model/data.yml", notediscovery.Code},
		{"broad", notediscovery.CollectionVault, []string{"**/*"}, "model/data.yaml", notediscovery.Code},
		{"directory", notediscovery.CollectionVault, []string{"model/**"}, "model/data.json", notediscovery.Code},
		{"YAML brace", notediscovery.CollectionVault, []string{"model/*.{yaml,yml}"}, "model/data.YML", notediscovery.Note},
		{"YAML extension", notediscovery.CollectionVault, []string{"model/*.YAML"}, "model/data.yaml", notediscovery.Note},
		{"directory case", notediscovery.CollectionVault, []string{"Model/*.yaml"}, "model/data.yaml", notediscovery.Code},
		{"basename case", notediscovery.CollectionVault, []string{"model/Data.yaml"}, "model/data.yaml", notediscovery.Code},
		{"selected generated", notediscovery.CollectionVault, []string{"generated/selected.json"}, "generated/selected.JSON", notediscovery.Note},
		{"unselected generated", notediscovery.CollectionVault, []string{"generated/selected.json"}, "generated/other.json", notediscovery.Code},
		{"extension wildcard", notediscovery.CollectionVault, []string{"model/*.y*"}, "model/data.yaml", notediscovery.Code},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := notediscovery.Compile(notediscovery.Config{Mode: tc.mode, Includes: tc.includes, Registry: registry})
			require.NoError(t, err)
			got, err := plan.Classify(paths.RelPath(tc.path), true)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Owner)
			if tc.want == notediscovery.Note {
				require.NotEmpty(t, got.Provider)
			} else {
				require.Empty(t, got.Provider)
			}
		})
	}
	plan, err := notediscovery.Compile(notediscovery.Config{Mode: notediscovery.CollectionVault, Includes: []string{"model/*.yaml"}, Registry: registry, Ignore: func(paths.RelPath) bool { return true }})
	require.NoError(t, err)
	got, err := plan.Classify("model/data.yaml", true)
	require.NoError(t, err)
	require.Equal(t, notediscovery.Ignored, got.Owner)
}
