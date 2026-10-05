package watchhub

import "testing"

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
		{path: ".rhizome/db.sqlite", want: true},
		{path: ".rhizome/db.sqlite-wal", want: true},
		{path: ".rhizome/config.yml", want: false},
		{path: ".rhizome/ignore", want: false},
		{path: ".rhizome/ontology/schema.graphql", want: false},
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
