package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// This goes through the same live runtime, watcher and HTTP GraphQL admission
// used by serve. The provider holds a physical request, rather than sleeping
// in a store or substituting a fake indexing lane.
func TestLiveIndexingPublicReadsAdvanceWhileEmbeddingBlocked(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("provider_failure=%t", fail), func(t *testing.T) {
			live, httpServer, provider, write := newPublicReadLiveFixture(t, fail)
			const firstPath = "specs/100-demo/first.md"
			const secondPath = "specs/100-demo/second.md"
			events, closeEvents := subscribePublicReadEvents(t, httpServer.URL)
			defer closeEvents()
			provider.block()
			write(firstPath, "after", "first new", "first-new")
			live.Hub().EmitHintPaths([]string{firstPath})
			select {
			case <-provider.started:
			case <-time.After(10 * time.Second):
				t.Fatal("live edit did not reach embedding provider")
			}
			// Reads must remain usable during the actual blocked physical call.
			require.Equal(t, "Plan", publicReadNode(t, httpServer.URL, "specs/100-demo/plan.md")["resolvedType"])
			require.Equal(t, "After", publicReadNode(t, httpServer.URL, firstPath)["resolvedType"])
			detail := publicReadNode(t, httpServer.URL, firstPath+"#^first-new")
			require.Equal(t, "Detail", detail["resolvedType"])
			require.Contains(t, fmt.Sprint(detail), "first new detail")
			awaitPublicReadCommit(t, events, firstPath, func() {
				require.Contains(t, fmt.Sprint(publicReadNode(t, httpServer.URL, firstPath)), "first new")
			})
			// A second path must publish while the first request remains held.
			write(secondPath, "after", "second new", "second-new")
			live.Hub().EmitHintPaths([]string{secondPath})
			awaitPublicReadCommit(t, events, secondPath, func() {
				require.Equal(t, "After", publicReadNode(t, httpServer.URL, secondPath)["resolvedType"])
				require.Contains(t, fmt.Sprint(publicReadNode(t, httpServer.URL, secondPath+"#^second-new")), "second new detail")
			})
			provider.release()
			select {
			case failed := <-provider.finished:
				require.Equal(t, fail, failed)
			case <-time.After(5 * time.Second):
				t.Fatal("held provider request did not finish")
			}
			require.Equal(t, "Plan", publicReadNode(t, httpServer.URL, "specs/100-demo/plan.md")["resolvedType"])
			require.Equal(t, "After", publicReadNode(t, httpServer.URL, secondPath)["resolvedType"])
		})
	}
}

func newPublicReadLiveFixture(t *testing.T, fail bool) (*bootstrap.LiveRuntime, *httptest.Server, *publicReadEmbeddingEndpoint, func(string, string, string, string)) {
	t.Helper()
	t.Setenv("RHIZOME_OPENAI_API_KEY", "synthetic-public-read-fixture")
	fixture := prepareOntologyFixtureVault(t)
	schemaPath := filepath.Join(fixture.root, ".rhizome/ontology/schema.graphql")
	schema, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	schema = append(schema, []byte(`
type Before @node(matches: ["tag:before"]) {
 summary: String
 detail: Detail @contains(level: H2, heading: "Detail")
}
type After @node(matches: ["tag:after"]) {
 summary: String
 detail: Detail @contains(level: H2, heading: "Detail")
}
type Detail implements Section @node(locator: EMBEDDED) { summary: String @field }
`)...)
	require.NoError(t, os.WriteFile(schemaPath, schema, 0o644))
	write := func(path, tag, summary, fragment string) {
		t.Helper()
		body := fmt.Sprintf("---\ntags: [%s]\nsummary: %s\n---\n# Changing\n\n## Detail\nsummary:: %s detail\n^%s\n", tag, summary, summary, fragment)
		require.NoError(t, os.WriteFile(filepath.Join(fixture.root, path), []byte(body), 0o644))
	}
	const firstPath = "specs/100-demo/first.md"
	const secondPath = "specs/100-demo/second.md"
	write(firstPath, "before", "first old", "first-old")
	write(secondPath, "before", "second old", "second-old")
	_, err = ontology.EnsureFreshRuntimeWithStore(t.Context(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	require.NoError(t, fixture.intelStore.Close())

	provider := newPublicReadEmbeddingEndpoint(t, fail)
	config := fmt.Sprintf("notes: {}\ncode:\n  enabled: true\nnoteEmbeddings:\n  enabled: true\n  provider: openai\n  model: synthetic-public-read\n  dimensions: 3\n  endpoint: %s\ncodeEmbeddings:\n  enabled: false\n", provider.server.URL)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".rhizome/config.yml"), []byte(config), 0o644))
	live, err := bootstrap.NewLiveRuntime(t.Context(), bootstrap.LiveOptions{VaultName: fixture.root, DisableSessionStore: true})
	require.NoError(t, err)
	t.Cleanup(func() { provider.release(); require.NoError(t, live.Close()) })
	ready, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	require.NoError(t, live.WaitForCodeIndex(ready))
	require.NoError(t, live.WaitForSemantic(ready))
	require.True(t, live.WaitForLeader(ready))
	runtime := &Runtime{Live: live}
	runtime.EnableIndexGate()
	runtime.MarkIndexReady()
	server, err := NewServer(t.Context(), Config{
		Vault: live.Vault, VaultDef: live.VaultDef, VaultPath: live.VaultPath,
		Cache: live.Cache(), Runtime: runtime, NoteMetadata: live.NoteMetadataIndexer(), NotePathOwned: live.NotePathOwned,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	live.SetGlobalEventSink(server.NotifyGlobalEvent)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	require.Eventually(t, func() bool {
		if live.LiveHealth().LastCompletedEpoch == nil || live.Lane() == nil || live.Lane().Status().Busy || provider.inflight.Load() != 0 {
			return false
		}
		// The baseline has no durable scheduler table. On the integrated path,
		// wait for every startup ticket, including backoff/unready work, to drain.
		var hasDerivedWork bool
		err := live.IntelStore().DB().QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='derived_work')`).Scan(&hasDerivedWork)
		require.NoError(t, err)
		if !hasDerivedWork {
			return true
		}
		var pending int
		require.NoError(t, live.IntelStore().DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM derived_work`).Scan(&pending))
		return pending == 0
	}, 15*time.Second, 20*time.Millisecond, "initial live resync did not finish: %+v", live.LiveHealth())
	node := publicReadNode(t, httpServer.URL, firstPath)
	require.Equal(t, "Before", node["resolvedType"])
	require.Equal(t, "Detail", publicReadNode(t, httpServer.URL, firstPath+"#^first-old")["resolvedType"])
	return live, httpServer, provider, write
}

// Measure from the actual filesystem write, including FSNotify, debounce, the
// production watcher clock and repeated-path cooldown. No hint bypasses intake.
func TestLiveIndexingPublicHealthyProviderEventLatency(t *testing.T) {
	_, httpServer, _, write := newPublicReadLiveFixture(t, false)
	events, closeEvents := subscribePublicReadEvents(t, httpServer.URL)
	defer closeEvents()
	const path = "specs/100-demo/first.md"
	for _, marker := range []string{"first rapid", "second rapid"} {
		start := time.Now()
		write(path, "after", marker, strings.ReplaceAll(marker, " ", "-"))
		deadline := time.NewTimer(30 * time.Second)
		found := false
		for !found {
			select {
			case event, ok := <-events:
				require.True(t, ok)
				// index.changed is the baseline's only readiness signal. The integrated
				// structural path should announce node.changed before derived work.
				if event.Kind != GlobalEventNodeChanged && event.Kind != GlobalEventIndexChanged {
					continue
				}
				if fields, ok := event.Data.(map[string]any); event.Kind == GlobalEventIndexChanged && ok && fields["domains"] != nil {
					continue
				}
				data, err := json.Marshal(event.Data)
				require.NoError(t, err)
				var changed NodeChangedEventData
				require.NoError(t, json.Unmarshal(data, &changed))
				for _, candidate := range changed.Paths {
					if candidate == path {
						node := publicReadNode(t, httpServer.URL, path)
						if !strings.Contains(fmt.Sprint(node), marker) {
							continue
						}
						elapsed := time.Since(start)
						t.Logf("file-write to %s for %q: %s", event.Kind, marker, elapsed)
						if event.Kind == GlobalEventNodeChanged {
							require.Less(t, elapsed, 2*time.Second, "structural readiness must bypass the semantic cooldown")
						}
						found = true
					}
				}
			case <-deadline.C:
				t.Fatalf("no public committed event for %q", marker)
			}
		}
		deadline.Stop()
	}
}

type publicReadEmbeddingEndpoint struct {
	server   *httptest.Server
	mu       sync.Mutex
	held     bool
	released chan struct{}
	started  chan struct{}
	finished chan bool
	once     sync.Once
	fail     bool
	inflight atomic.Int64
}

func newPublicReadEmbeddingEndpoint(t *testing.T, fail bool) *publicReadEmbeddingEndpoint {
	t.Helper()
	p := &publicReadEmbeddingEndpoint{released: make(chan struct{}), started: make(chan struct{}, 1), finished: make(chan bool, 32), fail: fail}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.inflight.Add(1)
		defer p.inflight.Add(-1)
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		p.mu.Lock()
		held := p.held
		p.mu.Unlock()
		if held {
			select {
			case p.started <- struct{}{}:
			default:
			}
			select {
			case <-p.released:
			case <-r.Context().Done():
				return
			}
			if p.fail {
				http.Error(w, "synthetic provider failure", http.StatusBadRequest)
				select {
				case p.finished <- true:
				default:
				}
				return
			}
		}
		data := make([]map[string]any, len(request.Input))
		for i := range data {
			data[i] = map[string]any{"index": i, "embedding": []float64{1, 0, 0}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		if held {
			select {
			case p.finished <- false:
			default:
			}
		}
	}))
	t.Cleanup(func() { p.release(); p.server.Close() })
	return p
}
func (p *publicReadEmbeddingEndpoint) block()   { p.mu.Lock(); p.held = true; p.mu.Unlock() }
func (p *publicReadEmbeddingEndpoint) release() { p.once.Do(func() { close(p.released) }) }

func publicReadNode(t *testing.T, baseURL, ref string) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": `query($ref: String!) { node(ref: $ref) { path resolvedType workspace { version fields { name values } } } }`, "variables": map[string]string{"ref": ref}})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/graphql", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	start := time.Now()
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err, "public navigation blocked for %s", ref)
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(payload))
	var result struct {
		Data   struct{ Node map[string]any }
		Errors any
	}
	require.NoError(t, json.Unmarshal(payload, &result))
	require.Nil(t, result.Errors, string(payload))
	require.NotNil(t, result.Data.Node, string(payload))
	t.Logf("public node %s: %s", ref, time.Since(start))
	return result.Data.Node
}

func subscribePublicReadEvents(t *testing.T, baseURL string) (<-chan GlobalEvent, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 5 * time.Second
	client := &http.Client{Transport: transport}
	t.Cleanup(func() { cancel(); transport.CloseIdleConnections() })
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/events", nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	if err != nil {
		cancel()
		transport.CloseIdleConnections()
		require.NoError(t, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(t, http.StatusOK, response.StatusCode)
	events := make(chan GlobalEvent, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(events)
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
				var event GlobalEvent
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) == nil {
					select {
					case events <- event:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	var once sync.Once
	closeStream := func() {
		once.Do(func() {
			cancel()
			_ = response.Body.Close()
			<-done
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(closeStream)
	return events, closeStream
}

func awaitPublicReadCommit(t *testing.T, events <-chan GlobalEvent, path string, verify func()) {
	t.Helper()
	deadline := time.NewTimer(7 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event, ok := <-events:
			require.True(t, ok)
			if event.Kind != GlobalEventNodeChanged {
				continue
			}
			data, err := json.Marshal(event.Data)
			require.NoError(t, err)
			var changed NodeChangedEventData
			require.NoError(t, json.Unmarshal(data, &changed))
			for _, candidate := range changed.Paths {
				if candidate == path {
					verify()
					return
				}
			}
		case <-deadline.C:
			t.Fatalf("no committed node.changed for %s while provider blocked", path)
		}
	}
}
