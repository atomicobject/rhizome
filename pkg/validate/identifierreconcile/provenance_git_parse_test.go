package identifierreconcile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Git 2.56 `log --follow -- a.md` output when an identical b.md was added on
// a merged side branch: the side branch's record names b.md, not a.md.
const followAcrossLinesOutput = "\x1e886f01e78a046e4e2c837eadc4fbee657a4d196d\x001784102400\x00950e607be5346d75ec00bbb589d51721150a0a07\x00\x00\nA\x00b.md\x00" +
	"\x1ebad7b41ac21e1fa5ab13ac04e6d26d77f6e48b99\x001784116800\x00950e607be5346d75ec00bbb589d51721150a0a07\x00\x00\nA\x00a.md\x00"

func TestParseGitHistoryIgnoresOtherLinesNames(t *testing.T) {
	events, err := parseGitHistory([]byte(followAcrossLinesOutput), "a.md", 2)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "bad7b41ac21e1fa5ab13ac04e6d26d77f6e48b99", events[0].oid)
	require.Equal(t, "a.md", events[0].path)
	require.Equal(t, byte('A'), events[0].status)
}

func TestParseGitHistoryStillRejectsAMalformedRecordForTheTrackedPath(t *testing.T) {
	malformed := "\x1ebad7b41ac21e1fa5ab13ac04e6d26d77f6e48b99\x001784116800\x00950e607be5346d75ec00bbb589d51721150a0a07\x00\x00\nA\x00extra\x00a.md\x00"
	_, err := parseGitHistory([]byte(malformed), "a.md", 1)
	require.ErrorContains(t, err, "path record malformed")
}

func TestParseGitHistoryValidatesForeignRecordsBeforeFiltering(t *testing.T) {
	const header = "\x1e886f01e78a046e4e2c837eadc4fbee657a4d196d\x001784102400\x00950e607be5346d75ec00bbb589d51721150a0a07\x00\x00\n"
	for _, tc := range []struct {
		name, record, wantError string
	}{
		{"addition status", "Aanything\x00b.md\x00", "path record malformed"},
		{"modification status", "M100\x00b.md\x00", "path record malformed"},
		{"addition shape", "A\x00extra\x00b.md\x00", "path record malformed"},
		{"modification shape", "M\x00extra\x00b.md\x00", "path record malformed"},
		{"rename", "R100\x00old.md\x00b.md\x00", "rename history ambiguous"},
		{"copy", "C100\x00old.md\x00b.md\x00", "copy history ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseGitHistory(append([]byte(header+tc.record), []byte(followAcrossLinesOutput)...), "a.md", 3)
			require.ErrorContains(t, err, tc.wantError)
		})
	}
}
