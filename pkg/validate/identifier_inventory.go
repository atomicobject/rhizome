package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
)

type identifierRuntimeNode struct {
	row            codeanchor.IntelOntologyNode
	preferred      *ontology.Field
	aliasesField   string
	aliasesFields  []string
	aliasesByField map[string][]string
	preferredValue string
	aliases        []string
	pool           *identifierreconcile.PoolKey
}

type identifierTypeMetadata struct {
	preferred     *ontology.Field
	aliasesField  string
	aliasesFields []string
	pool          *identifierreconcile.PoolKey
}

type identifierRuntimePool struct {
	key                identifierreconcile.PoolKey
	targetFormat       ontology.IdentifierFormat
	members            []identifierreconcile.MigrationMember
	targetReservations []ontology.IdentifierReservation
	blockers           []identifierMigrationBlocker
}

type identifierMigrationBlocker struct {
	node    identifierreconcile.CanonicalNodeKey
	value   string
	reason  string
	always  bool
	format  ontology.IdentifierFormat
	poolKey identifierreconcile.PoolKey
}

type identifierRuntimeInventory struct {
	claims           []identifierreconcile.Claim
	manualClaims     []identifierreconcile.Claim
	invalidPreferred []identifierRuntimeInvalidPreferred
	nodesByKey       map[string]identifierRuntimeNode
	preferredByNode  map[string]string
	poolsByKey       map[identifierreconcile.PoolKey]*identifierRuntimePool
	notePaths        []string
}

type identifierRuntimeInvalidPreferred struct {
	node  identifierreconcile.CanonicalNodeKey
	value string
}

func buildIdentifierRuntimeInventory(ctx context.Context, runtime *ontology.Runtime) (identifierRuntimeInventory, error) {
	if runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		return identifierRuntimeInventory{}, fmt.Errorf("prepared ontology runtime is incomplete")
	}
	typeNames := make([]string, 0, len(runtime.Schema.Types))
	for name := range runtime.Schema.Types {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)

	metadataByType := make(map[string]identifierTypeMetadata)
	for _, typeName := range typeNames {
		noteType := runtime.Schema.Types[typeName]
		preferred := preferredIdentifierField(noteType)
		if preferred == nil {
			continue
		}
		aliasesFields := identifierAliasesFields(noteType)
		aliasesField := aliasesFields[0]
		var pool *identifierreconcile.PoolKey
		if preferred.IdentifierFormat != nil && !preferred.IsDerivableIdentifier {
			value, err := identifierreconcile.NewPoolKey(preferred.IdentifierFormat)
			if err != nil {
				return identifierRuntimeInventory{}, fmt.Errorf("identifier pool for %s.%s: %w", typeName, preferred.Name, err)
			}
			pool = &value
		}
		metadataByType[typeName] = identifierTypeMetadata{preferred: preferred, aliasesField: aliasesField, aliasesFields: aliasesFields, pool: pool}
	}
	rows, err := runtime.Store.AllOntologyNodes(ctx)
	if err != nil {
		return identifierRuntimeInventory{}, err
	}
	catalogPathsByName := make(map[string]struct{})
	var nodes []identifierRuntimeNode
	for _, row := range rows {
		if ontology.NodeKind(row.NodeKind) == ontology.NodeKindNote && row.NotePath != "" {
			catalogPathsByName[row.NotePath] = struct{}{}
		}
		metadata, ok := metadataByType[row.TypeName]
		if !ok {
			continue
		}
		nodes = append(nodes, identifierRuntimeNode{row: row, preferred: metadata.preferred, aliasesField: metadata.aliasesField, aliasesFields: metadata.aliasesFields, pool: metadata.pool})
	}
	notePaths, err := runtime.Store.NotePaths(ctx)
	if err != nil {
		return identifierRuntimeInventory{}, err
	}
	for _, path := range notePaths {
		catalogPathsByName[path] = struct{}{}
	}
	notePaths = notePaths[:0]
	for path := range catalogPathsByName {
		notePaths = append(notePaths, path)
	}
	sort.Strings(notePaths)
	typedNotePaths := make(map[string]struct{})
	for _, node := range nodes {
		if ontology.NodeKind(node.row.NodeKind) == ontology.NodeKindNote {
			typedNotePaths[node.row.NotePath] = struct{}{}
		}
	}
	unresolvedPaths := make([]string, 0)
	for _, path := range notePaths {
		if _, typed := typedNotePaths[path]; !typed {
			unresolvedPaths = append(unresolvedPaths, path)
		}
	}
	rawAliases := make(map[string][]string, len(notePaths))
	rawPreferred := make(map[string]map[string][]string, len(notePaths))
	if len(notePaths) > 0 {
		propertyNames := []string{"aliases"}
		for _, metadata := range metadataByType {
			propertyNames = append(propertyNames, metadata.preferred.Name)
			propertyNames = append(propertyNames, metadata.aliasesFields...)
		}
		rows, readErr := runtime.Store.CurrentNotePropertyValues(ctx, notePaths, sortedUnique(propertyNames), semdb.NotePropertySourceFrontmatter)
		if readErr != nil {
			return identifierRuntimeInventory{}, readErr
		}
		for _, row := range rows {
			value := strings.TrimSpace(row.ValueText)
			if value == "" {
				continue
			}
			if strings.EqualFold(row.PropertyName, "aliases") {
				rawAliases[row.NotePath] = append(rawAliases[row.NotePath], value)
			}
			if rawPreferred[row.NotePath] == nil {
				rawPreferred[row.NotePath] = make(map[string][]string)
			}
			rawPreferred[row.NotePath][strings.ToLower(row.PropertyName)] = append(rawPreferred[row.NotePath][strings.ToLower(row.PropertyName)], value)
		}
	}
	recoveryBlockers, err := recoverIdentifierMigrationCandidates(ctx, runtime, rows, metadataByType, unresolvedPaths, rawPreferred, &nodes)
	if err != nil {
		return identifierRuntimeInventory{}, err
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].row.NotePath != nodes[j].row.NotePath {
			return nodes[i].row.NotePath < nodes[j].row.NotePath
		}
		if nodes[i].row.Fragment != nodes[j].row.Fragment {
			return nodes[i].row.Fragment < nodes[j].row.Fragment
		}
		return nodes[i].row.NodeID < nodes[j].row.NodeID
	})

	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.row.NodeID)
	}
	fieldRows, err := runtime.Store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	if err != nil {
		return identifierRuntimeInventory{}, err
	}
	fieldsByNode := make(map[string][]codeanchor.IntelOntologyNodeFieldValue, len(nodes))
	for _, row := range fieldRows {
		fieldsByNode[row.NodeID] = append(fieldsByNode[row.NodeID], row)
	}

	result := identifierRuntimeInventory{
		nodesByKey: make(map[string]identifierRuntimeNode, len(nodes)), preferredByNode: make(map[string]string, len(nodes)), poolsByKey: make(map[identifierreconcile.PoolKey]*identifierRuntimePool), notePaths: notePaths,
	}
	for _, blocker := range recoveryBlockers {
		pool := result.poolsByKey[blocker.poolKey]
		if pool == nil {
			pool = &identifierRuntimePool{key: blocker.poolKey, targetFormat: blocker.format}
			result.poolsByKey[blocker.poolKey] = pool
		}
		pool.blockers = append(pool.blockers, blocker)
	}
	for _, node := range nodes {
		key, keyErr := identifierreconcile.NewCanonicalNodeKey(node.row.NotePath, node.row.Fragment, node.row.TypeName, node.preferred.Name)
		if keyErr != nil {
			return identifierRuntimeInventory{}, keyErr
		}
		var preferredValues, aliases []string
		node.aliasesByField = make(map[string][]string)
		for _, row := range fieldsByNode[node.row.NodeID] {
			value := strings.TrimSpace(row.ValueText)
			if value == "" {
				continue
			}
			switch {
			case strings.EqualFold(row.FieldName, node.preferred.Name):
				preferredValues = append(preferredValues, value)
			default:
				for _, field := range node.aliasesFields {
					if strings.EqualFold(row.FieldName, field) {
						aliases = append(aliases, value)
						node.aliasesByField[field] = append(node.aliasesByField[field], value)
					}
				}
			}
		}
		if ontology.NodeKind(node.row.NodeKind) == ontology.NodeKindNote {
			aliases = append(aliases, rawAliases[node.row.NotePath]...)
			node.aliasesByField["aliases"] = append(node.aliasesByField["aliases"], rawAliases[node.row.NotePath]...)
			preferredValues = append(preferredValues, rawPreferred[node.row.NotePath][strings.ToLower(node.preferred.Name)]...)
		}
		preferredValues = uniqueIdentifierValues(preferredValues)
		aliases = uniqueIdentifierValues(aliases)
		node.aliases = append([]string(nil), aliases...)
		if len(preferredValues) > 0 {
			node.preferredValue = preferredValues[0]
			result.preferredByNode[key.String()] = preferredValues[0]
		}
		result.nodesByKey[key.String()] = node
		if node.pool != nil {
			pool := result.poolsByKey[*node.pool]
			if pool == nil {
				pool = &identifierRuntimePool{key: *node.pool, targetFormat: *node.preferred.IdentifierFormat}
				result.poolsByKey[*node.pool] = pool
			}
			for _, value := range preferredValues {
				pool.members = append(pool.members, identifierreconcile.MigrationMember{
					Node: key, PreferredValue: value, ProspectivePath: key.NotePath,
				})
			}
			if len(preferredValues) != 1 {
				pool.blockers = append(pool.blockers, identifierMigrationBlocker{
					node: key, value: strings.Join(preferredValues, ", "),
					reason: fmt.Sprintf("expected exactly one preferred identifier, found %d", len(preferredValues)),
					format: pool.targetFormat, poolKey: pool.key,
				})
			}
			for _, alias := range aliases {
				retiringMirror := false
				for _, preferredValue := range preferredValues {
					if identifierreconcile.IdentifierComparisonKey(alias) == identifierreconcile.IdentifierComparisonKey(preferredValue) {
						retiringMirror = true
						break
					}
				}
				if !retiringMirror {
					pool.targetReservations = append(pool.targetReservations, ontology.IdentifierReservation{Value: alias, Owner: key.String()})
				}
			}
		}
		for _, value := range preferredValues {
			if !identifierRuntimeValueMatchesStrategy(node, value) {
				result.invalidPreferred = append(result.invalidPreferred, identifierRuntimeInvalidPreferred{node: key, value: value})
				continue
			}
			appendIdentifierRuntimeClaim(&result, key, node.pool, value, identifierreconcile.ClaimPreferred)
		}
		for _, value := range aliases {
			appendIdentifierRuntimeClaim(&result, key, node.pool, value, identifierreconcile.ClaimAlias)
		}
	}
	return result, nil
}

func recoverIdentifierMigrationCandidates(
	ctx context.Context,
	runtime *ontology.Runtime,
	rows []codeanchor.IntelOntologyNode,
	metadataByType map[string]identifierTypeMetadata,
	notePaths []string,
	rawPreferred map[string]map[string][]string,
	nodes *[]identifierRuntimeNode,
) ([]identifierMigrationBlocker, error) {
	if len(notePaths) == 0 {
		return nil, nil
	}
	assessments, err := runtime.Store.OntologyAssessmentsByPaths(ctx, notePaths)
	if err != nil {
		return nil, err
	}
	var blockers []identifierMigrationBlocker
	typedPaths := make(map[string]struct{})
	rowsByPath := make(map[string]codeanchor.IntelOntologyNode)
	for _, row := range rows {
		if ontology.NodeKind(row.NodeKind) != ontology.NodeKindNote {
			continue
		}
		rowsByPath[row.NotePath] = row
		if row.TypeName != "_FallbackNote" {
			typedPaths[row.NotePath] = struct{}{}
		}
	}
	for _, notePath := range notePaths {
		if _, typed := typedPaths[notePath]; typed {
			continue
		}
		assessmentRow, ok := assessments[notePath]
		if !ok {
			continue
		}
		assessment, err := ontology.AssessmentFromJSON(assessmentRow.AssessmentJSON)
		if err != nil {
			return nil, err
		}
		var candidates []string
		// Identifier-gated classification keeps undeclared freeform notes under
		// broad typed paths out of the type. Filter that boundary before deciding
		// whether the remaining recovery candidates are ambiguous.
		for _, typeName := range assessment.SelectorCandidateTypes {
			metadata, eligible := metadataByType[typeName]
			if eligible && metadata.pool != nil && len(rawPreferred[notePath][strings.ToLower(metadata.preferred.Name)]) > 0 {
				candidates = append(candidates, typeName)
			}
		}
		if len(candidates) > 1 {
			for _, typeName := range candidates {
				metadata := metadataByType[typeName]
				node, keyErr := identifierreconcile.NewCanonicalNodeKey(notePath, "", typeName, metadata.preferred.Name)
				if keyErr != nil {
					return nil, keyErr
				}
				blockers = append(blockers, identifierMigrationBlocker{
					node: node, reason: "multiple allocatable ontology types match the unresolved note selectors",
					always: true,
					format: *metadata.preferred.IdentifierFormat, poolKey: *metadata.pool,
				})
			}
			continue
		}
		if len(candidates) == 0 {
			continue
		}
		metadata := metadataByType[candidates[0]]
		row := rowsByPath[notePath]
		row.NotePath = notePath
		row.NodeKind = string(ontology.NodeKindNote)
		row.TypeName = candidates[0]
		*nodes = append(*nodes, identifierRuntimeNode{row: row, preferred: metadata.preferred, aliasesField: metadata.aliasesField, aliasesFields: metadata.aliasesFields, pool: metadata.pool})
	}
	return blockers, nil
}

func identifierRuntimeValueMatchesStrategy(node identifierRuntimeNode, value string) bool {
	if node.pool == nil || node.preferred == nil || node.preferred.IdentifierFormat == nil {
		return true
	}
	strategy, err := node.preferred.IdentifierFormat.StrategyContract()
	if err != nil {
		return false
	}
	_, ok := strategy.Parse(value)
	return ok
}

func appendIdentifierRuntimeClaim(result *identifierRuntimeInventory, node identifierreconcile.CanonicalNodeKey, pool *identifierreconcile.PoolKey, value string, kind identifierreconcile.ClaimKind) {
	claim := identifierreconcile.Claim{Node: node, Value: value, Kind: kind}
	if pool == nil {
		result.manualClaims = append(result.manualClaims, claim)
		return
	}
	claim.Pool = *pool
	result.claims = append(result.claims, claim)
}

func preferredIdentifierField(noteType *ontology.NoteType) *ontology.Field {
	if noteType == nil {
		return nil
	}
	for _, field := range noteType.Fields {
		if field != nil && field.IsPreferredIdentifier {
			return field
		}
	}
	return nil
}

func identifierAliasesFields(noteType *ontology.NoteType) []string {
	var fields []string
	if noteType != nil {
		for _, field := range noteType.Fields {
			if field != nil && (strings.EqualFold(field.Name, "aliases") || strings.EqualFold(field.Name, "alias")) {
				fields = append(fields, field.Name)
			}
		}
	}
	if len(fields) == 0 {
		return []string{"aliases"}
	}
	return fields
}

func identifierRewriteAliasFields(node identifierRuntimeNode, value string, aliasOnly bool) (string, []string) {
	fields := append([]string(nil), node.aliasesFields...)
	if ontology.NodeKind(node.row.NodeKind) == ontology.NodeKindNote {
		fields = append(fields, "aliases")
	}
	fields = sortedUnique(fields)
	if aliasOnly {
		owned := fields[:0]
		for _, field := range fields {
			for _, alias := range node.aliasesByField[field] {
				if identifierreconcile.IdentifierComparisonKey(alias) == identifierreconcile.IdentifierComparisonKey(value) {
					owned = append(owned, field)
					break
				}
			}
		}
		fields = owned
	}
	mirror := node.aliasesField
	if ontology.NodeKind(node.row.NodeKind) == ontology.NodeKindNote {
		mirror = "aliases"
	}
	if aliasOnly && len(fields) > 0 {
		mirror = fields[0]
	}
	var additional []string
	for _, field := range fields {
		if field != mirror {
			additional = append(additional, field)
		}
	}
	return mirror, additional
}

func uniqueIdentifierValues(values []string) []string {
	byKey := make(map[string]string, len(values))
	for _, raw := range values {
		value := identifierreconcile.NormalizeIdentifierValue(raw)
		if value == "" {
			continue
		}
		key := identifierreconcile.IdentifierComparisonKey(value)
		if existing, ok := byKey[key]; !ok || value < existing {
			byKey[key] = value
		}
	}
	out := make([]string, 0, len(byKey))
	for _, value := range byKey {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := identifierreconcile.IdentifierComparisonKey(out[i]), identifierreconcile.IdentifierComparisonKey(out[j])
		if left != right {
			return left < right
		}
		return out[i] < out[j]
	})
	return out
}
