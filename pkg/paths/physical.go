package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveAgainst resolves p against dir the way a process working in dir
// would open it, without cleaning: ".." must apply after any symlink before
// it is followed (see RelPhysicalStrict). An absolute p stands alone; on
// Windows a rooted p without a drive (\x or /x) takes dir's drive, and a
// drive-relative p (C:x) is returned as is, since it cannot be resolved.
func ResolveAgainst(dir, p string) string {
	if filepath.IsAbs(p) || dir == "" || filepath.VolumeName(p) != "" {
		return p
	}
	if p != "" && os.IsPathSeparator(p[0]) {
		return filepath.VolumeName(dir) + filepath.FromSlash(p)
	}
	return strings.TrimRight(dir, `/\`) + string(filepath.Separator) + filepath.FromSlash(p)
}

// RelPhysicalStrict is RelStrict for an absolute path a process opened:
// symlinks resolve one component at a time, as the kernel does, so
// link/../x names the parent of the link's target and a link out of the
// vault is rejected. A relative path is rejected.
func (v VaultPaths) RelPhysicalStrict(p string) (RelPath, error) {
	if !filepath.IsAbs(p) {
		return RelPath(""), ErrOutsideVault
	}
	return v.RelStrict(physicalPath(p))
}

// physicalPath resolves an absolute path one component at a time. Components
// after the first missing one cannot be links and apply lexically.
func physicalPath(p string) string {
	p = filepath.FromSlash(p)
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	missing := false
	for _, part := range strings.Split(p[len(vol):], string(filepath.Separator)) {
		switch part {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, part)
		if !missing {
			if resolved, err := filepath.EvalSymlinks(next); err == nil {
				next = resolved
			} else {
				missing = true
			}
		}
		cur = next
	}
	return cur
}
