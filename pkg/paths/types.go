package paths

// RelPath is a vault-root-relative path with forward slashes, no ./ prefix, cleaned.
// This is the canonical storage format for database keys.
type RelPath string

// String returns the path as a string.
func (p RelPath) String() string {
	return string(p)
}

// AbsPath is an absolute, symlink-resolved path (OS-native separators).
// Used only for filesystem I/O, never stored.
type AbsPath string

// String returns the path as a string.
func (p AbsPath) String() string {
	return string(p)
}

// NotePath is a RelPath for an authored note file.
//
// A NotePath never infers or constrains the file extension. Use the explicitly
// Markdown compatibility helpers when an operation intentionally needs the
// historical .md default.
type NotePath RelPath

// String returns the path as a string.
func (p NotePath) String() string {
	return string(p)
}

// CodePath is a RelPath for code files (no suffix guarantee).
type CodePath RelPath

// String returns the path as a string.
func (p CodePath) String() string {
	return string(p)
}
