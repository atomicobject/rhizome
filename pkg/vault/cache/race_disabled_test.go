//go:build !race

package cache

func raceEnabled() bool {
	return false
}
