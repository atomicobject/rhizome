package ontology

// LifecycleStage is where an enum value sits in a lifecycle. Views read it for
// behavior ("in motion", "stale", "finished"); tone stays presentation and
// defaults from the stage. See SPEC-0112.
type LifecycleStage string

const (
	StageOpen    LifecycleStage = "open"
	StageActive  LifecycleStage = "active"
	StageDone    LifecycleStage = "done"
	StageDropped LifecycleStage = "dropped"
)

// stageDefaultTones is the tone a value gets from its declared stage when it
// authors none. It also lists the valid stages.
var stageDefaultTones = map[LifecycleStage]string{
	StageOpen:    "neutral",
	StageActive:  "progress",
	StageDone:    "success",
	StageDropped: "muted",
}

// EnumValueStage is one value's effective stage and whether the schema
// declared it or Rhizome inferred it from authored tone or collapsed metadata.
type EnumValueStage struct {
	Stage    LifecycleStage `json:"stage"`
	Declared bool           `json:"declared,omitempty"`
}

// HasDeclaredStages reports whether the enum declares @view(stage:). The
// compiler requires every value to declare one when any does.
func (t *EnumType) HasDeclaredStages() bool {
	if t == nil {
		return false
	}
	for _, value := range t.Values {
		if value != nil && value.View.Stage != "" {
			return true
		}
	}
	return false
}

// valuesMissingStage names the values without a stage when some value of the
// enum declares one.
func (t *EnumType) valuesMissingStage() []string {
	if !t.HasDeclaredStages() {
		return nil
	}
	var missing []string
	for _, value := range t.Values {
		if value != nil && value.View.Stage == "" {
			missing = append(missing, value.Name)
		}
	}
	return missing
}

// Stages returns each value's effective stage in declaration order, or nil
// when the enum declares no stages and has no authored tone or collapsed
// metadata to infer them from.
func (t *EnumType) Stages() []EnumValueStage {
	if t == nil {
		return nil
	}
	out := make([]EnumValueStage, len(t.Values))
	if t.HasDeclaredStages() {
		for index, value := range t.Values {
			if value != nil {
				out[index] = EnumValueStage{Stage: value.View.Stage, Declared: true}
			}
		}
		return out
	}
	authored := false
	for index, value := range t.Values {
		if value == nil {
			continue
		}
		if value.View.Tone != "" || value.View.Collapsed != nil {
			authored = true
		}
		out[index] = EnumValueStage{Stage: inferredStage(value.View)}
	}
	if !authored {
		return nil
	}
	return out
}

func inferredStage(view EnumValueView) LifecycleStage {
	switch view.Tone {
	case "progress":
		return StageActive
	case "success":
		return StageDone
	case "muted":
		return StageDropped
	case "":
		if view.Collapsed != nil && *view.Collapsed {
			return StageDropped
		}
	}
	return StageOpen
}

// CollapsedDefaults reports, in declaration order, whether each value starts
// collapsed: an authored collapsed flag, else whether the declared stage is
// dropped.
func (t *EnumType) CollapsedDefaults() []bool {
	if t == nil {
		return nil
	}
	out := make([]bool, len(t.Values))
	for index, value := range t.Values {
		switch {
		case value == nil:
		case value.View.Collapsed != nil:
			out[index] = *value.View.Collapsed
		default:
			out[index] = value.View.Stage == StageDropped
		}
	}
	return out
}
