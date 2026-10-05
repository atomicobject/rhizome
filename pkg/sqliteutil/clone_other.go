//go:build !darwin && !linux

package sqliteutil

func cloneFile(string, string) error { return errCloneUnsupported }
