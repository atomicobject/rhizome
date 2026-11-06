//go:build !windows

package jsonstdio

func reapedKillErrorIsBenign(<-chan struct{}, error) bool { return false }
