package paths

// CleanRelPath validates and cleans a vault-root-relative path.
// It returns ErrOutsideVault if the path is absolute, contains a drive letter,
// or escapes the vault via ".." segments.
func CleanRelPath(p string) (RelPath, error) {
	clean, err := cleanRelPath(p)
	if err != nil {
		return RelPath(""), err
	}
	return RelPath(clean), nil
}
