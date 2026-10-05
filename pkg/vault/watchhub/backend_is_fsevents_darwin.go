//go:build darwin

package watchhub

func backendIsFSEvents(backend Backend) bool {
	_, ok := backend.(*fseventsBackend)
	return ok
}
