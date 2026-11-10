package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// configGeneration is the `config` component of the index generation
// (SPEC-0136 R4): a hash of the configuration files taken each time the
// runtime publishes a configuration. The publication count is hashed too, so
// a file that changed between its load and this hash still yields a new value
// at the next publication.
type configGeneration struct {
	published uint64
	value     string
}

// configGenerationFiles are the files whose reloads pass through
// publishOwnershipConfiguration.
var configGenerationFiles = []string{".rhizome/config.yml", ".rhizome/ignore", ".obsidianignore"}

// advance must run under ownershipConfigMu, after the new configuration is
// visible, so a reader that sees the new value also reads with the new config.
func (g *configGeneration) advance(vaultPath string) {
	g.published++
	hash := sha256.New()
	fmt.Fprintf(hash, "%d\n", g.published)
	for _, rel := range configGenerationFiles {
		data, err := os.ReadFile(filepath.Join(vaultPath, filepath.FromSlash(rel)))
		if err != nil {
			data = nil
		}
		fmt.Fprintf(hash, "%s %d\n", rel, len(data))
		hash.Write(data)
	}
	g.value = hex.EncodeToString(hash.Sum(nil))[:12]
}
