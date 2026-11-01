package tasks

import (
	"context"
	"path/filepath"
	"testing"
)

func TestModuleTasksGetQualifiedIDsAndSeparateFolders(t *testing.T) {
	dir := newRepo(t, map[string]string{"a-b.yml": "name: Flat\nevery: 1d\nprompt: flat\n"})
	commitFile(t, dir, ".rhizome/modules/a/tasks/b.yml", "name: Module\nevery: 1d\nprompt: module\n", "module task")
	// Neither a nested file nor a folder with an invalid module name holds tasks.
	commitFile(t, dir, ".rhizome/modules/a/tasks/nested/c.yml", "name: Nested\nevery: 1d\nprompt: x\n", "nested")
	commitFile(t, dir, ".rhizome/modules/Bad/tasks/d.yml", "name: Bad\nevery: 1d\nprompt: x\n", "bad module")
	ctx := context.Background()
	svc := newService(t, dir, &fakeAgent{}, "")
	_, commit, err := svc.opts.Repo.ResolvePrimary(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	defs, problems, err := LoadDefinitions(ctx, svc.opts.Repo, commit)
	if err != nil || len(problems) != 0 {
		t.Fatalf("load: %v %+v", err, problems)
	}
	if len(defs) != 2 || defs[0].ID != "a-b" || defs[1].ID != "a/b" || defs[1].Tag() != "Rhizome-Task: a/b" {
		t.Fatalf("expected flat a-b then module a/b, got %+v", defs)
	}
	flat, module := defs[0], defs[1]

	if f, m := svc.locations(flat, commit).DataDir, svc.locations(module, commit).DataDir; f == m || filepath.Base(m) != "a_b" {
		t.Fatalf("data folders collide or are misnamed: %s, %s", f, m)
	}
	date := svc.opts.Now().Format("2006-01-02")
	var dirs []string
	for _, def := range defs {
		branch, wt, err := svc.createWorktree(ctx, def, commit)
		if err != nil {
			t.Fatal(err)
		}
		if branch != "rzm/"+def.ID+"/"+date {
			t.Fatalf("unexpected branch %q", branch)
		}
		dirs = append(dirs, filepath.Base(wt))
	}
	if dirs[0] != "a-b-"+date || dirs[1] != "a_b-"+date {
		t.Fatalf("unexpected worktree names %q", dirs)
	}

	// The tag grep treats the slash literally and only matches whole lines.
	commitFile(t, dir, "notes.md", "x\n", "work\n\nRhizome-Task: a/b")
	if _, found, err := svc.opts.Repo.LastTagged(ctx, "main", "a/b"); err != nil || !found {
		t.Fatalf("expected a/b's tagged commit: %v", err)
	}
	if _, found, _ := svc.opts.Repo.LastTagged(ctx, "main", "a"); found {
		t.Fatal("flat task a must not match a/b's tag")
	}

	// A draft run's file in a module gets the module task's id.
	def, err := ParseDefinition(draftPath(dir, filepath.Join(dir, ".rhizome", "modules", "a", "tasks", "b.yml")), []byte("name: X\nevery: 1d\nprompt: x\n"))
	if err != nil || def.ID != "a/b" {
		t.Fatalf("draft id: %v %q", err, def.ID)
	}
}

func TestTaskBranchesNeedTheDateSuffix(t *testing.T) {
	for branch, want := range map[string]bool{
		"rzm/compounding/2026-11-01":       true,
		"rzm/compounding/2026-11-01-3":     true,
		"rzm/compounding/sweep/2026-11-01": false,
		"rzm/compounding-x/2026-11-01":     false,
		"rzm/compounding/wip":              false,
	} {
		if got := isTaskBranch(branch, "compounding"); got != want {
			t.Errorf("isTaskBranch(%q, compounding) = %v", branch, got)
		}
	}
	if !isTaskBranch("rzm/compounding/sweep/2026-11-01", "compounding/sweep") {
		t.Error("a module task matches its own branches")
	}
}
