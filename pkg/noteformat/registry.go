package noteformat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

var (
	// ErrDuplicateFormatID reports case-insensitively duplicate provider IDs.
	ErrDuplicateFormatID = errors.New("duplicate note format ID")
	// ErrDuplicateExtension reports a case-insensitively overlapping extension claim.
	ErrDuplicateExtension = errors.New("duplicate note format extension")
)

// Registry is an immutable, closed collection of registered note providers.
// It matches format IDs and extensions case-insensitively while never changing
// authored path casing.
type Registry struct {
	byID        map[string]Provider
	byExtension map[string]Provider
	ids         []FormatID
}

// NewRegistry validates providers and returns an immutable registry. Provider
// descriptors are copied during assembly so later provider-owned slice mutation
// cannot change registry behavior.
func NewRegistry(providers ...Provider) (Registry, error) {
	registry := Registry{
		byID:        make(map[string]Provider, len(providers)),
		byExtension: make(map[string]Provider),
	}
	for _, provider := range providers {
		if provider == nil {
			return Registry{}, errors.New("note format provider is required")
		}
		descriptor, err := normalizeDescriptor(provider.Descriptor())
		if err != nil {
			return Registry{}, err
		}
		idKey := fold(descriptor.ID)
		if _, exists := registry.byID[idKey]; exists {
			return Registry{}, fmt.Errorf("%w: %q", ErrDuplicateFormatID, descriptor.ID)
		}
		frozen := descriptorProvider{descriptor: descriptor}
		registry.byID[idKey] = frozen
		registry.ids = append(registry.ids, descriptor.ID)
		for _, extension := range descriptor.Extensions {
			extensionKey := foldExtension(extension)
			if existing, exists := registry.byExtension[extensionKey]; exists {
				return Registry{}, fmt.Errorf("%w: %q claimed by %q and %q", ErrDuplicateExtension, extension, existing.Descriptor().ID, descriptor.ID)
			}
			registry.byExtension[extensionKey] = frozen
		}
	}
	sort.Slice(registry.ids, func(i, j int) bool { return registry.ids[i] < registry.ids[j] })
	return registry, nil
}

// Provider returns the provider for id using case-insensitive matching.
func (r Registry) Provider(id FormatID) (Provider, bool) {
	provider, ok := r.byID[fold(id)]
	return provider, ok
}

// ProviderForPath returns the provider that claims path's extension using
// case-insensitive matching. It does not normalize or mutate path.
func (r Registry) ProviderForPath(path paths.RelPath) (Provider, bool) {
	extension := filepath.Ext(path.String())
	provider, ok := r.byExtension[foldExtension(extension)]
	return provider, ok
}

// IDs returns the registered provider IDs in deterministic order.
func (r Registry) IDs() []FormatID {
	return append([]FormatID(nil), r.ids...)
}

// ManifestFingerprint returns a deterministic fingerprint of the registry's
// semantic provider manifest. Provider assembly order and extension ordering
// do not affect the result.
func (r Registry) ManifestFingerprint() string {
	type entry struct {
		ID                string   `json:"id"`
		Extensions        []string `json:"extensions"`
		ProviderVersion   string   `json:"providerVersion"`
		ProjectionVersion string   `json:"projectionVersion"`
		OwnershipPolicy   string   `json:"ownershipPolicy"`
		Capabilities      []string `json:"capabilities"`
	}
	entries := make([]entry, 0, len(r.ids))
	for _, id := range r.ids {
		provider, ok := r.Provider(id)
		if !ok {
			continue
		}
		descriptor := provider.Descriptor()
		extensions := make([]string, len(descriptor.Extensions))
		for index, extension := range descriptor.Extensions {
			extensions[index] = foldExtension(extension)
		}
		sort.Strings(extensions)
		capabilities := descriptor.Capabilities.Values()
		capabilityNames := make([]string, len(capabilities))
		for index, capability := range capabilities {
			capabilityNames[index] = string(capability)
		}
		entries = append(entries, entry{
			ID:                fold(descriptor.ID),
			Extensions:        extensions,
			ProviderVersion:   descriptor.ProviderVersion,
			ProjectionVersion: descriptor.ProjectionVersion,
			OwnershipPolicy:   string(descriptor.OwnershipPolicy),
			Capabilities:      capabilityNames,
		})
	}
	encoded, _ := json.Marshal(entries)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

type descriptorProvider struct {
	descriptor Descriptor
}

func (p descriptorProvider) Descriptor() Descriptor {
	return cloneDescriptor(p.descriptor)
}

func normalizeDescriptor(descriptor Descriptor) (Descriptor, error) {
	descriptor.ID = FormatID(fold(descriptor.ID))
	if descriptor.ID == "" {
		return Descriptor{}, errors.New("note format ID is required")
	}
	descriptor.ProviderVersion = strings.TrimSpace(descriptor.ProviderVersion)
	if descriptor.ProviderVersion == "" {
		return Descriptor{}, fmt.Errorf("note format %q provider version is required", descriptor.ID)
	}
	descriptor.ProjectionVersion = strings.TrimSpace(descriptor.ProjectionVersion)
	if descriptor.ProjectionVersion == "" {
		return Descriptor{}, fmt.Errorf("note format %q projection version is required", descriptor.ID)
	}
	descriptor.OwnershipPolicy = OwnershipPolicy(strings.TrimSpace(string(descriptor.OwnershipPolicy)))
	if descriptor.OwnershipPolicy != OwnershipDefault && descriptor.OwnershipPolicy != OwnershipExplicitInclude {
		return Descriptor{}, fmt.Errorf("note format %q has invalid ownership policy %q", descriptor.ID, descriptor.OwnershipPolicy)
	}
	if !descriptor.Capabilities.valid() {
		return Descriptor{}, fmt.Errorf("note format %q has invalid capabilities", descriptor.ID)
	}
	if len(descriptor.Extensions) == 0 {
		return Descriptor{}, fmt.Errorf("note format %q must claim an extension", descriptor.ID)
	}
	descriptor.Extensions = append([]string(nil), descriptor.Extensions...)
	seen := make(map[string]struct{}, len(descriptor.Extensions))
	for index, extension := range descriptor.Extensions {
		extension = strings.TrimSpace(extension)
		if !strings.HasPrefix(extension, ".") || len(extension) == 1 {
			return Descriptor{}, fmt.Errorf("note format %q has invalid extension %q", descriptor.ID, descriptor.Extensions[index])
		}
		key := foldExtension(extension)
		if _, exists := seen[key]; exists {
			return Descriptor{}, fmt.Errorf("%w: %q", ErrDuplicateExtension, extension)
		}
		seen[key] = struct{}{}
		descriptor.Extensions[index] = extension
	}
	return descriptor, nil
}

func descriptorClaimsPath(descriptor Descriptor, path paths.NotePath) bool {
	extension := foldExtension(filepath.Ext(path.String()))
	for _, claimed := range descriptor.Extensions {
		if foldExtension(claimed) == extension {
			return true
		}
	}
	return false
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	descriptor.Extensions = append([]string(nil), descriptor.Extensions...)
	return descriptor
}

func fold(value FormatID) string {
	return strings.ToLower(strings.TrimSpace(string(value)))
}

func foldExtension(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
