package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"sort"
	"strings"
)

type BlockIDValidationIssue struct {
	Code     string
	NotePath string
	Ref      NodeRef
	TypeName string
	Label    string
	BlockID  string
	Message  string
}

func ValidateEmbeddedBlockIDs(snapshot *DocumentSnapshot, schema *ontoschema.Schema) []BlockIDValidationIssue {
	if snapshot == nil || schema == nil {
		return nil
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return nil
	}
	counts := map[string]int{}
	var collect func([]*SectionNode)
	collect = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if id := strings.TrimSpace(node.BlockID); id != "" {
				counts[strings.TrimPrefix(id, "^")]++
			}
			collect(node.Children)
		}
	}
	collect(snapshot.Sections)
	issues := make([]BlockIDValidationIssue, 0)
	var walk func([]*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			ref := resolver.sectionNodeRef(node)
			rawBlockID := strings.TrimSpace(strings.TrimPrefix(node.BlockID, "^"))
			malformed := strings.TrimSpace(strings.TrimPrefix(node.MalformedBlockID, "^"))
			switch ref.Kind {
			case NodeKindEmbedded:
				switch {
				case malformed != "":
					issues = append(issues, BlockIDValidationIssue{
						Code:     "malformed_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  malformed,
						Message:  "embedded ontology node has a malformed block ID",
					})
				case rawBlockID == "":
					projection, err := ProjectBoundNodeFromSnapshot(snapshot, schema, ref)
					expected := ""
					if err == nil {
						expected = GenerateBlockIDForProjection(snapshot, projection)
					}
					issues = append(issues, BlockIDValidationIssue{
						Code:     "missing_embedded_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  expected,
						Message:  "embedded ontology node is missing a durable block ID",
					})
				case !ValidBlockID(rawBlockID):
					issues = append(issues, BlockIDValidationIssue{
						Code:     "malformed_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  rawBlockID,
						Message:  "embedded ontology node has a malformed block ID",
					})
				case counts[rawBlockID] > 1:
					issues = append(issues, BlockIDValidationIssue{
						Code:     "duplicate_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  rawBlockID,
						Message:  "block ID is duplicated within the note",
					})
				}
			case NodeKindSection:
				// WHY: a `^block-id` on a section that the schema does not
				// classify as embedded is "stray" — the surrounding sweep no
				// longer promotes such sections to EMBEDDED so the anchor's
				// presence becomes visible drift instead of silently changing
				// node identity. Coderefs:
				// [[linkable-embedded-node-identifiers#^spec-0023-us5]]
				switch {
				case malformed != "":
					issues = append(issues, BlockIDValidationIssue{
						Code:     "malformed_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  malformed,
						Message:  "structural section has a malformed block ID",
					})
				case rawBlockID != "":
					issues = append(issues, BlockIDValidationIssue{
						Code:     "stray_block_id_on_non_embedded_section",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  rawBlockID,
						Message:  "block ID on a section the schema does not classify as embedded; remove it or change the schema role",
					})
				}
			}
			walk(node.Children)
		}
	}
	walk(snapshot.Sections)
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].NotePath != issues[j].NotePath {
			return issues[i].NotePath < issues[j].NotePath
		}
		return issues[i].Ref.StartByte < issues[j].Ref.StartByte
	})
	return issues
}
