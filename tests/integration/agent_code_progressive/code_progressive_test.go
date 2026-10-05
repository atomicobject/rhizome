//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestProgressiveCodeDiscoveryIsSelectiveAndRuntimeFree(t *testing.T) {
	fixture := newProgressiveCodeFixture(t)
	binary := buildProgressiveBinary(t)
	before := fileSnapshot(t, fixture.vault)

	surface := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "surface", "--vault", fixture.vault)
	require.NoError(t, surface.err, "stdout=%s stderr=%s", surface.stdout, surface.stderr)
	require.Empty(t, surface.stderr)
	var surfacePayload struct {
		FormatVersion string             `json:"formatVersion"`
		VersionHash   string             `json:"versionHash"`
		Operations    []surfaceOperation `json:"operations"`
	}
	require.NoError(t, json.Unmarshal([]byte(surface.stdout), &surfacePayload))
	require.Equal(t, agentcode.FormatVersion, surfacePayload.FormatVersion)
	require.NotEmpty(t, surfacePayload.VersionHash)
	require.Subset(t, surfaceOperationNames(surfacePayload.Operations), []string{"file_context", "files", "semantic_query", "validate", "note_move", "next_id", "ontology_query", "query_recipe"})
	for _, operation := range surfacePayload.Operations {
		require.NotEmpty(t, operation.Summary)
		require.NotEmpty(t, operation.Effects)
		require.Empty(t, operation.InputSchema)
		require.Empty(t, operation.OutputSchema)
	}
	require.Contains(t, surface.stdout, "semantic_query")
	require.NotContains(t, surface.stdout, "inputSchema")
	require.NotContains(t, surface.stdout, "outputSchema")
	require.Equal(t, before, fileSnapshot(t, fixture.vault), "surface must not write the fixture")

	describe := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "describe",
		"--operation", "files", "--operation", "file_context", "--vault", fixture.vault)
	require.NoError(t, describe.err, "stdout=%s stderr=%s", describe.stdout, describe.stderr)
	require.Empty(t, describe.stderr)
	var describePayload struct {
		FormatVersion string              `json:"formatVersion"`
		ContractHash  string              `json:"contractHash"`
		Selected      []string            `json:"selected"`
		Operations    []describeOperation `json:"operations"`
	}
	require.NoError(t, json.Unmarshal([]byte(describe.stdout), &describePayload))
	require.Equal(t, agentcode.DescribeFormatVersion, describePayload.FormatVersion)
	require.NotEmpty(t, describePayload.ContractHash)
	require.Contains(t, describe.stdout, `"invocation"`)
	require.Contains(t, describe.stdout, `"outcome"`)
	require.NotContains(t, surface.stdout, `"invocation"`)
	require.NotContains(t, surface.stdout, `"outcome"`)
	require.Equal(t, []string{"file_context", "files"}, describePayload.Selected)
	require.Equal(t, []string{"file_context", "files"}, describeOperationNames(describePayload.Operations))
	for _, operation := range describePayload.Operations {
		require.Equal(t, strings.ReplaceAll(operation.Name, "_", "-"), operation.AgentCommand)
		require.NotEmpty(t, operation.InputSchema)
		require.NotEmpty(t, operation.OutputSchema)
	}
	require.Equal(t, before, fileSnapshot(t, fixture.vault), "describe must not write the fixture")
	require.NoFileExists(t, filepath.Join(fixture.vault, ".rhizome", "db.sqlite"))
	require.NoDirExists(t, filepath.Join(fixture.vault, ".rhizome", "agent"))

	start := runProgressiveCommand(t, binary, fixture.vault, "agent", "start", "--vault", fixture.vault)
	require.NoError(t, start.err, "stdout=%s stderr=%s", start.stdout, start.stderr)
	require.Empty(t, start.stderr)
	var startPayload map[string]any
	require.NoError(t, json.Unmarshal([]byte(start.stdout), &startPayload))
	require.NotContains(t, startPayload, "codeMode")
	require.NotContains(t, start.stdout, "inputSchema")
	require.NotContains(t, start.stdout, "outputSchema")
}

func TestProgressiveCodeGenerationAndNodeParity(t *testing.T) {
	node := nodePathOrSkip(t)
	fixture := newProgressiveCodeFixture(t)
	binary := buildProgressiveBinary(t)
	output := filepath.Join(t.TempDir(), "generated client")
	require.NoError(t, os.MkdirAll(output, 0o755))
	sentinel := filepath.Join(output, "host-owned.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep me\n"), 0o644))

	firstResult := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "generate",
		"--operation", "files", "--operation", "file_context", "--output", output, "--vault", fixture.vault)
	require.NoError(t, firstResult.err, "stdout=%s stderr=%s", firstResult.stdout, firstResult.stderr)
	require.Empty(t, firstResult.stderr)
	var first agentcode.Manifest
	require.NoError(t, json.Unmarshal([]byte(firstResult.stdout), &first))
	require.Equal(t, "rhizome-agent-code-manifest-v1", first.Format)
	require.Equal(t, []string{"file_context", "files"}, first.Selected)
	require.False(t, first.Reused)
	require.True(t, filepath.IsAbs(first.ModulePath))
	require.True(t, filepath.IsAbs(first.DeclarationsPath))
	require.True(t, filepath.IsAbs(first.ExecutablePath))
	require.True(t, filepath.IsAbs(first.VaultPath))
	require.Equal(t, binary, first.ExecutablePath)
	require.Equal(t, resolvedAbsolutePath(t, fixture.vault), filepath.Clean(first.VaultPath))
	require.Equal(t, filepath.Join(output, first.ArtifactHash, "index.mjs"), first.ModulePath)
	require.Equal(t, filepath.Join(output, first.ArtifactHash, "index.d.mts"), first.DeclarationsPath)
	require.FileExists(t, first.ModulePath)
	require.FileExists(t, first.DeclarationsPath)
	// CI's integration job has no web/node_modules, so the tsc check below skips there.
	declarations := string(mustReadFile(t, first.DeclarationsPath))
	require.Contains(t, declarations, "readWrite?: boolean")
	require.Contains(t, declarations, "close(): Promise<void>")
	if tsc, ok := progressiveTypeScriptCompiler(t); ok {
		compileProgressiveDeclarations(t, tsc, first.ModulePath, binary, resolvedAbsolutePath(t, fixture.vault))
	} else {
		t.Log("TypeScript compiler is unavailable; generated declaration consumption was not checked")
	}
	for _, stored := range []string{filepath.Join(output, first.ArtifactHash, "manifest.json"), filepath.Join(output, "manifest.json")} {
		var persisted map[string]any
		require.NoError(t, json.Unmarshal(mustReadFile(t, stored), &persisted), stored)
		require.NotContains(t, persisted, "executablePath", stored)
		require.NotContains(t, persisted, "vaultPath", stored)
	}
	artifactBytes := fileSnapshot(t, filepath.Join(output, first.ArtifactHash))
	for _, path := range []string{first.ModulePath, first.DeclarationsPath, filepath.Join(output, first.ArtifactHash, "manifest.json")} {
		contents := mustReadFile(t, path)
		require.NotContains(t, string(contents), "progressive-session")
		require.NotContains(t, string(contents), fixture.vault)
		require.NotContains(t, string(contents), output)
	}
	require.FileExists(t, sentinel)

	secondResult := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "generate",
		"--operation", "file_context", "--operation", "files", "--output", output, "--vault", fixture.vault)
	require.NoError(t, secondResult.err, "stdout=%s stderr=%s", secondResult.stdout, secondResult.stderr)
	require.Empty(t, secondResult.stderr)
	var second agentcode.Manifest
	require.NoError(t, json.Unmarshal([]byte(secondResult.stdout), &second))
	require.True(t, second.Reused)
	require.Equal(t, first.ContractHash, second.ContractHash)
	require.Equal(t, first.ArtifactHash, second.ArtifactHash)
	require.Equal(t, first.ModulePath, second.ModulePath)
	require.Equal(t, first.DeclarationsPath, second.DeclarationsPath)
	require.Equal(t, artifactBytes, fileSnapshot(t, filepath.Join(output, second.ArtifactHash)))
	require.FileExists(t, sentinel)

	sessionID := "progressive-session"
	vaultPath := resolvedAbsolutePath(t, fixture.vault)
	beforeHashes := sourceHashes(t, fixture.vault)
	directFiles := runProgressiveCommand(t, binary, fixture.vault,
		"agent", "files", "--vault", vaultPath, "--session-id", sessionID,
		"--input", "notes/guide.md", "--include-content", "true",
		"--max-depth", "0", "--budget-chars", "60000")
	require.NoError(t, directFiles.err, "stdout=%s stderr=%s", directFiles.stdout, directFiles.stderr)
	directContext := runProgressiveCommand(t, binary, fixture.vault,
		"agent", "file-context", "--vault", vaultPath, "--session-id", sessionID,
		"--file", "src/example.py", "--ensure-link-targets", "never", "--intent", "understand the fixture",
		"--budget-chars", "60000")
	require.NoError(t, directContext.err, "stdout=%s stderr=%s", directContext.stdout, directContext.stderr)

	inputPath := filepath.Join(t.TempDir(), "client-input.json")
	input := map[string]any{
		"files": map[string]any{
			"inputs":         []string{"notes/guide.md"},
			"includeContent": true,
			"budgetChars":    60000,
		},
		"fileContext": map[string]any{
			"files":       []string{"src/example.py"},
			"intent":      "understand the fixture",
			"budgetChars": 60000,
		},
	}
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(inputPath, inputJSON, 0o644))
	clientScript := filepath.Join(t.TempDir(), "client.mjs")
	require.NoError(t, os.WriteFile(clientScript, []byte(clientScriptSource), 0o644))
	// This test pins CLI-versus-client parity of the generated artifacts and
	// dispatch. Run the client in-process like the direct commands above: an
	// auto-started runtime would index the vault between the two runs and
	// legitimately change the file_context enrichment. Runtime-hosted execution
	// has its own coverage (TestPersistentClient* and the cmd unit tests).
	clientResult := runNode(t, node, fixture.vault, map[string]string{
		"PROGRESSIVE_MODULE":    first.ModulePath,
		"PROGRESSIVE_INPUT":     inputPath,
		"PROGRESSIVE_BINARY":    binary,
		"PROGRESSIVE_VAULT":     vaultPath,
		"PROGRESSIVE_SESSION":   "progressive-client-session",
		"RZM_RUNTIME_AUTOSTART": "0",
	}, clientScript)
	require.NoError(t, clientResult.err, "stdout=%s stderr=%s", clientResult.stdout, clientResult.stderr)
	var clientPayload struct {
		Files       clientOutcome `json:"files"`
		FileContext clientOutcome `json:"fileContext"`
	}
	require.NoError(t, json.Unmarshal([]byte(clientResult.stdout), &clientPayload))
	require.True(t, clientPayload.Files.OK)
	require.True(t, clientPayload.FileContext.OK)
	require.Equal(t, 0, clientPayload.Files.ExitCode)
	require.Equal(t, 0, clientPayload.FileContext.ExitCode)
	require.Equal(t, directFiles.stderr, clientPayload.Files.Stderr)
	require.Equal(t, directContext.stderr, clientPayload.FileContext.Stderr)
	require.Equal(t, normalizeSessionPayload(t, directFiles.stdout), normalizeSessionPayload(t, string(clientPayload.Files.Payload)))
	require.Equal(t, normalizeSessionPayload(t, directContext.stdout), normalizeSessionPayload(t, string(clientPayload.FileContext.Payload)))

	var filesPayload struct {
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(directFiles.stdout), &filesPayload))
	contents := make(map[string]string, len(filesPayload.Files))
	for _, file := range filesPayload.Files {
		contents[file.Path] = file.Content
	}
	require.Equal(t, string(mustReadFile(t, filepath.Join(fixture.vault, "notes/guide.md"))), contents["notes/guide.md"])
	require.Equal(t, beforeHashes, sourceHashes(t, fixture.vault), "CLI/client reads must not modify note or code sources")
}

const clientScriptSource = `import { readFile } from "node:fs/promises";

const input = JSON.parse(await readFile(process.env.PROGRESSIVE_INPUT, "utf8"));
const { createClient } = await import(process.env.PROGRESSIVE_MODULE);
const client = createClient({
  executablePath: process.env.PROGRESSIVE_BINARY,
  vaultPath: process.env.PROGRESSIVE_VAULT,
  sessionId: process.env.PROGRESSIVE_SESSION,
});
const files = await client.files(input.files);
const fileContext = await client.fileContext(input.fileContext);
await client.close();
process.stdout.write(JSON.stringify({ files, fileContext }));
`

type progressiveCodeFixture struct {
	vault string
}

func newProgressiveCodeFixture(t *testing.T) progressiveCodeFixture {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	// Commands under test auto-start a headless vault runtime for this vault
	// (SPEC-0104). Stop it with the test so runtimes do not outlive their
	// temporary vaults for the idle timeout.
	t.Cleanup(func() { fixture.StopRuntime(t, vault) })
	files := map[string]string{
		".rhizome/config.yml": `notes:
  includes:
    - "**/*.md"
  links: wikilinks
code:
  python:
    roots:
      - src
fileContext:
  docPatterns:
    - CONTEXT.md
  maxEmptyLevels: 2
  contextBudget: 60000
`,
		"CONTEXT.md":     "# Fixture context\n\nThis context governs the example module.\n",
		"notes/guide.md": "# Fixture guide\n\nThe source note remains byte stable.\n",
		"src/example.py": "\"\"\"Example code for progressive client parity.\"\"\"\n\ndef greet(name):\n    return f\"hello {name}\"\n\n# Docs: [[notes/guide]]\n",
	}
	for rel, contents := range files {
		path := filepath.Join(vault, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	}
	return progressiveCodeFixture{vault: vault}
}

var progressiveBinary struct {
	once   sync.Once
	path   string
	output []byte
	err    error
}

func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		// CI used to swallow this package's Windows failures. With the harness
		// fixed, a different test fails each run: locked or renamed db.sqlite,
		// link-hygiene output. Code mode on Windows needs product work first.
		fmt.Println("skipping agent_code_progressive on Windows: code mode is not yet verified there")
		os.Exit(0)
	}
	code := m.Run()
	if progressiveBinary.path != "" {
		_ = os.RemoveAll(filepath.Dir(progressiveBinary.path))
	}
	os.Exit(code)
}

func buildProgressiveBinary(t *testing.T) string {
	t.Helper()
	progressiveBinary.once.Do(func() {
		dir, err := os.MkdirTemp("", "rzm-progressive-")
		if err != nil {
			progressiveBinary.err = err
			return
		}
		name := "rzm"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		progressiveBinary.path = filepath.Join(dir, name)
		command := exec.Command("go", "build", "-mod=vendor", "-tags", "fts5", "-o", progressiveBinary.path, ".")
		command.Dir = fixture.RepoRoot(t)
		command.Env = testEnvironment(nil)
		progressiveBinary.output, progressiveBinary.err = command.CombinedOutput()
	})
	require.NoError(t, progressiveBinary.err, "go build output: %s", progressiveBinary.output)
	return progressiveBinary.path
}

type progressiveProcessResult struct {
	stdout string
	stderr string
	err    error
}

func runProgressiveCommand(t *testing.T, binary, dir string, args ...string) progressiveProcessResult {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Env = testEnvironment(nil)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return progressiveProcessResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func runNode(t *testing.T, node, dir string, values map[string]string, script string) progressiveProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, script)
	command.Dir = dir
	command.Env = testEnvironment(values)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return progressiveProcessResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func testEnvironment(extra map[string]string) []string {
	remove := map[string]bool{
		"RZM_REPO_DELEGATED": true, "RZM_SKIP_REPO_DELEGATE": true,
		"PROGRESSIVE_MODULE": true, "PROGRESSIVE_INPUT": true, "PROGRESSIVE_BINARY": true,
		"PROGRESSIVE_VAULT": true, "PROGRESSIVE_SESSION": true,
	}
	env := make([]string, 0, len(os.Environ())+len(extra)+1)
	for _, value := range os.Environ() {
		key, _, ok := strings.Cut(value, "=")
		if !ok || remove[key] {
			continue
		}
		env = append(env, value)
	}
	env = append(env, "RZM_SKIP_REPO_DELEGATE=1")
	for key, value := range extra {
		if key == "PROGRESSIVE_MODULE" {
			// Node's ESM loader rejects a bare Windows path such as C:\...
			path := filepath.ToSlash(value)
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			value = (&url.URL{Scheme: "file", Path: path}).String()
		}
		env = append(env, key+"="+value)
	}
	return env
}

func nodePathOrSkip(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is unavailable; skipping real generated-client integration")
	}
	return node
}

func progressiveTypeScriptCompiler(t *testing.T) (string, bool) {
	t.Helper()
	path := filepath.Join(fixture.RepoRoot(t), "web", "node_modules", "typescript", "bin", "tsc")
	info, err := os.Stat(path)
	if err != nil {
		t.Logf("TypeScript compiler is unavailable at %s: %v", path, err)
		return "", false
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		t.Logf("TypeScript compiler is not executable at %s", path)
		return "", false
	}
	return path, true
}

func compileProgressiveDeclarations(t *testing.T, tsc, module, binary, vault string) {
	t.Helper()
	consumer := filepath.Join(t.TempDir(), "consumer.mts")
	moduleJSON, err := json.Marshal(module)
	require.NoError(t, err)
	binaryJSON, err := json.Marshal(binary)
	require.NoError(t, err)
	vaultJSON, err := json.Marshal(vault)
	require.NoError(t, err)
	source := "import { createClient } from " + string(moduleJSON) + ";\n" +
		"const client = createClient({ executablePath: " + string(binaryJSON) + ", vaultPath: " + string(vaultJSON) + ", readWrite: true });\n" +
		"await client.files({ inputs: [\"notes/guide.md\"], includeContent: true, limit: 1, budgetChars: 1000 });\n" +
		"await client.fileContext({ files: [\"src/example.py\"], intent: \"understand the fixture\", budgetChars: 1000 });\n" +
		"// @ts-expect-error generated operation inputs reject mutation flags\n" +
		"await client.files({ inputs: [\"notes/guide.md\"], includeContent: true, mutation: \"unexpected\" });\n" +
		"await client.close();\n"
	require.NoError(t, os.WriteFile(consumer, []byte(source), 0o644))
	command := exec.Command(tsc, "--noEmit", "--strict", "--target", "ES2022", "--module", "NodeNext", "--moduleResolution", "NodeNext", "--lib", "ES2022,DOM", consumer)
	command.Env = testEnvironment(nil)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "tsc output: %s", output)
}

type surfaceOperation struct {
	Name         string          `json:"name"`
	Summary      string          `json:"summary"`
	Effects      []string        `json:"effects"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

type describeOperation struct {
	Name         string          `json:"name"`
	AgentCommand string          `json:"agentCommand"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

func surfaceOperationNames(operations []surfaceOperation) []string {
	names := make([]string, 0, len(operations))
	for _, operation := range operations {
		names = append(names, operation.Name)
	}
	return names
}

func describeOperationNames(operations []describeOperation) []string {
	names := make([]string, 0, len(operations))
	for _, operation := range operations {
		names = append(names, operation.Name)
	}
	return names
}

func normalizeSessionPayload(t *testing.T, payload string) any {
	t.Helper()
	var value any
	require.NoError(t, json.Unmarshal([]byte(payload), &value))
	return removeSessionIDs(value)
}

func removeSessionIDs(value any) any {
	switch current := value.(type) {
	case map[string]any:
		copy := make(map[string]any, len(current))
		for key, child := range current {
			if key == "sessionId" {
				continue
			}
			copy[key] = removeSessionIDs(child)
		}
		return copy
	case []any:
		for i, child := range current {
			current[i] = removeSessionIDs(child)
		}
	}
	return value
}

func fileSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			// The live runtime creates and removes transient files such as
			// .rhizome/index.lock while the walk runs (seen on CI).
			return nil
		}
		if err != nil {
			return err
		}
		sum := sha256.Sum256(contents)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	}))
	return snapshot
}

func sourceHashes(t *testing.T, vault string) map[string]string {
	t.Helper()
	hashes := map[string]string{}
	for _, rel := range []string{"CONTEXT.md", "notes/guide.md", "src/example.py"} {
		contents := mustReadFile(t, filepath.Join(vault, filepath.FromSlash(rel)))
		sum := sha256.Sum256(contents)
		hashes[rel] = hex.EncodeToString(sum[:])
	}
	return hashes
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return contents
}

func resolvedAbsolutePath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}

type clientOutcome struct {
	OK       bool            `json:"ok"`
	ExitCode int             `json:"exitCode"`
	Payload  json.RawMessage `json:"payload"`
	Stderr   string          `json:"stderr"`
}
