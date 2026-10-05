package views

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// laneFieldNone turns board lanes off.
const laneFieldNone = "none"

// maxLaneTargetsPerRecord is the mean targets per filled record above which
// a relation field makes poor default lanes: records would repeat too often.
const maxLaneTargetsPerRecord = 1.25

// checkBoardFields rejects a requested column field other than the view's
// configured one or the profile's single-valued lifecycle and ordered fields,
// and a requested lane field that does not resolve or repeats the column
// field. A configured lane field that fails the same check does not break the
// view: it yields a warning and state falls back to the profile default lane,
// as a configured column field that cannot hold columns yields
// view_kanban_column_unsupported.
func checkBoardFields(def viewconfig.ViewDefinition, profile *ontology.TypeProfile, state *ExecutionState, laneRequested bool, capabilities []FieldCapability) (*Warning, error) {
	if def.Variants.Kanban == nil || state.Variant != "kanban" {
		return nil, nil
	}
	allowed := []string{strings.TrimSpace(def.Variants.Kanban.ColumnField)}
	if profile != nil {
		for _, name := range append([]string{profile.LifecycleField}, profile.OrderedFields...) {
			if capability, ok := resolveCapability(capabilities, name); ok && !capability.List {
				allowed = append(allowed, name)
			}
		}
	}
	if !slices.Contains(allowed, state.ColumnField) {
		return nil, fmt.Errorf("%w: board column field %q must be the view's column field or a single-valued lifecycle or ordered field", ErrInvalidRequest, state.ColumnField)
	}
	lane := state.LaneField
	if lane == "" || lane == laneFieldNone {
		return nil, nil
	}
	if capability, ok := resolveCapability(capabilities, lane); ok && capability.Key != capabilityField(capabilities, state.ColumnField) {
		return nil, nil
	}
	message := fmt.Sprintf("board lane field %q must be a field other than the column field", lane)
	if laneRequested {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRequest, message)
	}
	state.LaneField = ""
	return &Warning{Code: "view_kanban_lane_unsupported", Message: message + "; using the default lanes", Path: "variants.kanban.laneField"}, nil
}

// defaultLaneField is SPEC-0112's lane default: the first KEY ordered field
// any matching record fills, else the first KEY relation field whose filled
// records average at most 1.25 targets, else none.
func defaultLaneField(profile *ontology.TypeProfile, columnField string, capabilities []FieldCapability, rows []TableRow) string {
	if profile == nil {
		return ""
	}
	keyCapability := func(name string) (FieldCapability, bool) {
		capability, ok := resolveCapability(capabilities, name)
		ok = ok && capability.Key != columnField && effectiveFieldImportance(capability.Importance) == ontology.FieldImportanceKey
		return capability, ok
	}
	for _, name := range profile.OrderedFields {
		if capability, ok := keyCapability(name); ok && slices.ContainsFunc(rows, func(row TableRow) bool { return rowFillsField(row, capability) }) {
			return capability.Key
		}
	}
	for _, name := range profile.RelationFields {
		capability, ok := keyCapability(name)
		if !ok {
			continue
		}
		filled, targets := 0, 0
		for _, row := range rows {
			if count := len(laneValues(row, capability)); count > 0 {
				filled++
				targets += count
			}
		}
		if filled > 0 && float64(targets) <= maxLaneTargetsPerRecord*float64(filled) {
			return capability.Key
		}
	}
	return ""
}

type laneValue struct{ identity, value, label string }

// laneValues lists a row's distinct values of the lane field; relation values
// key on their resolved target like link groups do.
func laneValues(row TableRow, capability FieldCapability) []laneValue {
	raw, ok := capabilityFieldValue(row, capability.Key, capability)
	if !ok {
		return nil
	}
	relation := normalizedValueKind(capability.ValueKind) == "relation"
	var links map[string]TableRelationValue
	if relation {
		links = rowRelationValues(row, capability.Key, capability)
	}
	var out []laneValue
	seen := map[string]bool{}
	for _, item := range groupValues(raw) {
		value := strings.TrimSpace(groupBucketValue(item, capability))
		if value == "" {
			continue
		}
		next := laneValue{identity: value, value: value}
		if relation {
			next.identity, next.value, next.label = relationGroupIdentity(value, links)
		}
		if !seen[next.identity] {
			seen[next.identity] = true
			out = append(out, next)
		}
	}
	return out
}

type laneBuild struct {
	lane  BoardLane
	cells map[int][]int
	order int
	stage ontology.LifecycleStage
}

// boardLanes splits the board's page rows into lanes; each lane's cells
// follow the board's columns and index into rows, so a row with several lane
// values appears in each of those lanes. Enum lanes follow the enum; relation
// lanes follow their target's lifecycle stage, read from the statuses
// hydrateRelationStatuses set on the rows, then size; the lane without a value
// comes last.
func boardLanes(board *BoardLayout, capability FieldCapability, rows []TableRow) []BoardLane {
	enumOrder := enumOrderMap(capability)
	builds := map[string]*laneBuild{}
	var order []string
	for column, boardColumn := range board.Columns {
		for index := boardColumn.RowStart; index < boardColumn.RowEnd && index < len(rows); index++ {
			values := laneValues(rows[index], capability)
			if len(values) == 0 {
				values = []laneValue{{}}
			}
			for _, value := range values {
				build := builds[value.identity]
				if build == nil {
					build = &laneBuild{
						lane: BoardLane{
							Key:   groupKey("", capability.Key, value.value),
							Value: value.value,
							Label: laneLabel(value, capability),
							Tone:  groupTone(value.value, capability),
						},
						cells: map[int][]int{},
						order: len(enumOrder),
					}
					if position, ok := enumOrder[value.value]; ok {
						build.order = position
					}
					if status := laneTargetStatus(rows[index], capability, value); status != nil {
						build.stage = status.Stage
						build.lane.Tone = cmp.Or(status.Tone, build.lane.Tone)
					}
					builds[value.identity] = build
					order = append(order, value.identity)
				}
				build.cells[column] = append(build.cells[column], index)
				build.lane.Count++
			}
		}
	}
	out := make([]*laneBuild, 0, len(order))
	for _, identity := range order {
		out = append(out, builds[identity])
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if (left.lane.Value == "") != (right.lane.Value == "") {
			return right.lane.Value == ""
		}
		if left.order != right.order {
			return left.order < right.order
		}
		if rank := cmp.Compare(stageRank(left.stage), stageRank(right.stage)); rank != 0 {
			return rank < 0
		}
		if left.lane.Count != right.lane.Count {
			return left.lane.Count > right.lane.Count
		}
		return strings.ToLower(left.lane.Label) < strings.ToLower(right.lane.Label)
	})
	lanes := make([]BoardLane, 0, len(out))
	for _, build := range out {
		build.lane.Cells = make([]BoardLaneCell, 0, len(board.Columns))
		for column, boardColumn := range board.Columns {
			build.lane.Cells = append(build.lane.Cells, BoardLaneCell{Value: boardColumn.Value, Rows: append([]int{}, build.cells[column]...)})
		}
		lanes = append(lanes, build.lane)
	}
	return lanes
}

func laneLabel(value laneValue, capability FieldCapability) string {
	if value.value == "" {
		return "No " + strings.ToLower(firstNonEmpty(capability.Label, labelForField(capability.Key)))
	}
	return groupLabel(capability.Key, value.value, value.label, capability, nil)
}

// laneTargetStatus is the status of a relation lane's target.
func laneTargetStatus(row TableRow, capability FieldCapability, value laneValue) *TableRelationStatus {
	if value.value == "" || normalizedValueKind(capability.ValueKind) != "relation" {
		return nil
	}
	for raw, link := range rowRelationValues(row, capability.Key, capability) {
		if link.Ref == nil || link.Status == nil {
			continue
		}
		if identity, _, _ := relationGroupIdentity(raw, map[string]TableRelationValue{raw: link}); identity == value.identity {
			return link.Status
		}
	}
	return nil
}

// hydrateRelationStatuses sets the lifecycle status of linked records on page
// rows' relation values, for up to eight values per field and row, from one
// summary hydration.
func hydrateRelationStatuses(ctx context.Context, scope *noderead.Scope, schema *ontology.Schema, rows []TableRow) ([]TableRow, error) {
	if scope == nil || schema == nil {
		return rows, nil
	}
	seen := map[string]bool{}
	var refs []ontology.NodeRef
	for _, row := range rows {
		for _, values := range row.RelationValues {
			for _, value := range values[:min(len(values), reverseValuesPerRow)] {
				if value.Ref != nil && !seen[noderead.RefIdentityKey(*value.Ref)] {
					seen[noderead.RefIdentityKey(*value.Ref)] = true
					refs = append(refs, *value.Ref)
				}
			}
		}
	}
	if len(refs) == 0 {
		return rows, nil
	}
	records, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return rows, err
	}
	statuses := make(map[string]*TableRelationStatus, len(records))
	for _, record := range records {
		if status := recordStatus(schema, record); status != nil {
			statuses[noderead.RefIdentityKey(record.Ref)] = status
		}
	}
	out := slices.Clone(rows)
	for i := range out {
		if len(out[i].RelationValues) == 0 {
			continue
		}
		next := make(map[string][]TableRelationValue, len(out[i].RelationValues))
		for field, values := range out[i].RelationValues {
			values = slices.Clone(values)
			for j := range values[:min(len(values), reverseValuesPerRow)] {
				if values[j].Ref != nil {
					values[j].Status = statuses[noderead.RefIdentityKey(*values[j].Ref)]
				}
			}
			next[field] = values
		}
		out[i].RelationValues = next
	}
	return out, nil
}

// recordStatus returns a record's lifecycle value from its type's profile,
// or nil when the type has no lifecycle or the record holds no value.
func recordStatus(schema *ontology.Schema, record noderead.NodeRecord) *TableRelationStatus {
	noteType := schema.Types[record.TypeName]
	profile, ok := subjectProfile(schema, record.TypeName)
	if noteType == nil || !ok || profile.LifecycleField == "" {
		return nil
	}
	fields := map[string]any{}
	addSemanticFields(fields, noteType, record)
	value := valueString(fields[profile.LifecycleField])
	if value == "" {
		for _, row := range record.FieldValues[strings.ToLower(profile.LifecycleField)] {
			value = row.ValueText
		}
	}
	enumType := lifecycleEnum(schema, noteType.Fields, profile)
	if enumType == nil || value == "" {
		return nil
	}
	tones, stages := enumType.Tones(), enumValueStages(enumType)
	for index, enumValue := range enumType.Values {
		if enumValue != nil && enumValue.Name == value {
			return &TableRelationStatus{Value: value, Label: firstNonEmpty(enumValue.View.Label, value), Tone: tones[index], Stage: stages[value]}
		}
	}
	return nil
}
