package sqliteutil

import "testing"

func TestPathWithinMountUsesPathBoundaries(t *testing.T) {
	if !pathWithinMount("/workspace/repo/.rhizome/db.sqlite", "/workspace") {
		t.Fatal("expected database below mount to match")
	}
	if pathWithinMount("/workspace-other/db.sqlite", "/workspace") {
		t.Fatal("mount prefix without path boundary must not match")
	}
}

func TestUnescapeMountInfo(t *testing.T) {
	if got := unescapeMountInfo(`/workspace/My\040Repo`); got != "/workspace/My Repo" {
		t.Fatalf("unexpected mount path %q", got)
	}
}
