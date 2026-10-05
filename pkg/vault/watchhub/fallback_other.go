//go:build !darwin

package watchhub

func (h *Hub) maybeFallbackBackend(reason error) bool {
	return false
}
