package watchhub

import (
	"path/filepath"
	"testing"
)

func TestIsNoisyInternalPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "/repo/.gocache/ab/cd", want: true},
		{path: "/repo/.gotmp/go-build123", want: true},
		{path: "/repo/worktrees/feature/.gocache/xx", want: true},
		{path: "/repo/.rhizome/db.sqlite-wal", want: false},
		{path: "/repo/pkg/service/main.go", want: false},
		{path: "", want: false},
	}
	for _, tc := range cases {
		got := isNoisyInternalPath(tc.path)
		if got != tc.want {
			t.Fatalf("isNoisyInternalPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestShouldDropBackendPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "/repo/.rhizome/db.sqlite", want: false},
		{path: "/repo/.rhizome/db.sqlite-wal", want: false},
		{path: "/repo/.rhizome/config.yml", want: false},
		{path: "/repo/.rhizome/ignore", want: false},
		{path: "/repo/.rhizome/ontology/schema.graphql", want: false},
		{path: "/tmp/.rhizome/worktrees/repo/pkg/main.go", want: false},
		{path: "/repo/.claude/worktrees/kind-shaw/web/main.go", want: false},
		{path: "/repo/web/node_modules/playwright-core/index.js", want: false},
		{path: "/repo/pkg/node_modules_helper/main.go", want: false},
		{path: "/repo/notes/project.md", want: false},
	}
	for _, tc := range cases {
		got := shouldDropBackendPath(tc.path)
		if got != tc.want {
			t.Fatalf("shouldDropBackendPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestShouldDropVaultRelPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: ".claude/worktrees/kind-shaw/web/main.go", want: true},
		{path: "web/node_modules/playwright-core/index.js", want: true},
		{path: ".git/FETCH_HEAD", want: true},
		{path: ".git/refs/remotes/origin/HEAD.lock", want: true},
		{path: "notes/.gitignore", want: false},
		{path: ".rhizome/db.sqlite", want: true},
		{path: ".rhizome/modules", want: false},
		{path: ".rhizome/modules/compounding", want: false},
		{path: ".rhizome/modules/compounding/ontology", want: false},
		{path: ".rhizome/modules/compounding/ontology/compounding.graphql", want: false},
		{path: ".rhizome/modules/compounding/views", want: true},
		{path: ".rhizome/modules/compounding/scripts/mine.mjs", want: true},
		{path: ".rhizome/modules/Bad_Name", want: true},
		{path: ".rhizome/db.sqlite-wal", want: true},
		{path: ".rhizome/config.yml", want: false},
		{path: ".rhizome/ignore", want: false},
		{path: ".rhizome/ontology/schema.graphql", want: false},
		{path: ".rhizome/modules/team/ontology/team.graphql", want: false},
		{path: ".rhizome/modules/team/views/board.tsx", want: true},
		{path: "pkg/node_modules_helper/main.go", want: false},
		{path: "../.claude/worktrees/repo/pkg/main.go", want: true},
		{path: "notes/project.md", want: false},
	}
	for _, tc := range cases {
		got := shouldDropVaultRelPath(tc.path)
		if got != tc.want {
			t.Fatalf("shouldDropVaultRelPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// The index database lives in the vault's .rhizome folder. Its writes must not
// reach the backend buffer, or a large index run overflows it and forces a
// full resync; config, ignore rules, ontology schemas, and a vault that itself
// sits under some other .rhizome folder must still get through.
func TestHubDropsGeneratedRhizomeStateAtIngress(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, ".rhizome", "worktrees", "repo")
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		".rhizome/db.sqlite":                      true,
		".rhizome/db.sqlite-wal":                  true,
		".rhizome/user-state.sqlite-shm":          true,
		".rhizome/diagnostics/runtime-output.log": true,
		".rhizome/config.yml":                     false,
		".rhizome/ignore":                         false,
		".rhizome/ontology/schema.graphql":        false,
		"pkg/main.go":                             false,
		"notes/project.md":                        false,
	}
	for rel, want := range cases {
		if got := hub.dropsAtIngress(filepath.Join(root, filepath.FromSlash(rel))); got != want {
			t.Errorf("dropsAtIngress(%q) = %v, want %v", rel, got, want)
		}
	}
	if hub.dropsAtIngress(filepath.Join(parent, "elsewhere", "note.md")) {
		t.Error("a path outside the vault must reach the hub")
	}
}
