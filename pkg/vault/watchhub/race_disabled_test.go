//go:build !race

package watchhub

func raceEnabled() bool {
	return false
}
