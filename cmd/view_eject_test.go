package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestViewEjectCopiesBundledFolderThatThenResolves(t *testing.T) {
	bundled := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(bundled, "group", "parts"), 0o755))
	for name, body := range map[string]string{
		"group/briefing.yaml":  "apiVersion: rhizome.view.v1\nid: group.briefing\nname: Briefing\nsource: {kind: custom, entry: briefing.tsx}\nmount: {kind: group, group: \"*\"}\n",
		"group/briefing.tsx":   "import { Part } from \"./parts/part\";\nexport default function Briefing() { return <Part />; }\n",
		"group/parts/part.tsx": "export function Part() { return null; }\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(bundled, filepath.FromSlash(name)), []byte(body), 0o644))
	}
	t.Setenv("RHIZOME_BUNDLED_VIEWS_DIR", bundled)
	vaultRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultRoot, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultRoot, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	ctx := contextWithCommandEnv(context.Background(), commandEnv{Getwd: func() (string, error) { return vaultRoot, nil }})

	run := func(args ...string) (string, error) {
		cmd := newViewCmd(false)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(ctx)
		return out.String(), err
	}

	out, err := run("eject", "group.briefing")
	require.NoError(t, err, out)
	require.Equal(t, "wrote .rhizome/views/group/briefing.tsx\n"+
		"wrote .rhizome/views/group/briefing.yaml\n"+
		"wrote .rhizome/views/group/parts/part.tsx\n"+
		"group.briefing now resolves to .rhizome/views/group\n", out)
	copied, err := os.ReadFile(filepath.Join(vaultRoot, ".rhizome", "views", "group", "parts", "part.tsx"))
	require.NoError(t, err)
	require.Equal(t, "export function Part() { return null; }\n", string(copied))

	out, err = run("show", "group.briefing", "--json")
	require.NoError(t, err, out)
	var entry appviews.CatalogEntry
	require.NoError(t, json.Unmarshal([]byte(out), &entry))
	require.Equal(t, viewconfig.OriginRepository, entry.Origin)

	_, err = run("eject", "group.briefing")
	require.ErrorContains(t, err, "already comes from")
}
