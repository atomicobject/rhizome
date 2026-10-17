package noderead

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownLinkTargetPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

func (s *Scope) resolveFragmentByProjection(ctx context.Context, notePath string, fragment string) (ontology.NodeRef, error) {
	ref := ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindEmbedded, Fragment: fragment}
	fragment = strings.TrimSpace(fragment)
	reservedSelector := false
	if nodeID := strings.TrimSpace(strings.TrimPrefix(fragment, "node:")); nodeID != fragment {
		if nodeID == "" {
			return ontology.NodeRef{}, fmt.Errorf("node id is required in %s", notePath)
		}
		ref.Kind = ontology.NodeKindSection
		ref.Fragment = ""
		ref.NodeID = nodeID
		reservedSelector = true
	} else if structural := strings.TrimPrefix(fragment, "struct:"); structural != fragment {
		ref.Kind = ontology.NodeKindSection
		ref.Fragment = ""
		ref.Structural = strings.TrimSpace(structural)
		reservedSelector = true
	}
	projection, err := s.Projection(ctx, ref)
	if err != nil {
		return ontology.NodeRef{}, err
	}
	if projection == nil || projection.Ref.IsZero() {
		return ontology.NodeRef{}, fmt.Errorf("fragment %q not found in %s", fragment, notePath)
	}
	ref = projection.Ref
	if ref.TypeName == "" {
		ref.TypeName = projection.ResolvedType
	}
	if ref.Fragment == "" && !reservedSelector {
		ref.Fragment = fragment
	}
	return ref, nil
}

func parseNodeRefJSON(input string) (ontology.NodeRef, bool) {
	if !strings.HasPrefix(strings.TrimSpace(input), "{") {
		return ontology.NodeRef{}, false
	}
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(input), &ref); err != nil || ref.IsZero() {
		return ontology.NodeRef{}, false
	}
	return ref, true
}

func unwrapResolveTarget(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "[[") && strings.HasSuffix(input, "]]") {
		input = strings.TrimSuffix(strings.TrimPrefix(input, "[["), "]]")
		if idx := strings.Index(input, "|"); idx >= 0 {
			input = input[:idx]
		}
		return strings.TrimSpace(input)
	}
	if match := markdownLinkTargetPattern.FindStringSubmatch(input); len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	if target := unwrapResolveTargetURL(input); target != "" {
		return target
	}
	return input
}

func unwrapResolveTargetURL(input string) string {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil || u.Scheme == "" {
		return ""
	}
	values := u.Query()
	for _, key := range []string{"ref", "note", "path", "file"} {
		value := strings.TrimSpace(values.Get(key))
		if value == "" {
			continue
		}
		if fragment := strings.TrimSpace(u.Fragment); fragment != "" && !strings.Contains(value, "#") {
			value += "#" + fragment
		}
		return value
	}
	return ""
}

func isLikelyMarkdownLink(input string) bool {
	return markdownLinkTargetPattern.MatchString(strings.TrimSpace(input))
}

func splitLocatorTarget(target string) (string, string) {
	target = strings.TrimSpace(target)
	if idx := strings.LastIndex(target, "#"); idx > 0 {
		return filepath.ToSlash(filepath.Clean(target[:idx])), strings.TrimSpace(target[idx+1:])
	}
	return filepath.ToSlash(filepath.Clean(target)), ""
}

func nodeRefFromCatalogRow(row readmodel.NodeRow) ontology.NodeRef {
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(row.NodeRefJSON), &ref); err != nil {
		ref = ontology.NodeRef{
			NotePath: row.NotePath,
			Kind:     ontology.NodeKind(row.NodeKind),
			NodeID:   row.NodeID,
			TypeName: row.TypeName,
			Fragment: row.Fragment,
		}
	}
	if ref.Fragment == "" && row.Fragment != "" {
		ref.Fragment = row.Fragment
	}
	if ref.TypeName == "" {
		ref.TypeName = row.TypeName
	}
	if ref.Kind == "" {
		ref.Kind = ontology.NodeKind(row.NodeKind)
	}
	if ref.Structural == "" {
		ref.Structural = strings.TrimSpace(row.StructuralFingerprint)
	}
	return ref
}
