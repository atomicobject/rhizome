//go:build windows

package cmd

// Windows has no process-image replacement; preserve the delegated process's
// console and standard streams while waiting for its exit.
func executeRepoBinary(target string, args []string) int {
	return runRepoBinary(target, args)
}
