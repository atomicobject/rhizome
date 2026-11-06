package paths

import "strings"

// CanonicalExistingPath resolves an existing path for filesystem identity.
// Windows DOS namespace aliases use ordinary spelling only when both name the
// same file and Win32 parsing cannot change a component's name.
func CanonicalExistingPath(path string) (AbsPath, error) {
	canonical, err := canonicalExistingPath(path)
	if err != nil {
		return "", err
	}
	return AbsPath(canonical), nil
}

func verbatimDOSPath(path string) string {
	if len(path) >= 3 && path[1] == ':' && path[2] == '\\' &&
		(path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z') {
		return `\\?\` + path
	}
	if strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, `\\?\`) && !strings.HasPrefix(path, `\\.\`) {
		return `\\?\UNC\` + path[2:]
	}
	return path
}

func ordinaryDOSPath(path string) string {
	if !strings.HasPrefix(path, `\\?\`) {
		return path
	}
	tail := path[4:]
	if len(tail) >= 4 && strings.EqualFold(tail[:4], `UNC\`) {
		return `\\` + tail[4:]
	}
	if len(tail) >= 3 && tail[1] == ':' && tail[2] == '\\' &&
		(tail[0] >= 'A' && tail[0] <= 'Z' || tail[0] >= 'a' && tail[0] <= 'z') {
		return tail
	}
	return path
}
