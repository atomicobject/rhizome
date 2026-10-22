package hook

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellReadsRecognizesBoundedReadForms(t *testing.T) {
	cwd := filepath.FromSlash("/repo")
	for _, tc := range []struct {
		cmd  string
		want []string
	}{
		{"cat Makefile", []string{"/repo/Makefile"}},
		{"cat 'docs/a file.md' \"pkg/b c.go\"", []string{"/repo/docs/a file.md", "/repo/pkg/b c.go"}},
		{`cat docs/a\ file.md`, []string{"/repo/docs/a file.md"}},
		{"cd pkg && sed -n 1,20p store/db.go", []string{"/repo/pkg/store/db.go"}},
		{"cd sub; head -n 5 x.go", []string{"/repo/sub/x.go"}},
		{"head -20 a.go && tail -n +3 b.go", []string{"/repo/a.go", "/repo/b.go"}},
		{"nl -ba main.go | head -40", []string{"/repo/main.go"}},
		{"rg -n 'func Run' pkg/app cmd/root.go", []string{"/repo/pkg/app", "/repo/cmd/root.go"}},
		{"rg -g '*.go' -e Run pkg", []string{"/repo/pkg"}},
		{"grep -rn --include=*.go Deadline pkg/app/cli", []string{"/repo/pkg/app/cli"}},
		{"rg --files docs", []string{"/repo/docs"}},
		{"cat a.go > out.txt", []string{"/repo/a.go"}},
		{"FOO=1 cat a.go 2>/dev/null", []string{"/repo/a.go"}},
		{"cat 'C:\\repo\\pkg\\a.go'", []string{`C:\repo\pkg\a.go`}},
		{"sed -n -e1,20p pkg/a.go", []string{"/repo/pkg/a.go"}},
		{"sed -n -e 1,20p pkg/a.go", []string{"/repo/pkg/a.go"}},
		{"sed -ne1,20p pkg/a.go", []string{"/repo/pkg/a.go"}},
		{"grep -eTODO pkg/a.go", []string{"/repo/pkg/a.go"}},
		{"grep -e TODO pkg/a.go", []string{"/repo/pkg/a.go"}},
		{"grep -ieTODO pkg/a.go", []string{"/repo/pkg/a.go"}},
		{"grep -inA3 TODO pkg/a.go", []string{"/repo/pkg/a.go"}},
		{"rg -g*.go TODO pkg", []string{"/repo/pkg"}},
		{"cat a.go # see docs/notes.md", []string{"/repo/a.go"}},
		{"cat a.go #docs/notes.md\ncat b.go", []string{"/repo/a.go", "/repo/b.go"}},
		{"cat a#b.go 'c # d.md'", []string{"/repo/a#b.go", "/repo/c # d.md"}},
	} {
		got := shellReads(tc.cmd, cwd)
		want := make([]string, len(tc.want))
		for i, p := range tc.want {
			want[i] = filepath.FromSlash(p)
			if p[0] == 'C' && runtime.GOOS != "windows" {
				// A drive path is relative off Windows.
				want[i] = "/repo/" + p
			}
		}
		require.Equal(t, want, got, tc.cmd)
	}
}

func TestShellReadsIgnoresEverythingElse(t *testing.T) {
	for _, cmd := range []string{
		"rg Deadline",            // the pattern is not a path
		"grep -n TODO",           // no file operand
		"sed 's/a/b/' x.go",      // sed without -n prints everything; not a read form
		"sed -i -n 1p x.go",      // in-place edit
		"sed -ni 1p x.go",        // in-place edit in a cluster
		"# cat a.go",             // a comment
		"cat $FILE",              // expansion
		"cat *.go",               // glob
		"cat $(git ls-files)",    // command substitution
		"go test ./...",          // not a read command
		"echo cat a.go",          // cat is an argument
		"cat <<EOF\nhello\nEOF",  // here-document
		"(cd pkg && cat a.go)",   // subshell
		"cat 'unterminated a.go", // broken quoting
	} {
		require.Empty(t, shellReads(cmd, "/repo"), cmd)
	}
}

func TestMatchesPrefixChecksEachSegment(t *testing.T) {
	prefixes := []string{"go  mod vendor", "", "make web-e2e"}
	require.True(t, matchesPrefix("go mod vendor", prefixes))
	require.True(t, matchesPrefix("cd web &&  make   web-e2e PORT=1", prefixes))
	require.False(t, matchesPrefix("echo go mod vendor", prefixes))
	require.False(t, matchesPrefix("ls", prefixes), "an empty prefix matches nothing")
}

func TestPatchPathsIncludeMovesAndDeletes(t *testing.T) {
	patch := "*** Begin Patch\n*** Update File: pkg/a.go\n*** Move to: pkg/b.go\n@@\n-x\n+y\n*** Delete File: old.go\r\n*** Add File: new.go\n+z\n*** End Patch"
	require.Equal(t, []string{
		filepath.FromSlash("/repo/pkg/a.go"), filepath.FromSlash("/repo/pkg/b.go"),
		filepath.FromSlash("/repo/old.go"), filepath.FromSlash("/repo/new.go"),
	}, patchPaths(patch, filepath.FromSlash("/repo")))
}
