//go:build darwin

package sqliteutil

import "golang.org/x/sys/unix"

func cloneFile(source, target string) error {
	return unix.Clonefile(source, target, unix.CLONE_NOFOLLOW)
}
