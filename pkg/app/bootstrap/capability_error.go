package bootstrap

// ErrCapabilityNotRequested reports a runtime capability omitted by an
// explicit one-shot requirement set. A zero-value target matches any omitted
// capability through errors.Is; a populated target matches one capability.
type ErrCapabilityNotRequested struct {
	Capability RuntimeCapability
}

func (e ErrCapabilityNotRequested) Error() string {
	switch e.Capability {
	case RuntimeCapabilitySearch:
		return "search capability not requested"
	case RuntimeCapabilitySemantic:
		return "semantic capability not requested"
	case RuntimeCapabilityCodeIndex:
		return "code index capability not requested"
	default:
		return string(e.Capability) + " capability not requested"
	}
}

// Is supports both a generic zero-value sentinel and a capability-specific
// sentinel while retaining the concrete type for errors.As.
func (e ErrCapabilityNotRequested) Is(target error) bool {
	want, ok := target.(ErrCapabilityNotRequested)
	return ok && (want.Capability == "" || want.Capability == e.Capability)
}

func capabilityNotRequested(capability RuntimeCapability) error {
	return ErrCapabilityNotRequested{Capability: capability}
}
