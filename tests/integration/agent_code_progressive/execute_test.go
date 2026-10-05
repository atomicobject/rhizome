//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/stretchr/testify/require"
)

func TestManagedCodeExecutionUsesRealOperationsWithoutSetup(t *testing.T) {
	nodePathOrSkip(t)
	binary := buildProgressiveBinary(t)
	vault := newProgressiveCodeFixture(t).vault
	before := sourceHashes(t, vault)
	program := `
const files = await rzm.files({inputs: ["notes/guide.md"], includeContent: true, dedupe: false});
const context = await rzm.fileContext({files: ["src/example.py"], budgetChars: 4000});
return {filesOK: files.ok, contextOK: context.ok, session: files.payload?.sessionId,
  paths: files.payload?.files.map(file => file.path),
  containsEvidence: files.payload?.files[0].content.includes("source note remains byte stable")};
`
	// The invoking directory differs from the selected vault; the host must bind
	// both Node and the managed Rhizome process to the selected canonical root.
	result := runProgressiveCommand(t, binary, filepath.Join(vault, "src"), "agent", "code", "execute",
		"--vault", vault, "--session-id", "managed-session", "--code", program)
	require.NoError(t, result.err, "stdout=%s stderr=%s", result.stdout, result.stderr)
	var envelope agentcode.ExecuteResult
	require.NoError(t, json.Unmarshal([]byte(result.stdout), &envelope))
	require.True(t, envelope.OK, result.stdout)
	require.JSONEq(t, `{"filesOK":true,"contextOK":true,"session":"managed-session","paths":["notes/guide.md"],"containsEvidence":true}`, string(envelope.Result))
	require.Equal(t, before, sourceHashes(t, vault))
	for path := range fileSnapshot(t, vault) {
		require.NotContains(t, path, "index.mjs")
		require.NotContains(t, path, "connection.json")
	}

	changed := "# Guide\n\nNew evidence between independent executions.\n"
	require.NoError(t, os.WriteFile(filepath.Join(vault, "notes/guide.md"), []byte(changed), 0o644))
	result = runProgressiveCommand(t, binary, vault, "agent", "code", "execute", "--session-id", "managed-session", "--code",
		`const read = await rzm.files({inputs:["notes/guide.md"], includeContent:true, dedupe:false}); return read.payload.files[0].content;`)
	require.NoError(t, result.err, result.stderr)
	require.NoError(t, json.Unmarshal([]byte(result.stdout), &envelope))
	var actual string
	require.NoError(t, json.Unmarshal(envelope.Result, &actual))
	require.Equal(t, changed, actual)
}

func TestManagedCodeExecutionPreservesRealMutationAndValidationOutcomes(t *testing.T) {
	nodePathOrSkip(t)
	binary := buildProgressiveBinary(t)
	vault := newProgressiveCodeFixture(t).vault
	move := `const moved = await rzm.noteMove({source:"notes/guide.md", target:"notes/moved.md", updateBacklinks:true}); return moved;`
	denied := runProgressiveCommand(t, binary, vault, "agent", "code", "execute", "--code", move)
	require.NoError(t, denied.err, "stdout=%s stderr=%s", denied.stdout, denied.stderr) // A returned domain failure is not a script failure.
	var envelope agentcode.ExecuteResult
	require.NoError(t, json.Unmarshal([]byte(denied.stdout), &envelope))
	require.True(t, envelope.OK)
	var outcome struct {
		OK         bool `json:"ok"`
		ExitCode   int  `json:"exitCode"`
		Diagnostic struct {
			Code string `json:"code"`
		} `json:"diagnostic"`
	}
	require.NoError(t, json.Unmarshal(envelope.Result, &outcome))
	require.False(t, outcome.OK)
	require.Equal(t, "write_requires_read_write", outcome.Diagnostic.Code)
	require.FileExists(t, filepath.Join(vault, "notes/guide.md"))
	require.NoFileExists(t, filepath.Join(vault, "notes/moved.md"))

	applied := runProgressiveCommand(t, binary, vault, "agent", "code", "execute", "--read-write", "--code", move)
	require.NoError(t, applied.err, applied.stderr)
	require.NoError(t, json.Unmarshal([]byte(applied.stdout), &envelope))
	require.NoError(t, json.Unmarshal(envelope.Result, &outcome))
	require.True(t, envelope.OK)
	require.True(t, outcome.OK, applied.stdout)
	require.NoFileExists(t, filepath.Join(vault, "notes/guide.md"))
	require.FileExists(t, filepath.Join(vault, "notes/moved.md"))

	require.NoError(t, os.WriteFile(filepath.Join(vault, "notes/broken.md"), []byte("# Broken\n\n[[missing-note]]\n"), 0o644))
	validation := runProgressiveCommand(t, binary, vault, "agent", "code", "execute", "--code",
		`return await rzm.validate({action:"run", selector:"broken-links"});`)
	require.NoError(t, validation.err, validation.stderr)
	require.NoError(t, json.Unmarshal([]byte(validation.stdout), &envelope))
	require.NoError(t, json.Unmarshal(envelope.Result, &outcome))
	require.True(t, envelope.OK)
	require.False(t, outcome.OK)
	require.Equal(t, 1, outcome.ExitCode)

	failed := runProgressiveCommand(t, binary, vault, "agent", "code", "execute", "--read-write", "--code",
		`const moved = await rzm.noteMove({source:"notes/moved.md", target:"notes/final.md", updateBacklinks:true});
if (!moved.ok) throw new Error(JSON.stringify(moved)); throw new Error("failed after completed write");`)
	require.Error(t, failed.err)
	require.Empty(t, failed.stderr, "script failures must not print Cobra usage or prose")
	require.NoError(t, json.Unmarshal([]byte(failed.stdout), &envelope))
	require.False(t, envelope.OK)
	require.FileExists(t, filepath.Join(vault, "notes/final.md"))
	require.NoFileExists(t, filepath.Join(vault, "notes/moved.md"))
}
