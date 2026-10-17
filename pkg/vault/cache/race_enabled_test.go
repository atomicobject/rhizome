//go:build integration && race

package cache

func raceEnabled() bool {
	return true
}
