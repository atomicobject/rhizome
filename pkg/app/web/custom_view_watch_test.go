package web

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const watchTestTeamYAML = "apiVersion: rhizome.view.v1\nid: team\nname: Team\nsource: {kind: custom, entry: team.tsx}\nmount: {kind: standalone}\n"

func editViewFile(t *testing.T, srv *Server, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(srv.customViewsRoot(), filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// awaitViewsChanged reads views.changed events until one names want, failing
// if one names never first. Repeats are allowed: a poll can see a file
// mid-write and announce its folder again once the write finishes.
func awaitViewsChanged(t *testing.T, events <-chan GlobalEvent, want, never string) {
	t.Helper()
	for {
		select {
		case event := <-events:
			if event.Kind != GlobalEventViewsChanged {
				continue
			}
			data, _ := event.Data.(map[string]string)
			switch data["folder"] {
			case want:
				return
			case never:
				t.Fatalf("views.changed announced %s before %s", never, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("no views.changed for %s", want)
		}
	}
}

// One poll publishes all of its announcements together, so an unchanged
// folder announced beside a changed one arrives before a later edit's event.
func TestViewFolderChangesAreAnnouncedPerFolder(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx":   "export default null",
		"team/team.yaml":  watchTestTeamYAML,
		"team/team.tsx":   "export default null",
		"solo/solo.yaml":  strings.ReplaceAll(watchTestTeamYAML, "team", "solo"),
		"solo/solo.tsx":   "export default null",
		"tables/x.yaml":   "apiVersion: rhizome.view.v1\nid: x\nname: X\nsource: {kind: ontology_type, type: Doc}\nmount: {kind: type, type: Doc}\n",
		"poc/lib/part.ts": "export const part = 1",
	})
	srv.globalEvents = newGlobalEventBroker()
	events, unsubscribe := srv.globalEvents.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.pollViewFolders(ctx, 10*time.Millisecond)

	editViewFile(t, srv, "poc/lib/part.ts", "export const part = 22")
	awaitViewsChanged(t, events, "poc", "solo")
	editViewFile(t, srv, "team/team.tsx", "export default function Team() { return null }")
	awaitViewsChanged(t, events, "team", "solo")
}

// openEventStream connects to /api/v1/events and returns once the stream is
// open, when the folder watch has taken its baseline. closeStream returns once
// the handler has finished.
func openEventStream(t *testing.T, srv *Server) (body *bufio.Reader, closeStream func()) {
	t.Helper()
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.handleGlobalEvents))
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body = bufio.NewReader(resp.Body)
	if line, err := body.ReadString('\n'); err != nil || !strings.HasPrefix(line, ": connected") {
		t.Fatalf("stream did not open: %q %v", line, err)
	}
	return body, func() {
		cancel()
		_ = resp.Body.Close()
		httpSrv.Close()
	}
}

// The folder watch runs while any event stream is open. After the last one
// closes, an edit is never announced: a later stream starts from a baseline
// that already includes it, so its first announcements are only later edits.
func TestViewFolderWatchRunsOnlyWhileAStreamIsOpen(t *testing.T) {
	t.Parallel()
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx":  "export default null",
		"team/team.yaml": watchTestTeamYAML,
		"team/team.tsx":  "export default null",
		"solo/solo.yaml": strings.ReplaceAll(watchTestTeamYAML, "team", "solo"),
		"solo/solo.tsx":  "export default null",
	})
	srv.globalEvents = newGlobalEventBroker()
	events, unsubscribe := srv.globalEvents.Subscribe()
	defer unsubscribe()

	_, closeFirst := openEventStream(t, srv)
	_, closeSecond := openEventStream(t, srv)
	closeFirst()
	editViewFile(t, srv, "poc/board.tsx", "export default function Board() { return null }")
	awaitViewsChanged(t, events, "poc", "")

	closeSecond()
	editViewFile(t, srv, "team/team.tsx", "export default function Team() { return null }")
	_, closeThird := openEventStream(t, srv)
	defer closeThird()
	editViewFile(t, srv, "solo/solo.tsx", "export default function Solo() { return null }")
	awaitViewsChanged(t, events, "solo", "team")
	// A watch left running would have announced team by now; one more edit
	// orders anything it published before this check.
	editViewFile(t, srv, "poc/board.tsx", "export default function Board() { return <p /> }")
	awaitViewsChanged(t, events, "poc", "team")
}

// Server.Close closes the event broker, which ends every stream and with it
// the stream's hold on the folder watch.
func TestClosingTheEventBrokerEndsStreams(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	srv.globalEvents = newGlobalEventBroker()
	body, closeStream := openEventStream(t, srv)
	defer closeStream()
	srv.globalEvents.Close()
	if _, err := io.ReadAll(body); err != nil {
		t.Fatalf("stream did not end cleanly: %v", err)
	}
}
