package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestIndexStatus_BehaviorLock(t *testing.T) {
	vault := behaviorTestVault(t, behaviorVaultOptions{
		noteEmbeddings: &embeddings.Config{
			Enabled:  true,
			Provider: "openai",
			Model:    "text-embedding-3-small",
		},
		codeEnabled: true,
	})

	stdout, stderr, err := runCLI(t, []string{"index", "--status", "--vault", vault}, "")
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(stdout))
	require.Contains(t, stderr, "Semantic index:")
	require.Contains(t, stderr, "Code index:")
	require.Contains(t, stderr, "Provider:openai")
}

func TestIndexCommandsDetectCodeChangesWithPreservedTimestamp(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "full", args: []string{"index"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			disabled := &embeddings.Config{Enabled: false}
			vault := behaviorTestVault(t, behaviorVaultOptions{
				noteEmbeddings: disabled,
				codeEmbeddings: disabled,
				codeEnabled:    true,
			})
			codePath := filepath.Join(vault, "main.go")
			mtime := time.Unix(1_700_000_000, 0)
			require.NoError(t, os.WriteFile(codePath, []byte("package main\nfunc Before() {}\n"), 0o644))
			require.NoError(t, os.Chtimes(codePath, mtime, mtime))

			_, _, err := runCLIWithVault(t, tt.args, "", vault)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(codePath, []byte("package main\nfunc After() {}\n"), 0o644))
			require.NoError(t, os.Chtimes(codePath, mtime, mtime))
			_, _, err = runCLIWithVault(t, tt.args, "", vault)
			require.NoError(t, err)

			store, err := semdb.Open(obsidian.UnifiedIndexPath(vault, ""))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			symbols, err := store.SymbolsByFile(context.Background(), "main.go")
			require.NoError(t, err)
			require.Contains(t, strings.Join(symbols, "\n"), "After")
			require.NotContains(t, strings.Join(symbols, "\n"), "Before")
		})
	}
}

func TestUnifiedSearch_BehaviorLock_NoMatches(t *testing.T) {
	vault := behaviorTestVault(t, behaviorVaultOptions{
		noteEmbeddings: &embeddings.Config{
			Enabled:  false,
			Provider: "test",
			Model:    "deterministic",
		},
		codeEnabled: true,
	})

	dbPath := filepath.Join(vault, ".rhizome", "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	stdout, _, err := runCLIWithVault(t, []string{"search", "kickoff", "--fast", "--timeout", "0s"}, "", vault)
	require.NoError(t, err)
	require.Contains(t, stdout, "No matches.")
}

func TestUnifiedSearch_FailsWhenProviderInvalid(t *testing.T) {
	vault := behaviorTestVault(t, behaviorVaultOptions{
		noteEmbeddings: &embeddings.Config{
			Enabled:  true,
			Provider: "definitely-invalid-provider",
			Model:    "deterministic",
		},
		codeEnabled: false,
	})

	_, _, err := runCLIWithVault(t, []string{"search", "kickoff", "--timeout", "0s"}, "", vault)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "provider")
}

func TestRootHelpOmitsRemovedAliasCommands(t *testing.T) {
	stdout, stderr, err := runCLI(t, []string{"--help"}, "")
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(stderr))
	require.NotContains(t, stdout, "  semantic    ")
	require.NotContains(t, stdout, "  daily       ")
	require.NotContains(t, stdout, "  file        ")
	require.NotContains(t, stdout, "  tags        ")
	require.NotContains(t, stdout, "  properties  ")
}

func TestUnifiedSearchHelpOmitsBackendToggleFlags(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		present []string
		absent  []string
	}{
		{"search", []string{"search", "--help"}, []string{"--mode", "--raw"}, []string{"--vector", "--intel", "--graph", "--refs", "--use-fts-body", "--intent"}},
		{"init", []string{"init", "--help"}, nil, []string{"--ai"}},
		{"properties", []string{"note", "properties", "list", "--help"}, []string{"--value-limit", "--value-counts"}, []string{"--enum-threshold", "--enum-counts"}},
		{"code", []string{"code", "--help"}, []string{"--code-root"}, []string{"--python-root", "explain [paths...]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runCLI(t, tc.args, "")
			require.NoError(t, err)
			require.Empty(t, strings.TrimSpace(stderr))
			for _, value := range tc.present {
				require.Contains(t, stdout, value)
			}
			for _, value := range tc.absent {
				require.NotContains(t, stdout, value)
			}
		})
	}
}

func TestUnifiedSearchSpecificity_PrefersDirectEmbeddingEvidence(t *testing.T) {
	vault := behaviorTestVault(t, behaviorVaultOptions{
		noteEmbeddings: &embeddings.Config{Enabled: true, Provider: "test", Model: "deterministic", Dimensions: 16},
		codeEmbeddings: &embeddings.Config{Enabled: true, Provider: "test", Model: "deterministic", Dimensions: 16},
		codeEnabled:    true,
	})
	require.NoError(t, os.MkdirAll(filepath.Join(vault, "docs", "hubs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(vault, "pkg", "search", "semantic"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(vault, "pkg", "generic"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vault, "docs", "hubs", "Embeddings (Hub).md"), []byte("# Embeddings\n\nNotes are embedded into chunks by the note embedding pipeline.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vault, "pkg", "search", "semantic", "embedder.go"), []byte(`package semantic

// QueryEmbeddings holds computed embeddings for a query.
type QueryEmbeddings struct {
	Note []float64
	Code []float64
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vault, "pkg", "generic", "store.go"), []byte(`package generic

// Store persists generic records.
type Store struct{}
`), 0o644))

	_, _, err := runCLIWithVault(t, []string{"index", "--rebuild"}, "", vault)
	require.NoError(t, err)

	stdout, _, err := runCLIWithVault(t, []string{"search", "how are notes embedded?"}, "", vault)
	require.NoError(t, err)
	require.Contains(t, stdout, "docs/hubs/Embeddings (Hub).md")
	require.Contains(t, stdout, "pkg/search/semantic/embedder.go")
	require.NotContains(t, stdout, "pkg/generic/store.go")
	require.NotContains(t, stdout, "Missing tests")
}

type behaviorVaultOptions struct {
	noteEmbeddings *embeddings.Config
	codeEmbeddings *embeddings.Config
	codeEnabled    bool
}

func behaviorTestVault(t *testing.T, opts behaviorVaultOptions) string {
	t.Helper()

	vault := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vault, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vault, "note.md"), []byte("# note\n"), 0o644))

	cfg := obsidian.LocalConfig{
		IndexPath: filepath.Join(vault, ".rhizome", "db.sqlite"),
	}
	if opts.noteEmbeddings != nil {
		cfg.NoteEmbeddings = opts.noteEmbeddings
	}
	if opts.codeEmbeddings != nil {
		cfg.CodeEmbeddings = opts.codeEmbeddings
	}
	if opts.codeEnabled {
		cfg.Code = obsidian.LocalCodeConfig{
			Enabled: true,
			Go:      &obsidian.LocalCodeLangConfig{Roots: []string{"."}},
		}
	} else {
		cfg.Code = obsidian.LocalCodeConfig{Enabled: false}
	}
	require.NoError(t, obsidian.SaveLocalConfig(vault, cfg))
	return vault
}

func runCLI(t *testing.T, args []string, stdin string) (stdout string, stderr string, runErr error) {
	return runCLIWithVault(t, args, stdin, "")
}

func runCLIWithVault(t *testing.T, args []string, stdin string, forcedVault string) (stdout string, stderr string, runErr error) {
	t.Helper()
	resetBehaviorGlobals()
	resetFlags(rootCmd)
	resetCommandContexts(rootCmd)
	if strings.TrimSpace(forcedVault) != "" {
		vaultName = forcedVault
	}

	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origIn := rootCmd.InOrStdin()
	origStdout := os.Stdout
	origStderr := os.Stderr
	origStdin := os.Stdin

	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)

	var (
		outBytes []byte
		errBytes []byte
		readWG   sync.WaitGroup
		readErrs = make(chan error, 2)
	)
	readPipe := func(dst *[]byte, src *os.File) {
		defer readWG.Done()
		data, readErr := io.ReadAll(src)
		if readErr != nil {
			readErrs <- readErr
			return
		}
		*dst = data
	}
	readWG.Add(2)
	go readPipe(&outBytes, outR)
	go readPipe(&errBytes, errR)

	var inR *os.File
	if stdin != "" {
		inR, err = pipeFromString(stdin)
		require.NoError(t, err)
		os.Stdin = inR
		rootCmd.SetIn(inR)
	}

	os.Stdout = outW
	os.Stderr = errW
	rootCmd.SetOut(outW)
	rootCmd.SetErr(errW)
	rootCmd.SetArgs(args)
	runErr = rootCmd.Execute()
	rootCmd.SetArgs([]string{})

	require.NoError(t, outW.Close())
	require.NoError(t, errW.Close())
	readWG.Wait()
	close(readErrs)
	for readErr := range readErrs {
		require.NoError(t, readErr)
	}
	require.NoError(t, outR.Close())
	require.NoError(t, errR.Close())

	rootCmd.SetOut(origOut)
	rootCmd.SetErr(origErr)
	rootCmd.SetIn(origIn)
	os.Stdout = origStdout
	os.Stderr = origStderr
	os.Stdin = origStdin
	if inR != nil {
		require.NoError(t, inR.Close())
	}

	return string(outBytes), string(errBytes), runErr
}

func TestRunCLICapturesOutputBeyondPipeCapacity(t *testing.T) {
	payload := strings.Repeat("large output\n", 1<<17)
	command := &cobra.Command{
		Use: "test-large-output",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, writer := range []io.Writer{cmd.OutOrStdout(), os.Stdout, cmd.ErrOrStderr(), os.Stderr} {
				if _, err := io.WriteString(writer, payload); err != nil {
					return err
				}
			}
			return nil
		},
	}
	rootCmd.AddCommand(command)
	defer rootCmd.RemoveCommand(command)

	stdout, stderr, err := runCLI(t, []string{command.Name()}, "")
	require.NoError(t, err)
	require.Equal(t, payload+payload, stdout)
	require.Equal(t, payload+payload, stderr)
}

func resetCommandContexts(cmd *cobra.Command) {
	cmd.SetContext(context.Background())
	for _, child := range cmd.Commands() {
		resetCommandContexts(child)
	}
}

func pipeFromString(value string) (*os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, value); err != nil {
		_ = r.Close()
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

func resetBehaviorGlobals() {
	vaultName = ""

	unifiedIntent = ""
	unifiedSeeds = nil
	unifiedQueries = nil
	unifiedFiles = nil
	unifiedLimit = 25
	unifiedPack = false
	unifiedBudgetChars = contextpack.DefaultBudgetChars
	unifiedMaxPerOwner = 3
	unifiedSeedLimit = 5
	unifiedFast = false
	unifiedTimeout = 4 * time.Second
	unifiedTimings = false
	unifiedRaw = false
	unifiedJSON = false
	unifiedContinuation = ""
}

func resetFlags(cmd *cobra.Command) {
	resetFlagSet(cmd.Flags())
	resetFlagSet(cmd.PersistentFlags())
	for _, child := range cmd.Commands() {
		resetFlags(child)
	}
}

func resetFlagSet(fs *pflag.FlagSet) {
	if fs == nil {
		return
	}
	fs.VisitAll(func(f *pflag.Flag) {
		if slice, ok := f.Value.(pflag.SliceValue); ok {
			_ = slice.Replace(nil)
		} else if err := f.Value.Set(f.DefValue); err != nil {
			switch f.Value.Type() {
			case "stringSlice", "stringArray":
				_ = f.Value.Set("")
			}
		}
		f.Changed = false
	})
}

func TestIndexPreservesDisabledProvidersAndCodeConfiguration(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	disabled := &embeddings.Config{Enabled: false, Provider: "voyage", Model: "voyage-code-3"}
	vault := behaviorTestVault(t, behaviorVaultOptions{noteEmbeddings: disabled, codeEmbeddings: disabled})
	require.NoError(t, os.WriteFile(filepath.Join(vault, "main.go"), []byte("package main\nfunc Ignored() {}\n"), 0o644))
	configPath := filepath.Join(vault, ".rhizome", "config.yml")
	var normalized os.FileInfo
	for iteration := range 2 {
		_, _, err := runCLIWithVault(t, []string{"index"}, "", vault)
		require.NoError(t, err)
		cfg, err := obsidian.LoadLocalConfig(vault)
		require.NoError(t, err)
		require.False(t, cfg.Code.Enabled)
		require.False(t, cfg.NoteEmbeddings.Enabled)
		require.False(t, cfg.CodeEmbeddings.Enabled)
		require.Equal(t, "voyage", cfg.NoteEmbeddings.Provider)
		store, err := semdb.Open(obsidian.UnifiedIndexPath(vault, ""))
		require.NoError(t, err)
		symbols, err := store.SymbolsByFile(context.Background(), "main.go")
		require.NoError(t, err)
		require.Empty(t, symbols)
		notes, err := store.NotePaths(context.Background())
		require.NoError(t, err)
		require.Contains(t, notes, "note.md")
		require.NoError(t, store.Close())
		if iteration == 0 {
			require.NoError(t, os.Chtimes(configPath, time.Unix(1000000000, 0), time.Unix(1000000000, 0)))
			normalized, err = os.Stat(configPath)
			require.NoError(t, err)
		} else {
			after, err := os.Stat(configPath)
			require.NoError(t, err)
			require.True(t, os.SameFile(normalized, after), "unchanged index replaced config")
			require.Equal(t, normalized.ModTime(), after.ModTime())
		}
	}
}
