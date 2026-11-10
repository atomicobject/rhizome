//go:build integration

// Package aokb runs the real rzm binary against a disposable copy of the
// synthetic AO KB product fixture.
package aokb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ownerReason = "Managed by AO KB. Change it with the AO KB skills"

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binaryPath != "" {
		_ = os.RemoveAll(filepath.Dir(binaryPath))
	}
	os.Exit(code)
}

func rzmBinary(t *testing.T) string {
	t.Helper()
	binaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "rzm-aokb-integration-")
		if err != nil {
			binaryErr = err
			return
		}
		binaryPath = filepath.Join(dir, "rzm")
		if runtime.GOOS == "windows" {
			binaryPath += ".exe"
		}
		build := exec.Command("go", "build", "-mod=vendor", "-tags", "fts5", "-o", binaryPath, ".")
		build.Dir = fixture.RepoRoot(t)
		if output, err := build.CombinedOutput(); err != nil {
			binaryErr = fmt.Errorf("go build: %w: %s", err, output)
		}
	})
	require.NoError(t, binaryErr)
	return binaryPath
}

type aoKB struct {
	t         *testing.T
	root      string
	home      string
	authored  map[string]string
	serveLogs []string
}

func (kb *aoKB) env() []string {
	drop := map[string]bool{"RZM_REPO_DELEGATED": true, "RZM_SKIP_REPO_DELEGATE": true, appruntime.AutostartEnv: true, "HOME": true, "USERPROFILE": true, "XDG_CONFIG_HOME": true, "APPDATA": true}
	env := []string{}
	for _, value := range os.Environ() {
		if key, _, ok := strings.Cut(value, "="); ok && !drop[key] {
			env = append(env, value)
		}
	}
	config := filepath.Join(kb.home, "config")
	return append(env, "RZM_SKIP_REPO_DELEGATE=1", appruntime.AutostartEnv+"=0", "HOME="+kb.home, "USERPROFILE="+kb.home, "XDG_CONFIG_HOME="+config, "APPDATA="+config)
}

func (kb *aoKB) rzm(args ...string) (string, error) {
	kb.t.Helper()
	// Build first: the deadline is for rzm, and a cold-cache build can take minutes.
	binary := rzmBinary(kb.t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir, command.Env = kb.root, kb.env()
	output, err := command.CombinedOutput()
	return string(output), err
}

func (kb *aoKB) write(path, content string) {
	kb.t.Helper()
	full := filepath.Join(kb.root, filepath.FromSlash(path))
	require.NoError(kb.t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(kb.t, os.WriteFile(full, []byte(content), 0o644))
}

func (kb *aoKB) read(path string) string {
	kb.t.Helper()
	data, err := os.ReadFile(filepath.Join(kb.root, filepath.FromSlash(path)))
	require.NoError(kb.t, err)
	return string(data)
}

// files returns every authored byte: all files outside Rhizome's own state,
// plus the ownership declaration and installed bindings.
func (kb *aoKB) files() map[string]string {
	kb.t.Helper()
	files := map[string]string{}
	require.NoError(kb.t, filepath.WalkDir(kb.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(kb.root, path)
		require.NoError(kb.t, err)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".rhizome/") && rel != ".rhizome/source-ownership.yml" && !strings.HasPrefix(rel, ".rhizome/ontology/") {
			return nil
		}
		files[rel] = kb.read(rel)
		return nil
	}))
	return files
}

func (kb *aoKB) requireUnchanged() {
	kb.t.Helper()
	require.Equal(kb.t, kb.authored, kb.files(), "authored bytes changed")
}

// newAOKB copies the selected-product fixture, installs the starter's static
// knowledge SDL and real ownership declaration, and applies AO-side edits: an
// owned assertion links into an unowned note, an owned note has malformed
// frontmatter, and AO added a model file that the compiled bindings predate.
func newAOKB(t *testing.T, compiled bool) *aoKB {
	t.Helper()
	repo := fixture.RepoRoot(t)
	kb := &aoKB{t: t, root: filepath.Join(t.TempDir(), "vault"), home: t.TempDir()}
	require.NoError(t, os.CopyFS(kb.root, os.DirFS(filepath.Join(repo, "testdata/ao-kb-product/account"))))
	sdl, err := filepath.Glob(filepath.Join(repo, "pkg/app/cli/init/templates/starters/ao-kb/module/ontology/*.graphql"))
	require.NoError(t, err)
	if compiled {
		sdl = append(sdl, filepath.Join(repo, "testdata/ao-kb-product/expected-model.graphql"), filepath.Join(repo, "testdata/ao-kb-product/expected-knowledge.graphql"))
	}
	for _, file := range sdl {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		kb.write(".rhizome/ontology/"+filepath.Base(file), string(data))
	}
	declaration, err := os.ReadFile(filepath.Join(repo, "pkg/app/cli/init/templates/starters/ao-kb/repo/.rhizome/source-ownership.yml"))
	require.NoError(t, err)
	require.Contains(t, string(declaration), ownerReason)
	kb.write(".rhizome/source-ownership.yml", string(declaration))
	kb.write(".rhizome/config.yml", "notes:\n  includes:\n    - \"**/*.md\"\n    - \"artifacts/_assets/_model/*.yaml\"\n    - \"products/alpha/artifacts/_assets/_model/*.yaml\"\n  links: wikilinks\n")
	kb.write("notes/glossary.md", "# Glossary\n\n## Terms\n\nShared words.\n")
	kb.write("assertions/selected.md", kb.read("assertions/selected.md")+"\nSee [[glossary#Terms]].\n")
	kb.write("MOCs/broken.md", "---\nid: [unterminated\n---\nMalformed but owned.\n")
	kb.write("products/alpha/artifacts/_assets/_model/widgets.yaml", "widgets:\n  - {id: W1, name: Added after the last binding refresh}\n")
	kb.authored = kb.files()
	output, err := kb.rzm("index")
	require.NoError(t, err, output)
	kb.requireUnchanged()
	return kb
}

// serve starts the real HTTP runtime on a free port in 7880-7899.
func (kb *aoKB) serve() string {
	t := kb.t
	t.Helper()
	port := 0
	for candidate := 7880; candidate <= 7899 && port == 0; candidate++ {
		if listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(candidate)); err == nil {
			_ = listener.Close()
			port = candidate
		}
	}
	require.NotZero(t, port, "no free port in 7880-7899")
	log, err := os.Create(filepath.Join(t.TempDir(), "serve.log"))
	require.NoError(t, err)
	kb.serveLogs = append(kb.serveLogs, log.Name())
	command := exec.Command(rzmBinary(t), "serve", "--open=false", "--reuse-discovery-port=false", "--port", strconv.Itoa(port))
	command.Dir, command.Env, command.Stdout, command.Stderr = kb.root, kb.env(), log, log
	require.NoError(t, command.Start())
	exited := make(chan struct{})
	go func() { _ = command.Wait(); _ = log.Close(); close(exited) }()
	t.Cleanup(func() {
		select {
		case <-exited:
		case <-time.After(20 * time.Second):
			_ = command.Process.Kill()
			<-exited
		}
	})
	t.Cleanup(func() { fixture.StopRuntime(t, kb.root) })
	var base string
	ready := func() bool {
		client, health, err := appruntime.LiveManifest(context.Background(), kb.root)
		if err == nil && health.Ready {
			base = client.Manifest.HTTPURL
		}
		return base != ""
	}
	if !assert.Eventually(t, ready, 2*time.Minute, 200*time.Millisecond) {
		output, _ := os.ReadFile(log.Name())
		t.Fatalf("runtime never became ready:\n%s", output)
	}
	return base
}

func post(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	response, err := http.Post(url, "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer response.Body.Close()
	var decoded map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&decoded))
	return response.StatusCode, decoded
}

// SPEC-0133 R5 and the Batch 5 mutation-refusal row: with compiled bindings and
// after a failed initial binding compile, the real CLI, HTTP edit-session API
// and GraphQL API refuse native writes to every AO-owned case with the
// owner's reason, and no authored byte changes.
func TestAOKBOwnedSourcesRefuseCLIHTTPAndGraphQLWrites(t *testing.T) {
	owned := map[string]string{
		"typed assertion":                 "assertions/selected.md",
		"unselected-product fallback":     "assertions/beta-only.md",
		"malformed fallback note":         "MOCs/broken.md",
		"YAML model records":              "artifacts/_assets/_model/features.yaml",
		"model file the bindings predate": "products/alpha/artifacts/_assets/_model/widgets.yaml",
	}
	for _, state := range []struct {
		name     string
		compiled bool
	}{{"compiled bindings", true}, {"failed initial binding compile", false}} {
		t.Run(state.name, func(t *testing.T) {
			kb := newAOKB(t, state.compiled)
			cli := [][]string{
				{"note", "properties", "set", "status", "--value", "edited", "--inputs", owned["typed assertion"]},
				{"note", "tags", "add", "edited", "--inputs", owned["malformed fallback note"]},
				{"note", "tags", "add", "edited", "--inputs", owned["unselected-product fallback"]},
				{"note", "delete", "--vault", kb.root, owned["unselected-product fallback"]},
				{"note", "move", owned["YAML model records"], "notes/features.yaml"},
				{"note", "move", owned["model file the bindings predate"], "notes/widgets.yaml"},
				// Graph repair: each would rewrite the owned assertion's link.
				{"note", "move", "notes/glossary.md", "notes/terms.md"},
				{"note", "rename-heading", "notes/glossary.md", "Terms", "Words", "--apply"},
			}
			for _, args := range cli {
				t.Run("CLI "+strings.ReplaceAll(strings.Join(args, " "), kb.root, "<vault>"), func(t *testing.T) {
					kb.t = t
					output, err := kb.rzm(args...)
					require.Error(t, err, output)
					require.Contains(t, output, ownerReason)
					kb.requireUnchanged()
				})
			}

			kb.t = t
			base := kb.serve()
			session := func(t *testing.T) string {
				code, created := post(t, base+"/api/v1/edit-sessions", map[string]any{})
				require.Equal(t, http.StatusOK, code, created)
				return created["sessionId"].(string)
			}
			for name, path := range owned {
				t.Run("HTTP stage "+name, func(t *testing.T) {
					kb.t = t
					code, staged := post(t, base+"/api/v1/edit-sessions/"+session(t)+"/stage", map[string]any{"ops": []map[string]any{{"kind": "setField", "path": path, "field": "title", "value": "edited"}}})
					require.Equal(t, http.StatusBadRequest, code, staged)
					require.Contains(t, staged["error"], ownerReason)
					kb.requireUnchanged()
				})
			}
			t.Run("HTTP commit of an edit staged before ownership changed", func(t *testing.T) {
				kb.t = t
				code, staged := post(t, base+"/api/v1/edit-sessions/"+session(t)+"/stage", map[string]any{"ops": []map[string]any{{"kind": "setField", "path": "notes/glossary.md", "field": "title", "value": "Edited glossary"}}})
				require.Equal(t, http.StatusOK, code, staged)
				declaration := kb.read(".rhizome/source-ownership.yml")
				kb.write(".rhizome/source-ownership.yml", declaration+"      - notes/**\n")
				defer kb.write(".rhizome/source-ownership.yml", declaration)
				code, committed := post(t, base+"/api/v1/edit-sessions/"+staged["sessionId"].(string)+"/commit", map[string]any{})
				require.Equal(t, http.StatusBadRequest, code, committed)
				require.Contains(t, committed["error"], ownerReason)
				require.Equal(t, kb.authored["notes/glossary.md"], kb.read("notes/glossary.md"))
			})

			kb.t = t
			selections := []string{}
			for _, path := range owned {
				selections = append(selections, fmt.Sprintf("n%d: node(ref: %q) { workspace { capabilities { canEdit canEditFields canEditCollections readOnlyReason } } }", len(selections), path))
			}
			if state.compiled {
				selections = append(selections, "records: aOKBModel_feature_account(first: 5) { workspace { capabilities { canEdit canEditFields canEditCollections readOnlyReason } } }")
			}
			code, result := post(t, base+"/api/v1/graphql", map[string]any{"query": "{ " + strings.Join(selections, "\n") + " }"})
			require.Equal(t, http.StatusOK, code, result)
			require.Nil(t, result["errors"], result)
			nodes := []any{}
			for key, value := range result["data"].(map[string]any) {
				if key == "records" {
					require.Len(t, value, 2, "both selected account features")
					nodes = append(nodes, value.([]any)...)
				} else {
					nodes = append(nodes, value)
				}
			}
			want := len(owned)
			if state.compiled {
				want += 2
			}
			require.Len(t, nodes, want)
			for _, node := range nodes {
				capabilities := node.(map[string]any)["workspace"].(map[string]any)["capabilities"].(map[string]any)
				require.Equal(t, map[string]any{"canEdit": false, "canEditFields": false, "canEditCollections": false}, map[string]any{"canEdit": capabilities["canEdit"], "canEditFields": capabilities["canEditFields"], "canEditCollections": capabilities["canEditCollections"]})
				require.Contains(t, capabilities["readOnlyReason"], ownerReason)
			}
			code, result = post(t, base+"/api/v1/graphql", map[string]any{"query": `mutation { setField(path: "assertions/selected.md", field: "title", value: "edited") }`})
			require.Equal(t, http.StatusOK, code)
			require.Nil(t, result["data"])
			require.Contains(t, fmt.Sprint(result["errors"]), `Schema does not support operation type "mutation"`)
			kb.requireUnchanged()
		})
	}
}
