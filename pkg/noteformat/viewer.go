package noteformat

import "fmt"

// ViewerBootstrapPlanner is the optional syntax-owned boundary for choosing a
// proven ephemeral bootstrap insertion point in an active note document.
type ViewerBootstrapPlanner interface {
	Provider
	ViewerBootstrapOffset(AuthoredSource) (int, error)
}

// ViewerBootstrapOffset dispatches bootstrap placement to the selected
// provider without exposing concrete syntax to application consumers.
func (r Runtime) ViewerBootstrapOffset(source AuthoredSource) (int, error) {
	descriptor, err := r.descriptorForSource(source)
	if err != nil {
		return 0, err
	}
	if !descriptor.Capabilities.Has(CapabilityActiveContentViewing) {
		return 0, fmt.Errorf("format %q does not support active content viewing", descriptor.ID)
	}
	projector, ok := r.projectors[fold(descriptor.ID)]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrProjectorUnavailable, descriptor.ID)
	}
	planner, ok := projector.(ViewerBootstrapPlanner)
	if !ok {
		return 0, fmt.Errorf("format %q declares active viewing without a bootstrap planner", descriptor.ID)
	}
	return planner.ViewerBootstrapOffset(source)
}
