//go:build !darwin && !linux && !windows

package proctree

// sessionOf is unknown on other systems; only the group and recorded
// descendants are killed there.
func sessionOf(int) int { return -1 }
