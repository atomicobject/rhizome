package sourcetxn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These inputs deliberately bypass the journal encoder and fingerprint helper.
// The literal digests and wire field names are historical compatibility data.
func TestRecoveryOfLiteralLegacyJournals(t *testing.T) {
	for _, tc := range []struct{ name, current, expected string }{
		{"prepared", "new\n", "old\n"}, {"committed", "later authored bytes\n", "later authored bytes\n"}, {"receipt", "new\n", "new\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, options, _ := sourceFixture(t)
			journal := filepath.Join(root, ".rhizome", "edit-journal", tc.name+".json")
			receipt := filepath.Join(root, ".rhizome", "edit-receipts", "literal.json")
			paths := map[string]string{
				"@TEMP_A@":   filepath.Join(root, "a.md.rhizome-write-literal"),
				"@BACKUP_A@": filepath.Join(root, ".a.md.rhizome-backup-literal"),
				"@TEMP_B@":   filepath.Join(root, "b.md.rhizome-write-literal"),
				"@BACKUP_B@": filepath.Join(root, ".b.md.rhizome-backup-literal"),
				"@RECEIPT@":  receipt, "@RECEIPT_TEMP@": filepath.Join(filepath.Dir(receipt), ".completion-literal"),
			}
			require.NoError(t, os.MkdirAll(filepath.Dir(journal), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte(tc.current), 0640))
			require.NoError(t, os.WriteFile(paths["@TEMP_A@"], []byte("new\n"), 0640))
			require.NoError(t, os.WriteFile(paths["@BACKUP_A@"], []byte("old\n"), 0640))
			if tc.name == "prepared" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), []byte("old b\n"), 0640))
				require.NoError(t, os.WriteFile(paths["@TEMP_B@"], []byte("new b\n"), 0640))
				require.NoError(t, os.WriteFile(paths["@BACKUP_B@"], nil, 0640))
			}
			if tc.name == "receipt" {
				require.NoError(t, os.MkdirAll(filepath.Dir(receipt), 0700))
				require.NoError(t, os.WriteFile(receipt, []byte("receipt\n"), 0600))
				require.NoError(t, os.WriteFile(paths["@RECEIPT_TEMP@"], []byte("receipt\n"), 0600))
			}
			fixture, err := os.ReadFile(filepath.Join("testdata", "legacy", tc.name+".json"))
			require.NoError(t, err)
			for placeholder, path := range paths {
				encoded, err := json.Marshal(path)
				require.NoError(t, err)
				fixture = []byte(strings.ReplaceAll(string(fixture), `"`+placeholder+`"`, string(encoded)))
			}
			require.NoError(t, os.WriteFile(journal, fixture, 0600))
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, recoverSourceFixture(root, options))
			content, err := os.ReadFile(filepath.Join(root, "a.md"))
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(content))
			require.NoFileExists(t, journal)
			for placeholder, path := range paths {
				if placeholder != "@RECEIPT@" {
					require.NoFileExists(t, path)
				}
			}
			if tc.name == "prepared" {
				content, err := os.ReadFile(filepath.Join(root, "b.md"))
				require.NoError(t, err)
				require.Equal(t, "old b\n", string(content))
			}
			if tc.name == "receipt" {
				content, err := os.ReadFile(receipt)
				require.NoError(t, err)
				require.Equal(t, "receipt\n", string(content))
			}
			require.NoError(t, CheckPending(root))
		})
	}
}
