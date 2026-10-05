//go:build !darwin

package watchhub

func backendIsFSEvents(backend Backend) bool {
	return false
}
