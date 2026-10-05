package actions

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// A registered note format cannot fall through to attachment maintenance when
// its mutation adapter is unavailable. Check the complete move set before writes.
func validateNoteMoveFormats(indexer notemeta.Indexer, source, target string) error {
	sourceFormat, err := noteMutationFormat(indexer, source, noteformat.CapabilityFileRenameMove)
	if err != nil {
		return err
	}
	targetFormat, err := noteMutationFormat(indexer, target, noteformat.CapabilityFileRenameMove)
	if err != nil {
		return err
	}
	if sourceFormat != targetFormat {
		return fmt.Errorf("unsupported note format change from %q to %q: rename does not convert content", source, target)
	}
	return nil
}

func noteMutationFormat(indexer notemeta.Indexer, input string, capability noteformat.Capability) (noteformat.FormatID, error) {
	formats, err := indexer.FormatRuntime()
	if err != nil {
		return "", err
	}
	path, err := paths.CleanRelPath(input)
	if err != nil {
		return "", err
	}
	provider, known := formats.ProviderForPath(path)
	if !known {
		if capability == noteformat.CapabilityFileRenameMove {
			return "", nil // Attachments retain the existing file-move workflow.
		}
		return "", fmt.Errorf("unsupported %s for %q: no note format provider", capability, input)
	}
	descriptor := provider.Descriptor()
	if !formats.CanProject(descriptor.ID) || !descriptor.Capabilities.Has(capability) {
		return "", fmt.Errorf("unsupported %s for %q: note format %q has no supported mutation adapter", capability, input, descriptor.ID)
	}
	return descriptor.ID, nil
}
