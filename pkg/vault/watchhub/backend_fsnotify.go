//go:build !darwin || fsnotify_fallback

package watchhub

func newBackend(eventBuffer int) (Backend, error) {
	return newFSNotifyBackend(eventBuffer)
}
