package taskdef

import "testing"

func TestLandingDefaultsAndWrites(t *testing.T) {
	parse := func(body string) (Definition, error) {
		return ParseDefinition(".rhizome/tasks/x.yml", []byte("name: x\nevery: 1d\nprompt: x\n"+body))
	}
	for body, want := range map[string]string{
		"":                                       LandPR,
		"land: pr\n":                             LandPR,
		"writes: [notes/**]\n":                   LandInPlace,
		"land: in-place\nwrites: [notes/*.md]\n": LandInPlace,
	} {
		def, err := parse(body)
		if err != nil || def.Land != want {
			t.Errorf("%q: land %q, %v; want %q", body, def.Land, err, want)
		}
	}
	for _, body := range []string{
		"land: in-place\n",
		"land: pr\nwrites: [notes/**]\n",
		"land: direct\n",
		"writes: [/abs/**]\n",
		"writes: [../up/**]\n",
		"writes: [a//b]\n",
		"writes: [\"\"]\n",
		"writes: [\"notes/[\"]\n",
		`writes: ["notes\\a"]` + "\n",
	} {
		if _, err := parse(body); err == nil {
			t.Errorf("%q: expected an error", body)
		}
	}
	def := Definition{Writes: []string{"notes/**", "docs/*.md"}}
	for path, want := range map[string]bool{"notes/a.md": true, "notes/a/b.md": true, "docs/a.md": true, "docs/a/b.md": false, "README.md": false} {
		if def.MayWrite(path) != want {
			t.Errorf("MayWrite(%q) = %v", path, !want)
		}
	}
}
