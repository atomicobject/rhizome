package tasks

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseDefinition(t *testing.T) {
	good := "name: Update memory\nevery: 7d\nprompt: Do it\n"
	def, err := ParseDefinition(".rhizome/tasks/update-memory.yml", []byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != "update-memory" || def.Scope != ScopePerson || def.Cadence != 7*24*time.Hour || def.Limit != time.Hour {
		t.Fatalf("unexpected definition %+v", def)
	}
	cases := map[string]string{
		"missing name":       "every: 1d\nprompt: x\n",
		"bad every":          "name: x\nevery: soon\nprompt: x\n",
		"bad scope":          "name: x\nevery: 1d\nscope: org\nprompt: x\n",
		"nothing to do":      "name: x\nevery: 1d\n",
		"two check kinds":    "name: x\nevery: 1d\ncheck: {command: [true], script: a.js}\n",
		"unknown field":      "name: x\nevery: 1d\nprompt: x\ncadence: 1d\n",
		"nonpositive window": "name: x\nevery: 0d\nprompt: x\n",
	}
	for name, body := range cases {
		if _, err := ParseDefinition(".rhizome/tasks/x.yml", []byte(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := ParseDefinition(".rhizome/tasks/Bad_Name.yml", []byte(good)); err == nil {
		t.Error("expected an invalid id error")
	}
}

func TestDefinitionsComeFromThePrimaryRefOnly(t *testing.T) {
	dir := newRepo(t, map[string]string{
		"good.yml":   "name: Good\nevery: 1d\nprompt: original\n",
		"broken.yml": "name: Broken\nevery: whenever\nprompt: x\n",
	})
	// A branch edit and an uncommitted edit never change what runs.
	run(t, dir, "switch", "--quiet", "-c", "feature")
	commitFile(t, dir, ".rhizome/tasks/good.yml", "name: Good\nevery: 1d\nprompt: branch edit\n", "edit task")
	writeFile(t, dir+"/.rhizome/tasks/good.yml", "name: Good\nevery: 1d\nprompt: dirty edit\n")

	repo, err := OpenRepo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	_, commit, err := repo.ResolvePrimary(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	defs, problems, err := LoadDefinitions(context.Background(), repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Prompt != "original" {
		t.Fatalf("expected the primary ref's good task only, got %+v", defs)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Path, "broken.yml") {
		t.Fatalf("expected one problem for broken.yml, got %+v", problems)
	}
}

func TestResolvePrimaryDefaultsToOriginHead(t *testing.T) {
	dir := newRepo(t, nil)
	origin := t.TempDir()
	run(t, origin, "init", "--quiet", "--bare", "-b", "main")
	run(t, dir, "remote", "add", "origin", origin)
	run(t, dir, "push", "--quiet", "origin", "main")
	repo, _ := OpenRepo(context.Background(), dir)
	if _, _, err := repo.ResolvePrimary(context.Background(), ""); err == nil {
		t.Fatal("expected an error before origin/HEAD is set")
	}
	run(t, dir, "remote", "set-head", "origin", "--auto")
	ref, commit, err := repo.ResolvePrimary(context.Background(), "")
	if err != nil || ref != "origin/main" || commit == "" {
		t.Fatalf("got %q %q %v", ref, commit, err)
	}
}

func TestTeamClockCountsTaggedCommitsInSquashBodies(t *testing.T) {
	dir := newRepo(t, map[string]string{"tidy.yml": "name: Tidy\nevery: 7d\nscope: team\nprompt: x\n"})
	// A squash merge puts each commit's trailer in the middle of the body.
	commitFile(t, dir, "notes.md", "x\n", "Tidy notes (#12)\n\n* chore(tasks): tidy\n\nRhizome-Task: tidy\n\n* fix typo\n\nCo-Authored-By: Someone <s@example.com>")
	repo, _ := OpenRepo(context.Background(), dir)
	tagged, ok, err := repo.LastTagged(context.Background(), "main", "tidy")
	if err != nil || !ok {
		t.Fatalf("expected a tagged commit, got %v %v", ok, err)
	}
	def, _ := ParseDefinition(".rhizome/tasks/tidy.yml", []byte("name: Tidy\nevery: 7d\nscope: team\nprompt: x\n"))
	clock := Clock{LastTagged: &tagged}
	if clock.Due(def, tagged.At.Add(6*24*time.Hour)) {
		t.Fatal("a teammate's merged run should reset the team clock")
	}
	if !clock.Due(def, tagged.At.Add(8*24*time.Hour)) {
		t.Fatal("the task should come due after its cadence")
	}
	def.Scope = ScopePerson
	if !clock.Due(def, tagged.At.Add(time.Hour)) {
		t.Fatal("a teammate's run must not count for a person task")
	}
	if _, ok, _ := repo.LastTagged(context.Background(), "main", "tid"); ok {
		t.Fatal("the tag must match the whole line")
	}
}
