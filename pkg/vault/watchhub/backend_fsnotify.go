//go:build !darwin || fsnotify_fallback

package watchhub

func newBackend(eventBuffer int, drop func(string) bool) (Backend, error) {
	return newFSNotifyBackend(eventBuffer, drop)
}
