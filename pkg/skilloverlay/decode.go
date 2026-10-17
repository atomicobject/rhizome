package skilloverlay

import (
	"bytes"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

func Decode(filePath string, content []byte) (Overlay, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var overlay Overlay
	if err := decoder.Decode(&overlay); err != nil {
		return Overlay{}, skillOverlayErr(skillOverlayIssuef("overlay_yaml_decode_error", "", filePath, "", "", "%v", err))
	}
	if strings.TrimSpace(overlay.APIVersion) != APIVersion {
		return Overlay{}, skillOverlayErr(skillOverlayIssuef("unsupported_api_version", overlay.TargetSkill, filePath, "", overlay.ID, "apiVersion must be %s", APIVersion))
	}
	if strings.TrimSpace(overlay.ID) == "" {
		return Overlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, "", overlay.ID, "id is required"))
	}
	if strings.TrimSpace(overlay.TargetSkill) == "" {
		return Overlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, "", overlay.ID, "targetSkill is required"))
	}
	if len(overlay.Fragments) == 0 {
		return Overlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, "", overlay.ID, "fragments are required"))
	}
	for i, fragment := range overlay.Fragments {
		if strings.TrimSpace(fragment.Slot) == "" {
			return Overlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, "", overlay.ID, "fragments[%d].slot is required", i))
		}
		if !validSkillOverlayOp(fragment.Op) {
			return Overlay{}, skillOverlayErr(skillOverlayIssuef("invalid_operation", overlay.TargetSkill, filePath, fragment.Slot, overlay.ID, "unsupported operation %q", fragment.Op))
		}
		if fragment.Order == 0 {
			overlay.Fragments[i].Order = 100
		}
		if fragment.Op != Suppress && strings.TrimSpace(fragment.Content) == "" {
			return Overlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, fragment.Slot, overlay.ID, "content is required for %s", fragment.Op))
		}
		if fragment.Op == Suppress && strings.TrimSpace(fragment.Content) != "" {
			return Overlay{}, skillOverlayErr(skillOverlayIssuef("invalid_operation", overlay.TargetSkill, filePath, fragment.Slot, overlay.ID, "suppress fragments must not include content"))
		}
	}
	return overlay, nil
}
