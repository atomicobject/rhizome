package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const moduleTeamYAML = "apiVersion: rhizome.view.v1\nid: work.team\nname: Team\nsource: {kind: custom, entry: team.tsx}\nmount: {kind: standalone}\n"

// newModuleViewTestServer adds a module "work" whose views/team folder holds a
// custom view, beside the flat test view and a flat folder named "modules".
func newModuleViewTestServer(t *testing.T) *Server {
	t.Helper()
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx":      "export default null",
		"modules/mods.yaml":  strings.NewReplacer("work.team", "flat.mods", "team.tsx", "mods.tsx").Replace(moduleTeamYAML),
		"modules/mods.tsx":   "export default null",
		"team/flatteam.yaml": strings.NewReplacer("work.team", "flat.team").Replace(moduleTeamYAML),
		"team/team.tsx":      "export default null",
	})
	for rel, body := range map[string]string{
		"team/team.yaml":  moduleTeamYAML,
		"team/team.tsx":   `export default function Team() { return <p>team</p>; }`,
		"team/broken.tsx": "export default () => <div>;",
	} {
		writeModuleViewFile(t, srv, rel, body)
	}
	return srv
}

func writeModuleViewFile(t *testing.T, srv *Server, rel, body string) {
	t.Helper()
	full := filepath.Join(viewRoot{module: "work"}.dir(srv.cfg.VaultPath), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModuleCustomViewIsServedAndStampedFromItsModule(t *testing.T) {
	srv := newModuleViewTestServer(t)

	body := getCustomView(srv, "/views/work.team").Body.String()
	for _, want := range []string{
		`import("/views/_module/work/_files/team/team.tsx")`,
		`"folder":"module:work/team"`,
		`"stamp":"/views/_module/work/_stamp/team"`,
		`"check":"/views/_module/work/_check/team"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing %q:\n%s", want, body)
		}
	}
	// A flat folder named "modules" keeps its flat URLs and bare folder key.
	flat := getCustomView(srv, "/views/flat.mods").Body.String()
	if !strings.Contains(flat, `import("/views/_files/modules/mods.tsx")`) || !strings.Contains(flat, `"folder":"modules"`) {
		t.Fatalf("flat modules folder shell:\n%s", flat)
	}

	module := getCustomView(srv, "/views/_module/work/_files/team/team.tsx")
	if module.Code != http.StatusOK || !strings.Contains(module.Body.String(), "react/jsx-runtime") {
		t.Fatalf("module file: %d %s", module.Code, module.Body.String())
	}
	if check := getCustomView(srv, "/views/_module/work/_check/team"); !strings.HasPrefix(check.Body.String(), "team/broken.tsx:1:") {
		t.Fatalf("check: %d %q", check.Code, check.Body.String())
	}

	before := getCustomView(srv, "/views/_module/work/_stamp/team").Body.String()
	writeModuleViewFile(t, srv, "team/team.tsx", "export default function Team() { return null }")
	if after := getCustomView(srv, "/views/_module/work/_stamp/team").Body.String(); after == before {
		t.Fatalf("module stamp did not change: %q", after)
	}
}

func TestModuleViewRoutesStayInsideTheModuleViewsFolder(t *testing.T) {
	srv := newModuleViewTestServer(t)
	link := filepath.Join(viewRoot{module: "work"}.dir(srv.cfg.VaultPath), "team", "leak.tsx")
	if err := os.Symlink(filepath.Join(srv.cfg.VaultPath, ".rhizome", "views", "poc", "board.tsx"), link); err != nil {
		t.Skip("symlinks unavailable")
	}
	for _, target := range []string{
		"/views/_module/work/_files/team/leak.tsx",
		"/views/_module/work/_files/../../../../secret.txt",
		"/views/_module/work/_files/../../../views/poc/board.tsx",
		"/views/_module/work/_files/team/team.yaml",
		"/views/_module/../_files/poc/board.tsx",
		"/views/_module/missing/_files/team/team.tsx",
		"/views/_module/work/team/team.tsx",
	} {
		if rec := getCustomView(srv, target); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d body %q", target, rec.Code, rec.Body.String())
		}
	}
	for _, target := range []string{"/views/_module/work/_stamp/../..", "/views/_module/work/_check/../../../views"} {
		if rec := getCustomView(srv, target); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", target, rec.Code)
		}
	}
}

func TestViewFolderPollerAnnouncesModuleViewChanges(t *testing.T) {
	srv := newModuleViewTestServer(t)
	srv.globalEvents = newGlobalEventBroker()
	events, unsubscribe := srv.globalEvents.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.pollViewFolders(ctx, 10*time.Millisecond)

	writeModuleViewFile(t, srv, "team/team.tsx", "export default function Team() { return <b /> }")
	// The flat "team" folder is a different folder and stays quiet.
	awaitViewsChanged(t, events, "module:work/team", "team")
}
