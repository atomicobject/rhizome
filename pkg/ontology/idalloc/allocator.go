// Package idalloc resolves the next available identifier for a typed node
// family that declares an @identifier(preferred: true, prefix: "...") field.
//
// The allocator is the canonical replacement for skill-side identifier
// recipes described in docs/specs/process/id-allocation.md. It reads the typed
// note family through the existing ontology store and delegates syntax and
// reservation behavior to the compiled identifier strategy. Sequential gaps
// remain preserved per SPEC-0006; datetime families use their lowest free
// collision ordinal.
package idalloc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// Store is the minimal subset of *semdb.Store the allocator needs. Defining it
// here lets tests substitute an in-memory fake without dragging in SQLite.
type Store interface {
	OntologyNodesByType(context.Context, string) ([]codeanchor.IntelOntologyNode, error)
	OntologyNodeFieldValuesByNodeIDs(context.Context, []string, []string) ([]codeanchor.IntelOntologyNodeFieldValue, error)
	OntologyPathsByType(ctx context.Context, typeName string, limit int) ([]string, error)
	CurrentNotePropertyValues(ctx context.Context, notePaths []string, onlyProperties []string, sourceFilter semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error)
}

const MaxBatchCount = 100

// Result describes the next allocation for a typed note family. It is the
// canonical JSON shape returned by `rzm agent next-id`.
type Result struct {
	Type            string                      `json:"type"`
	IdentifierField string                      `json:"identifierField"`
	Strategy        ontology.IdentifierStrategy `json:"strategy"`
	Source          string                      `json:"source"`
	Prefix          string                      `json:"prefix"`
	Separator       string                      `json:"separator"`
	Pad             *int                        `json:"pad,omitempty"`
	Next            string                      `json:"next"`
	IDs             []string                    `json:"ids"`
	Count           int                         `json:"count"`
	Last            string                      `json:"last,omitempty"`
	CurrentMax      *int                        `json:"currentMax,omitempty"`
	CurrentMaxValue string                      `json:"currentMaxValue,omitempty"`
	CurrentMaxOwner string                      `json:"currentMaxOwner,omitempty"`
	OwnersScanned   int                         `json:"ownersScanned"`
	// SharedWith lists the other ontology types that allocate from the same
	// `<prefix><separator>` sequence. SPEC-0006 explicitly shares one SPEC
	// number-line across all spec variants, so an allocator that only scans
	// the requested type would happily hand out a value already claimed by
	// a sibling type.
	SharedWith    []string     `json:"sharedWith,omitempty"`
	OwnersMatched []string     `json:"ownersMatched,omitempty"`
	OwnersSkipped []string     `json:"ownersSkipped,omitempty"`
	Notes         []string     `json:"notes,omitempty"`
	Paths         []string     `json:"paths,omitempty"`
	Allocations   []Allocation `json:"allocations,omitempty"`
}

// Request is the strategy-neutral allocation request. Paths are prospective
// vault-relative note paths and retain caller order. Sequential allocation
// uses Count; filename-derived strategies use Paths.
type Request struct {
	Type  string
	Count int
	Paths []string
}

// Allocation correlates one prospective path with its allocated identifier.
// Base and Disambiguator are populated by strategies that allocate within a
// filename-derived reservation family.
type Allocation struct {
	Path          string `json:"path"`
	ID            string `json:"id"`
	Base          string `json:"base,omitempty"`
	Disambiguator *int   `json:"disambiguator,omitempty"`
}

// Sentinel errors. Callers (CLI, MCP) translate these into stable JSON error
// codes so skills can branch on them.
var (
	ErrSchemaMissing         = errors.New("ontology schema unavailable")
	ErrStoreMissing          = errors.New("ontology store unavailable")
	ErrTypeNotFound          = errors.New("type_not_found")
	ErrNoPreferredIdentifier = errors.New("no_preferred_identifier")
	ErrUnsupportedForType    = errors.New("unsupported_for_type")
	ErrInvalidInput          = errors.New("invalid_input")
	ErrIdentifierExhausted   = errors.New("identifier_exhausted")
)

// ErrorCode returns the stable JSON code agents should branch on. Other errors
// (DB, IO) are reported as "internal_error".
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTypeNotFound):
		return "type_not_found"
	case errors.Is(err, ErrNoPreferredIdentifier):
		return "no_preferred_identifier"
	case errors.Is(err, ErrUnsupportedForType):
		return "unsupported_for_type"
	case errors.Is(err, ErrInvalidInput):
		return "invalid_input"
	case errors.Is(err, ErrIdentifierExhausted):
		return "identifier_exhausted"
	case errors.Is(err, ErrSchemaMissing), errors.Is(err, ErrStoreMissing):
		return "runtime_unavailable"
	default:
		return "internal_error"
	}
}

// Allocate computes the next id for typeName by reading the typed note family
// through store and parsing ids that match the schema's declared format.
//
// Behaviour:
//   - Empty vault (no prior matching ids) returns "<prefix>-0001".
//   - Off-pattern ids (legacy `SPEC-0001a`, swapped prefix, etc.) are listed in
//     OwnersSkipped but never consume a number.
//   - Gaps are preserved: max+1 wins, never the lowest available number.
func Allocate(ctx context.Context, schema *ontology.Schema, store Store, typeName string) (*Result, error) {
	return AllocateRequest(ctx, schema, store, Request{Type: typeName, Count: 1})
}

// AllocateBatch computes count contiguous ids for typeName from the same
// observed max. Use this when an authoring session creates multiple notes
// before the index has seen the freshly-written files.
func AllocateBatch(ctx context.Context, schema *ontology.Schema, store Store, typeName string, count int) (*Result, error) {
	return AllocateRequest(ctx, schema, store, Request{Type: typeName, Count: count})
}

// AllocateRequest dispatches allocation from the compiled identifier strategy.
func AllocateRequest(ctx context.Context, schema *ontology.Schema, store Store, request Request) (*Result, error) {
	if schema == nil {
		return nil, ErrSchemaMissing
	}
	typeName := strings.TrimSpace(request.Type)
	if typeName == "" {
		return nil, fmt.Errorf("%w: type name required", ErrTypeNotFound)
	}
	nt := schema.Types[typeName]
	if nt == nil {
		return nil, fmt.Errorf("%w: %q is not a typed note family", ErrTypeNotFound, typeName)
	}
	field := preferredIdentifierField(nt)
	if field == nil {
		return nil, fmt.Errorf("%w: type %q has no @identifier(preferred: true) field", ErrNoPreferredIdentifier, typeName)
	}
	if field.IsDerivableIdentifier || strings.TrimSpace(field.DerivedSuffix) != "" {
		return nil, fmt.Errorf("%w: %s.%s is derived from its parent and bypasses identifier allocation", ErrUnsupportedForType, typeName, field.Name)
	}
	if field.IdentifierFormat == nil {
		return nil, fmt.Errorf("%w: %s.%s does not declare prefix; add prefix:\"...\" to @identifier or allocate manually", ErrUnsupportedForType, typeName, field.Name)
	}

	count := request.Count
	if count < 1 {
		count = 1
	}
	if count > MaxBatchCount {
		count = MaxBatchCount
	}
	paths := append([]string(nil), request.Paths...)
	result := requestResult(typeName, field, count, paths)

	strategy, err := field.IdentifierFormat.StrategyContract()
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrUnsupportedForType, err)
	}
	strategyRequest := ontology.IdentifierAllocationRequest{Count: count, Paths: paths}
	if err := strategy.ValidateAllocationRequest(strategyRequest); err != nil {
		return result, translateStrategyError(err)
	}
	if store == nil {
		return result, ErrStoreMissing
	}
	inventory, err := collectIdentifierInventory(ctx, schema, store, typeName, field)
	if err != nil {
		return result, err
	}
	allocation, err := strategy.Allocate(strategyRequest, ontology.IdentifierReservationInventory{Reservations: inventory.reservations})
	if err != nil {
		return result, translateStrategyError(err)
	}
	return buildResult(result, inventory, strategy, allocation)
}

func requestResult(typeName string, field *ontology.Field, count int, paths []string) *Result {
	source := strings.ToLower(strings.TrimSpace(field.Source))
	if source == "" {
		source = strings.ToLower(field.Name)
	}
	result := &Result{
		Type:            typeName,
		IdentifierField: field.Name,
		Strategy:        field.IdentifierFormat.Strategy,
		Source:          source,
		Prefix:          field.IdentifierFormat.Prefix,
		Separator:       field.IdentifierFormat.Separator,
		Count:           count,
		Paths:           paths,
	}
	if field.IdentifierFormat.Strategy == "" || field.IdentifierFormat.Strategy == ontology.IdentifierStrategySequential {
		pad := field.IdentifierFormat.Pad
		result.Pad = &pad
	}
	return result
}

func buildResult(result *Result, inventory collectedIdentifierInventory, strategy ontology.IdentifierStrategyContract, allocation ontology.IdentifierAllocationResult) (*Result, error) {
	result.IDs = make([]string, 0, len(allocation.Values))
	result.Allocations = make([]Allocation, 0, len(allocation.Values))
	for _, value := range allocation.Values {
		id, err := strategy.Format(value.Value)
		if err != nil {
			return result, fmt.Errorf("format allocated identifier: %w", err)
		}
		result.IDs = append(result.IDs, id)
		if value.Path == "" {
			continue
		}
		family, err := strategy.ReservationFamily(value.Value)
		if err != nil {
			return result, fmt.Errorf("derive allocated identifier reservation family: %w", err)
		}
		item := Allocation{Path: value.Path, ID: id, Base: string(family)}
		if value.Value.Ordinal > 1 {
			item.Disambiguator = intPointer(value.Value.Ordinal)
		}
		result.Allocations = append(result.Allocations, item)
	}
	result.Count = len(result.IDs)
	if len(result.IDs) > 0 {
		result.Next = result.IDs[0]
		result.Last = result.IDs[len(result.IDs)-1]
	}
	result.OwnersScanned = inventory.ownersScanned
	result.OwnersMatched = renderReservations(allocation.MatchedReservations, false)
	result.OwnersSkipped = renderReservations(allocation.SkippedReservations, true)
	if result.Strategy == ontology.IdentifierStrategySequential {
		currentMax := 0
		if allocation.HighWatermark != nil {
			currentMax = allocation.HighWatermark.Value.Number
			result.CurrentMaxValue = allocation.HighWatermark.Reservation.Value
			result.CurrentMaxOwner = allocation.HighWatermark.Reservation.Owner
		}
		result.CurrentMax = intPointer(currentMax)
	}

	notes := make([]string, 0, 3)
	if inventory.ownersScanned == 0 {
		if result.Strategy == ontology.IdentifierStrategySequential {
			notes = append(notes, "no notes of this type yet; starting from 1")
		} else {
			notes = append(notes, "no notes of this type yet")
		}
	}
	if len(result.OwnersSkipped) > 0 {
		notes = append(notes, fmt.Sprintf("%d off-pattern identifier value(s) ignored", len(result.OwnersSkipped)))
	}
	sharedWith := make([]string, 0, len(inventory.siblings))
	for _, sib := range inventory.siblings {
		if sib.typeName == result.Type {
			continue
		}
		sharedWith = append(sharedWith, sib.typeName)
	}
	sort.Strings(sharedWith)
	if len(sharedWith) > 0 {
		notes = append(notes, fmt.Sprintf("identifier sequence %q is shared with %d sibling type(s); allocator scanned them all", result.Prefix+result.Separator, len(sharedWith)))
	}
	result.SharedWith = sharedWith
	result.Notes = notes
	return result, nil
}

func renderReservations(reservations []ontology.IdentifierReservation, quoteValue bool) []string {
	out := make([]string, 0, len(reservations))
	for _, reservation := range reservations {
		if quoteValue {
			out = append(out, fmt.Sprintf("%s=%q", reservation.Owner, reservation.Value))
		} else {
			out = append(out, fmt.Sprintf("%s=%s", reservation.Owner, reservation.Value))
		}
	}
	sort.Strings(out)
	return out
}

func intPointer(value int) *int { return &value }
