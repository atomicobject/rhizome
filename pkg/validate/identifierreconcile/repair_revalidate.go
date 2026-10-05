package identifierreconcile

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// RevalidateCompleteSnapshot proves that the current schema and complete
// Markdown source inventory still match the sealed repair assembly. The EFF-0047
// adapter calls this while holding its opaque IndexLockLease, immediately before
// mapping physical operations; it does not create or acquire a competing lease.
func (a *RepairAssembly) RevalidateCompleteSnapshot(
	ctx context.Context,
	vaultDef obsidian.VaultDefinition,
) error {
	snapshot, err := a.ValidatedSnapshot()
	if err != nil {
		return err
	}
	basePath := strings.TrimSpace(vaultDef.BasePath())
	if basePath == "" {
		return fmt.Errorf("identifier repair revalidation requires vault root")
	}
	schema, err := ontology.LoadSchema(basePath)
	if err != nil {
		return fmt.Errorf("reload identifier repair ontology schema: %w", err)
	}
	if schema == nil || strings.TrimSpace(schema.Hash) == "" {
		return fmt.Errorf("reload identifier repair ontology schema: compiled schema is unavailable")
	}
	if schema.Hash != snapshot.SchemaHash {
		return fmt.Errorf("identifier repair schema changed after planning")
	}
	sources, err := notemeta.BuildNoteSourceFacts(ctx, vaultDef)
	if err != nil {
		return fmt.Errorf("rebuild identifier repair source inventory: %w", err)
	}
	current := make([]SourcePrecondition, 0, len(sources))
	for _, source := range sources {
		current = append(current, SourcePrecondition{
			NotePath:   source.Path.String(),
			SourceHash: source.ContentHash,
		})
	}
	if err := validateSourcePreconditions(current); err != nil {
		return fmt.Errorf("rebuild identifier repair source inventory: %w", err)
	}
	if !slices.Equal(current, snapshot.SourcePreconditions) {
		return fmt.Errorf("identifier repair source inventory changed after planning")
	}
	return nil
}
