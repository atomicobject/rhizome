// Package userstate persists personal view preferences outside the derived index.
package userstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/paths"
)

const (
	MaxScopeBytes       = 4096
	MaxKeyBytes         = 128
	MaxValueBytes       = 64 << 10
	MaxValuesBytes      = 128 << 10
	MaxKeys             = 256
	MaxMigrationIDBytes = 1024
)

var ErrInvalid = errors.New("invalid view preferences")

// Context has the same subject union as the public web ViewContext.
type Context struct {
	Kind      viewconfig.MountKind `json:"kind"`
	Type      string               `json:"type,omitempty"`
	Interface string               `json:"interface,omitempty"`
	Group     string               `json:"group,omitempty"`
	Ref       *ontology.NodeRef    `json:"ref,omitempty"`
}

// Scope identifies one view instance. The containing database identifies its vault.
type Scope struct {
	ViewID     string  `json:"viewId"`
	Context    Context `json:"context"`
	Slot       string  `json:"slot,omitempty"`
	WidgetSlot string  `json:"widgetSlot,omitempty"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func validIdentifier(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

// CanonicalScope normalizes identity without requiring a currently registered
// view or resolving a possibly stale node against the live ontology.
func CanonicalScope(vaultPath string, scope Scope) (Scope, string, error) {
	encoded, err := json.Marshal(scope)
	if err != nil || len(encoded) > MaxScopeBytes {
		return Scope{}, "", invalid("scope exceeds %d bytes", MaxScopeBytes)
	}
	if !validIdentifier(scope.ViewID, 256) || (scope.Slot != "" && !validIdentifier(scope.Slot, 256)) || (scope.WidgetSlot != "" && !validIdentifier(scope.WidgetSlot, 256)) {
		return Scope{}, "", invalid("viewId, slot and widgetSlot must be bounded identifiers")
	}
	c := &scope.Context
	valid := false
	switch c.Kind {
	case viewconfig.MountKindStandalone:
		valid = c.Type == "" && c.Interface == "" && c.Group == "" && c.Ref == nil
	case viewconfig.MountKindType:
		valid = validIdentifier(c.Type, 256) && c.Type != "*" && c.Interface == "" && c.Group == "" && c.Ref == nil
	case viewconfig.MountKindInterface:
		valid = validIdentifier(c.Interface, 256) && c.Interface != "*" && c.Type == "" && c.Group == "" && c.Ref == nil
	case viewconfig.MountKindGroup:
		valid = validIdentifier(c.Group, 256) && c.Group != "*" && c.Type == "" && c.Interface == "" && c.Ref == nil
	case viewconfig.MountKindNode:
		valid = validIdentifier(c.Type, 256) && c.Type != "*" && c.Interface == "" && c.Group == "" && c.Ref != nil
		if valid {
			ref := *c.Ref
			if ref.TypeName != "" && ref.TypeName != c.Type {
				return Scope{}, "", invalid("node ref type differs from context type")
			}
			if ref.NotePath == "" || filepath.IsAbs(ref.NotePath) || strings.Contains(ref.NotePath, `\`) || (len(ref.NotePath) >= 2 && ref.NotePath[1] == ':') {
				return Scope{}, "", invalid("node notePath must be vault-relative")
			}
			vaultPaths, err := paths.NewVaultPaths(vaultPath)
			if err != nil {
				return Scope{}, "", err
			}
			notePath, err := vaultPaths.RelNotePathStrict(ref.NotePath)
			if err != nil || notePath.String() == "" || notePath.String() == "." {
				return Scope{}, "", invalid("node notePath is outside the vault")
			}
			ref.NotePath, ref.TypeName = notePath.String(), c.Type
			ref.StartByte, ref.EndByte, ref.ParentID = 0, 0, ""
			ref.Kind = ontology.NodeKind(strings.ToUpper(strings.TrimSpace(string(ref.Kind))))
			ref.Fragment = strings.TrimPrefix(ref.Fragment, "#")
			switch ref.Kind {
			case ontology.NodeKindNote:
				ref.Fragment, ref.NodeID, ref.Structural = "", "", ""
			case ontology.NodeKindEmbedded:
				if ref.NodeID != "" {
					ref.Fragment, ref.Structural = "", ""
				} else if ref.Structural != "" {
					ref.Fragment = ""
				} else if ref.Fragment == "" {
					return Scope{}, "", invalid("embedded node requires an identity")
				}
			case ontology.NodeKindSection:
				ref.NodeID = ""
				if ref.Fragment != "" {
					ref.NodeID, ref.Structural = "", ""
				} else if ref.Structural == "" {
					return Scope{}, "", invalid("section node requires an identity")
				}
			default:
				return Scope{}, "", invalid("unknown node kind")
			}
			c.Ref = &ref
		}
	}
	if !valid {
		return Scope{}, "", invalid("context must identify exactly one concrete subject")
	}
	encoded, err = json.Marshal(scope)
	if err != nil || len(encoded) > MaxScopeBytes {
		return Scope{}, "", invalid("scope is too large")
	}
	return scope, string(encoded), nil
}

func validateValues(values map[string]json.RawMessage) error {
	if len(values) > MaxKeys {
		return invalid("too many preference keys")
	}
	total := 0
	for key, value := range values {
		if !validIdentifier(key, MaxKeyBytes) {
			return invalid("invalid preference key")
		}
		if len(value) > MaxValueBytes || !json.Valid(value) {
			return invalid("invalid or oversized value for %q", key)
		}
		total += len(key) + len(value)
	}
	if total > MaxValuesBytes {
		return invalid("preferences exceed %d bytes", MaxValuesBytes)
	}
	return nil
}
