package idalloc

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

type collectedIdentifierInventory struct {
	ownersScanned int
	siblings      []siblingType
	reservations  []ontology.IdentifierReservation
}

func collectIdentifierInventory(ctx context.Context, schema *ontology.Schema, store Store, typeName string, field *ontology.Field) (collectedIdentifierInventory, error) {
	// Gather every type that allocates from the same number-line. SPEC-0006
	// shares one `SPEC-XXXX` sequence across ProcessSpec, ProductSpec,
	// TechnicalSpec, ExperienceSpec, and OperationsSpec, so the allocator
	// must pool their ids before picking max+1.
	siblings := siblingTypesSharingFormat(schema, typeName, field.IdentifierFormat)

	allPaths := make([]string, 0)
	pathOwnerType := make(map[string]string)
	pathSourceField := make(map[string]string)
	nodeOwners := make(map[string]identifierNodeOwner)
	nodeIDs := make([]string, 0)
	nodeFields := make(map[string]struct{})
	for _, sib := range siblings {
		if sib.role != ontology.TypeRoleNote {
			nodes, err := store.OntologyNodesByType(ctx, sib.typeName)
			if err != nil {
				return collectedIdentifierInventory{}, fmt.Errorf("list identifier nodes for %s: %w", sib.typeName, err)
			}
			for _, node := range nodes {
				if _, seen := nodeOwners[node.NodeID]; seen {
					continue
				}
				nodeOwners[node.NodeID] = identifierNodeOwner{locator: node.SourceLocator, field: sib.field, aliases: sib.aliases}
				nodeIDs = append(nodeIDs, node.NodeID)
			}
			nodeFields[sib.field] = struct{}{}
			if field.IdentifierFormat.Strategy == ontology.IdentifierStrategyDateTime {
				for _, name := range sib.aliases {
					nodeFields[name] = struct{}{}
				}
			}
			continue
		}
		paths, err := store.OntologyPathsByType(ctx, sib.typeName, 0)
		if err != nil {
			return collectedIdentifierInventory{}, fmt.Errorf("list typed paths for %s: %w", sib.typeName, err)
		}
		for _, p := range paths {
			if _, seen := pathOwnerType[p]; seen {
				continue
			}
			pathOwnerType[p] = sib.typeName
			pathSourceField[p] = sib.source
			allPaths = append(allPaths, p)
		}
	}
	sort.Strings(allPaths)

	// Note roots retain raw source values, including off-pattern diagnostics.
	// Node-scoped rows below use declared field names rather than raw sources.
	sourceSet := make(map[string]struct{})
	for _, sib := range siblings {
		if sib.role == ontology.TypeRoleNote {
			sourceSet[sib.source] = struct{}{}
		}
	}
	sourceList := make([]string, 0, len(sourceSet))
	for s := range sourceSet {
		sourceList = append(sourceList, s)
	}
	sort.Strings(sourceList)

	var rows []semdb.NotePropertyValueRow
	// Empty path filters mean all notes in the raw-property API.
	if len(allPaths) > 0 {
		var err error
		rows, err = store.CurrentNotePropertyValues(ctx, allPaths, sourceList, semdb.NotePropertySourceFrontmatter)
		if err != nil {
			return collectedIdentifierInventory{}, fmt.Errorf("read identifier values: %w", err)
		}
	}

	reservations := make([]ontology.IdentifierReservation, 0, len(rows))
	seenReservations := make(map[string]struct{}, len(rows))
	addReservation := func(ownerKey, owner, raw string) {
		value := strings.TrimSpace(raw)
		if value == "" {
			return
		}
		key := ownerKey + "\x00" + ontology.NormalizeIdentifierSemanticValue(value)
		if _, seen := seenReservations[key]; seen {
			return
		}
		seenReservations[key] = struct{}{}
		reservations = append(reservations, ontology.IdentifierReservation{Value: value, Owner: owner})
	}
	for _, row := range rows {
		// Only consider the row's value when the matched property is the
		// allocator's source for that note's owning type. Otherwise
		// `legacyId` rows from a sibling type would leak into the pool.
		if pathSourceField[row.NotePath] != row.PropertyName {
			continue
		}
		addReservation("note:"+row.NotePath, row.NotePath, row.ValueText)
	}
	if len(allPaths) > 0 && field.IdentifierFormat.Strategy == ontology.IdentifierStrategyDateTime {
		strategy, strategyErr := field.IdentifierFormat.StrategyContract()
		if strategyErr != nil {
			return collectedIdentifierInventory{}, fmt.Errorf("load identifier strategy for aliases: %w", strategyErr)
		}
		aliasRows, err := store.CurrentNotePropertyValues(ctx, allPaths, []string{"aliases"}, semdb.NotePropertySourceFrontmatter)
		if err != nil {
			return collectedIdentifierInventory{}, fmt.Errorf("read identifier aliases: %w", err)
		}
		for _, row := range aliasRows {
			if _, belongsToPool := pathOwnerType[row.NotePath]; !belongsToPool {
				continue
			}
			value := strings.TrimSpace(row.ValueText)
			if value == "" {
				continue
			}
			if _, ok := strategy.Parse(value); !ok {
				continue
			}
			addReservation("note:"+row.NotePath, row.NotePath, value)
		}
	}
	if len(nodeIDs) > 0 {
		fieldNames := make([]string, 0, len(nodeFields))
		for name := range nodeFields {
			fieldNames = append(fieldNames, name)
		}
		sort.Strings(fieldNames)
		fieldRows, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, fieldNames)
		if err != nil {
			return collectedIdentifierInventory{}, fmt.Errorf("read node identifier values: %w", err)
		}
		strategy, err := field.IdentifierFormat.StrategyContract()
		if err != nil {
			return collectedIdentifierInventory{}, fmt.Errorf("load node identifier strategy: %w", err)
		}
		for _, row := range fieldRows {
			owner, ok := nodeOwners[row.NodeID]
			if !ok {
				continue
			}
			if !strings.EqualFold(row.FieldName, owner.field) {
				if field.IdentifierFormat.Strategy != ontology.IdentifierStrategyDateTime || !slices.ContainsFunc(owner.aliases, func(name string) bool { return strings.EqualFold(row.FieldName, name) }) {
					continue
				}
				if _, ok := strategy.Parse(strings.TrimSpace(row.ValueText)); !ok {
					continue
				}
			}
			addReservation("node:"+row.NodeID, owner.locator, row.ValueText)
		}
	}
	return collectedIdentifierInventory{ownersScanned: len(allPaths) + len(nodeOwners), siblings: siblings, reservations: reservations}, nil
}

type identifierNodeOwner struct {
	locator string
	field   string
	aliases []string
}

type siblingType struct {
	typeName string
	source   string
	field    string
	aliases  []string
	role     ontology.TypeRole
}

// siblingTypesSharingFormat returns every typed note family in the schema
// whose preferred identifier shares the requested rendered namespace. Schema
// compilation has already rejected incompatible strategies and formats inside
// one namespace, so pool discovery must not repeat strategy-specific rules.
// The requested type is always included so the caller can iterate uniformly.
func siblingTypesSharingFormat(schema *ontology.Schema, requested string, format *ontology.IdentifierFormat) []siblingType {
	namespace := strings.ToLower(format.Prefix + format.Separator)
	out := make([]siblingType, 0, 4)
	for name, nt := range schema.Types {
		if nt == nil {
			continue
		}
		f := preferredIdentifierField(nt)
		if f == nil || f.IdentifierFormat == nil || f.IsDerivableIdentifier || strings.TrimSpace(f.DerivedSuffix) != "" {
			continue
		}
		if strings.ToLower(f.IdentifierFormat.Prefix+f.IdentifierFormat.Separator) != namespace {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(f.Source))
		if source == "" {
			source = strings.ToLower(f.Name)
		}
		var aliases []string
		for _, candidate := range nt.Fields {
			if candidate != nil && (strings.EqualFold(candidate.Name, "aliases") || strings.EqualFold(candidate.Name, "alias")) {
				aliases = append(aliases, candidate.Name)
			}
		}
		out = append(out, siblingType{typeName: name, source: source, field: f.Name, aliases: aliases, role: nt.Role})
	}
	// Stable iteration: requested type first, others alphabetical.
	sort.Slice(out, func(i, j int) bool {
		if out[i].typeName == requested {
			return true
		}
		if out[j].typeName == requested {
			return false
		}
		return out[i].typeName < out[j].typeName
	})
	return out
}

func preferredIdentifierField(nt *ontology.NoteType) *ontology.Field {
	if nt == nil {
		return nil
	}
	for _, f := range nt.Fields {
		if f != nil && f.IsPreferredIdentifier {
			return f
		}
	}
	return nil
}
