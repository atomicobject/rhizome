package obsidian

import "path/filepath"

const (
	RhizomeDirName        = ".rhizome"
	RhizomeConfigFilename = "config.yml"
	RhizomeIgnoreFilename = "ignore"
	// RhizomeDBFile is the unified SQLite database used for both semantic embeddings and code intel/indexes.
	RhizomeDBFile            = "db.sqlite"
	RhizomeIndexLockFilename = "index.lock"
)

func rhizomeDir(base string) string {
	return filepath.Join(base, RhizomeDirName)
}

func rhizomeConfigPath(base string) string {
	return filepath.Join(rhizomeDir(base), RhizomeConfigFilename)
}

// IndexLockPath returns the path to the vault index lock file.
func IndexLockPath(base string) string {
	return filepath.Join(rhizomeDir(base), RhizomeIndexLockFilename)
}
