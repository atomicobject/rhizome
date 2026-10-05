package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestCodeIndexAnchorsCmd_RefreshesAnchors(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n"), 0o644))

	notePath := filepath.Join(vaultDir, "Billing.md")
	require.NoError(t, os.WriteFile(notePath, []byte(`---
title: "Billing"
anchors:
  - define:
      label: "BillingDomain"
      kind: "annotation"
      lang: "py"
      annotation:
        symbol:
          pkg: ""
          name: "decorator"
---
`), 0o644))

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origVault := vaultName
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		vaultName = origVault
		rootCmd.SetArgs([]string{})
	})

	rootCmd.SetArgs([]string{"code", "index-anchors", "--vault", vaultDir})
	err := rootCmd.Execute()
	require.NoError(t, err)
	require.Contains(t, errOut.String(), "Code anchors refreshed")

	codeCfg, err := obsidian.LoadCodeConfig(vaultDir)
	require.NoError(t, err)
	store, cleanup, err := obsidian.OpenIntelStoreForWriteFromConfig(vaultDir, codeCfg)
	require.NoError(t, err)
	defer cleanup()

	anchors, err := store.Anchors(context.Background())
	require.NoError(t, err)
	require.Len(t, anchors, 1)
	require.Equal(t, "BillingDomain", anchors[0].Label)
}

func TestCodeExplain_NoteJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	notePath := filepath.Join(dir, "anchors.md")
	require.NoError(t, os.WriteFile(notePath, []byte(`---
title: "Anchors"
code-anchors:
  py:
    - label: Billing
      decorator: svc.decorator
      args:
        tag: billing
---
`), 0o644))

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		codeExplainJSON = false
		codeAnchorsListJSON = false
		codeAnchorsListLimit = 0
		codeAnchorsMatchJSON = false
		codeAnchorsMatchKind = ""
		codeAnchorsMatchValue = ""
		codeAnchorsMatchLimit = 0
	})

	rootCmd.SetArgs([]string{"code", "anchors", "explain", notePath, "--vault", dir, "--json"})
	err := rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	var payload struct {
		Results []struct {
			Kind string           `json:"kind"`
			Note *codeanchor.Note `json:"note"`
		} `json:"results"`
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Len(t, payload.Results, 1)
	require.Equal(t, "note", payload.Results[0].Kind)
	require.NotNil(t, payload.Results[0].Note)
	require.Equal(t, "Anchors", payload.Results[0].Note.Title)
	require.NotEmpty(t, payload.Results[0].Note.DefinedAnchors)
}

func TestCodeExplainReportsConfiguredHTMLAsUnsupportedNoteFormat(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"notes/*.html\"]\n"), 0o644))
	notePath := filepath.Join(vaultDir, "notes", "Reference.HTML")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte("<h1>Reference</h1>\n"), 0o644))

	out := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	rootCmd.SetOut(out)
	origVault := vaultName
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		vaultName = origVault
		codeExplainJSON = false
	})

	rootCmd.SetArgs([]string{"code", "anchors", "explain", notePath, "--vault", vaultDir, "--json"})
	require.NoError(t, rootCmd.Execute())
	rootCmd.SetArgs([]string{})

	var payload struct {
		Results []struct {
			Kind  string           `json:"kind"`
			Note  *codeanchor.Note `json:"note"`
			Error string           `json:"error"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Len(t, payload.Results, 1)
	require.Equal(t, "note", payload.Results[0].Kind)
	require.Nil(t, payload.Results[0].Note)
	require.Equal(t, `code anchor explanation is not supported for note format "html"`, payload.Results[0].Error)
}

func TestCodeExplainRejectsUnownedPath(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	unknownPath := filepath.Join(vaultDir, "notes", "Unknown.asset")
	require.NoError(t, os.MkdirAll(filepath.Dir(unknownPath), 0o755))
	require.NoError(t, os.WriteFile(unknownPath, []byte("unknown\n"), 0o644))

	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	origVault := vaultName
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		vaultName = origVault
		codeExplainJSON = false
	})
	rootCmd.SetArgs([]string{"code", "anchors", "explain", unknownPath, "--vault", vaultDir, "--json"})
	err := rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.ErrorContains(t, err, "has no configured ownership")
}

func TestCodeExplain_FileJSON(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n"), 0o644))

	// Seed a note with an annotation anchor.
	notePath := filepath.Join(vaultDir, "Billing.md")
	require.NoError(t, os.WriteFile(notePath, []byte(`---
title: "Billing"
anchors:
  - define:
      label: "BillingDomain"
      kind: "annotation"
      lang: "py"
      annotation:
        symbol:
          pkg: ""
          name: "decorator"
        argFilters:
          tag: "billing"
---
`), 0o644))

	codePath := filepath.Join(vaultDir, "svc", "invoice.py")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte(`@decorator(tag="billing")
class InvoiceService: pass
`), 0o644))

	// Build the code index backing the explain command.
	dbPath := codeanchor.DefaultIndexPath(vaultDir)
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithoutWarmCache(),
		codeanchor.WithBasePath(vaultDir),
		codeanchor.WithWriteAccess(),
	)
	ctx := context.Background()
	noteContent, err := os.ReadFile(notePath)
	require.NoError(t, err)
	_, err = svc.IngestNoteSource(ctx, testNoteSourceSnapshot(t, vaultDir, notePath, string(noteContent)))
	require.NoError(t, err)
	codeContent, err := os.ReadFile(codePath)
	require.NoError(t, err)
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, codePath, codeContent))
	require.NoError(t, svc.RecomputeAnchorScopes(ctx))
	require.NoError(t, store.Close())

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origVault := vaultName
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		vaultName = origVault
		codeExplainJSON = false
		codeAnchorsListJSON = false
		codeAnchorsListLimit = 0
		codeAnchorsMatchJSON = false
		codeAnchorsMatchKind = ""
		codeAnchorsMatchValue = ""
		codeAnchorsMatchLimit = 0
	})

	rootCmd.SetArgs([]string{"code", "anchors", "explain", codePath, "--vault", vaultDir, "--json"})
	err = rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	var payload struct {
		Results []struct {
			Kind        string                  `json:"kind"`
			FileContext *codeanchor.FileContext `json:"fileContext"`
		} `json:"results"`
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Len(t, payload.Results, 1)
	require.Equal(t, "file", payload.Results[0].Kind)
	require.NotNil(t, payload.Results[0].FileContext)
	require.Contains(t, payload.Results[0].FileContext.Anchors, "BillingDomain")
	require.NotEmpty(t, payload.Results[0].FileContext.Trace)
}

func TestCodeAnchorsList_JSON(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n"), 0o644))

	notePath := filepath.Join(vaultDir, "Billing.md")
	require.NoError(t, os.WriteFile(notePath, []byte(`---
title: "Billing"
anchors:
  - define:
      label: "BillingDomain"
      kind: "annotation"
      lang: "py"
      annotation:
        symbol:
          pkg: ""
          name: "decorator"
        argFilters:
          tag: "billing"
---
`), 0o644))

	dbPath := codeanchor.DefaultIndexPath(vaultDir)
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithoutWarmCache(),
		codeanchor.WithBasePath(vaultDir),
	)
	ctx := context.Background()
	noteContent, err := os.ReadFile(notePath)
	require.NoError(t, err)
	_, err = svc.IngestNoteSource(ctx, testNoteSourceSnapshot(t, vaultDir, notePath, string(noteContent)))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origVault := vaultName
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		vaultName = origVault
		codeExplainJSON = false
		codeAnchorsListJSON = false
		codeAnchorsListLimit = 0
		codeAnchorsMatchJSON = false
		codeAnchorsMatchKind = ""
		codeAnchorsMatchValue = ""
		codeAnchorsMatchLimit = 0
	})

	rootCmd.SetArgs([]string{"code", "anchors", "list", "--vault", vaultDir, "--json"})
	err = rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	var payload struct {
		Anchors []struct {
			Label     string   `json:"label"`
			NotePaths []string `json:"notePaths"`
		} `json:"anchors"`
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Len(t, payload.Anchors, 1)
	require.Equal(t, "BillingDomain", payload.Anchors[0].Label)
	require.NotEmpty(t, payload.Anchors[0].NotePaths)
}

func TestCodeAnchorsMatch_DecoratorJSON(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n"), 0o644))

	notePath := filepath.Join(vaultDir, "Billing.md")
	require.NoError(t, os.WriteFile(notePath, []byte(`---
title: "Billing"
anchors:
  - define:
      label: "BillingDomain"
      kind: "annotation"
      lang: "py"
      annotation:
        symbol:
          pkg: ""
          name: "decorator"
---
`), 0o644))

	dbPath := codeanchor.DefaultIndexPath(vaultDir)
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithoutWarmCache(),
		codeanchor.WithBasePath(vaultDir),
	)
	ctx := context.Background()
	noteContent, err := os.ReadFile(notePath)
	require.NoError(t, err)
	_, err = svc.IngestNoteSource(ctx, testNoteSourceSnapshot(t, vaultDir, notePath, string(noteContent)))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origVault := vaultName
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		vaultName = origVault
		codeExplainJSON = false
		codeAnchorsListJSON = false
		codeAnchorsListLimit = 0
		codeAnchorsMatchJSON = false
		codeAnchorsMatchKind = ""
		codeAnchorsMatchValue = ""
		codeAnchorsMatchLimit = 0
	})

	rootCmd.SetArgs([]string{
		"code", "anchors", "match",
		"--vault", vaultDir,
		"--kind", "decorator",
		"--value", "py:decorator",
		"--json",
	})
	err = rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	var payload struct {
		Anchors []struct {
			Label string `json:"label"`
		} `json:"anchors"`
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Len(t, payload.Anchors, 1)
	require.Equal(t, "BillingDomain", payload.Anchors[0].Label)
}

func TestCodeAnchorsMatch_ByLabelShowsInstances(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n"), 0o644))

	notePath := filepath.Join(vaultDir, "Billing.md")
	require.NoError(t, os.WriteFile(notePath, []byte(`---
title: "Billing"
anchors:
  - define:
      label: "BillingDomain"
      kind: "annotation"
      lang: "py"
      annotation:
        symbol:
          pkg: ""
          name: "decorator"
        argFilters:
          tag: "billing"
---
`), 0o644))

	codePath := filepath.Join(vaultDir, "svc", "invoice.py")
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte(`@decorator(tag="billing")
class InvoiceService: pass
`), 0o644))

	dbPath := codeanchor.DefaultIndexPath(vaultDir)
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithoutWarmCache(),
		codeanchor.WithBasePath(vaultDir),
		codeanchor.WithWriteAccess(),
	)
	ctx := context.Background()
	noteContent, err := os.ReadFile(notePath)
	require.NoError(t, err)
	_, err = svc.IngestNoteSource(ctx, testNoteSourceSnapshot(t, vaultDir, notePath, string(noteContent)))
	require.NoError(t, err)
	codeContent, err := os.ReadFile(codePath)
	require.NoError(t, err)
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, codePath, codeContent))
	require.NoError(t, svc.RecomputeAnchorScopes(ctx))
	require.NoError(t, store.Close())

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origVault := vaultName
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		vaultName = origVault
		codeExplainJSON = false
		codeAnchorsListJSON = false
		codeAnchorsListLimit = 0
		codeAnchorsMatchJSON = false
		codeAnchorsMatchKind = ""
		codeAnchorsMatchValue = ""
		codeAnchorsMatchLimit = 0
	})

	rootCmd.SetArgs([]string{"code", "anchors", "match", "BillingDomain", "--vault", vaultDir, "--json"})
	err = rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	var payload struct {
		Anchors []struct {
			Label   string   `json:"label"`
			Symbols []string `json:"symbols"`
		} `json:"anchors"`
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Len(t, payload.Anchors, 1)
	require.Equal(t, "BillingDomain", payload.Anchors[0].Label)
	require.NotEmpty(t, payload.Anchors[0].Symbols)
}

func TestCodeAnchorsMatch_KindDirMatchesDirAnchors(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n"), 0o644))

	// Create a directory target + file. glob/dir anchors match on path alone (no code indexing needed).
	codeDir := filepath.Join(vaultDir, "src", "todoapp", "services")
	require.NoError(t, os.MkdirAll(codeDir, 0o755))
	codePath := filepath.Join(codeDir, "tasks.py")
	require.NoError(t, os.WriteFile(codePath, []byte("# stub\n"), 0o644))

	// Ingest a dir: anchor (stored as glob patterns resolved relative to basePath).
	notePath := filepath.Join(vaultDir, "Notes", "DirAnchor.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
title: "Dir anchor"
code-anchors:
  python:
    - dir: src/todoapp/services
---
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	dbPath := codeanchor.DefaultIndexPath(vaultDir)
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithoutWarmCache(),
		codeanchor.WithBasePath(vaultDir),
	)
	ctx := context.Background()
	_, err = svc.IngestNoteSource(ctx, testNoteSourceSnapshot(t, vaultDir, notePath, noteContent))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origVault := vaultName
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		vaultName = origVault
		codeExplainJSON = false
		codeAnchorsListJSON = false
		codeAnchorsListLimit = 0
		codeAnchorsMatchJSON = false
		codeAnchorsMatchKind = ""
		codeAnchorsMatchValue = ""
		codeAnchorsMatchLimit = 0
	})

	rootCmd.SetArgs([]string{
		"code", "anchors", "match",
		"--vault", vaultDir,
		"--kind", "dir",
		"--value", codePath,
		"--json",
	})
	err = rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	var payload struct {
		Anchors []struct {
			Label string `json:"label"`
		} `json:"anchors"`
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Len(t, payload.Anchors, 1)
	require.Equal(t, "src/todoapp/services", payload.Anchors[0].Label)
}

func testNoteSourceSnapshot(t *testing.T, root, path, content string) notemeta.NoteSourceSnapshot {
	t.Helper()
	if root == "" {
		return notemeta.NewContentOnlyNoteSourceSnapshot(path, content, 0)
	}
	rel, err := filepath.Rel(root, path)
	require.NoError(t, err)
	return notemeta.NewContentOnlyNoteSourceSnapshot(filepath.ToSlash(rel), content, 0)
}
