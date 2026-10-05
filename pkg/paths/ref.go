package paths

// PathRef bundles the canonical relative and absolute forms of a path.
// Rel is vault-root-relative and normalized (used for storage/IDs/FQNs).
// Abs is absolute and symlink-resolved (used only for filesystem I/O).
type PathRef[T pathRel] struct {
	Rel T
	Abs AbsPath
}

type pathRel interface {
	RelPath | NotePath | CodePath
}

// CodePathRef is a PathRef for code files (no suffix guarantee).
type CodePathRef = PathRef[CodePath]

// NotePathRef is a PathRef for authored notes (extension-neutral).
type NotePathRef = PathRef[NotePath]
