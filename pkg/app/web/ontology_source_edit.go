package web

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func editBaseDocumentsFromOps(ops []OntologyEditOp) ([]ontology.EditBaseDocument, error) {
	byPath := make(map[string]ontology.EditBaseDocument)
	for _, op := range ops {
		if op.Expected == nil || op.Expected.SourceHash == "" {
			continue
		}
		notePath := strings.TrimSpace(strings.SplitN(op.Path, "#", 2)[0])
		snapshot, err := buildMarkdownDocumentSnapshotCompat(notePath, op.Expected.SourceContent, time.Time{})
		if err != nil {
			return nil, fmt.Errorf("verify edit source %s: %w", notePath, err)
		}
		if snapshot.ContentFingerprint != op.Expected.SourceHash {
			return nil, fmt.Errorf("edit source fingerprint mismatch for %s", notePath)
		}
		if existing, ok := byPath[notePath]; ok && existing.Fingerprint != op.Expected.SourceHash {
			return nil, fmt.Errorf("multiple source revisions supplied for %s", notePath)
		}
		byPath[notePath] = ontology.EditBaseDocument{
			NotePath: notePath, Fingerprint: op.Expected.SourceHash, Content: op.Expected.SourceContent,
		}
	}
	out := make([]ontology.EditBaseDocument, 0, len(byPath))
	for _, document := range byPath {
		out = append(out, document)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NotePath < out[j].NotePath })
	return out, nil
}

func validateBoundedSourceEdit(previous, next string, schemas ...*ontology.Schema) error {
	identityKeys := protectedIdentifierKeys(schemas...)
	previousProtected, err := protectedSourceSignature(previous, identityKeys)
	if err != nil {
		return fmt.Errorf("parse original source: %w", err)
	}
	nextProtected, err := protectedSourceSignature(next, identityKeys)
	if err != nil {
		return fmt.Errorf("parse edited source: %w", err)
	}
	if !reflect.DeepEqual(previousProtected, nextProtected) {
		return fmt.Errorf("source edit changes a heading, block ID, preferred identifier, or link; use the structured editor for that change")
	}
	return nil
}

type sourceGraphSignature struct {
	Sections   []string
	BlockIDs   []string
	Links      []string
	Identities []string
}

var standaloneBlockIDPattern = regexp.MustCompile(`(?m)^\s*\^([A-Za-z0-9-]+)\s*$`)

func protectedSourceSignature(source string, identityKeys map[string]struct{}) (sourceGraphSignature, error) {
	snapshot, err := buildMarkdownDocumentSnapshotCompat("source.md", source, time.Time{})
	if err != nil {
		return sourceGraphSignature{}, err
	}
	signature := sourceGraphSignature{}
	for _, match := range standaloneBlockIDPattern.FindAllStringSubmatch(source, -1) {
		signature.BlockIDs = append(signature.BlockIDs, "source\x00"+match[1])
	}
	var walk func([]*ontology.SectionNode)
	walk = func(sections []*ontology.SectionNode) {
		for _, section := range sections {
			if section == nil {
				continue
			}
			signature.Sections = append(signature.Sections, string(section.Level)+"\x00"+section.Title+"\x00"+section.BlockID)
			walk(section.Children)
		}
	}
	walk(snapshot.Sections)
	var walkSourceSpans func([]*ontology.MarkdownSourceSpan, string)
	walkSourceSpans = func(spans []*ontology.MarkdownSourceSpan, parent string) {
		for index, span := range spans {
			if span == nil {
				continue
			}
			owner := fmt.Sprintf("%s/%d:%s", parent, index, span.Shape)
			signature.BlockIDs = append(signature.BlockIDs, owner+"\x00"+span.BlockID+"\x00"+span.MalformedBlockID)
			walkSourceSpans(span.Children, owner)
		}
	}
	walkSourceSpans(snapshot.SourceSpans, "item")
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	inFence := false
	inFrontmatter := len(lines) > 0 && strings.TrimSpace(lines[0]) == "---"
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inFrontmatter {
			if index > 0 && trimmed == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || index == 0 || strings.TrimSpace(lines[index-1]) == "" {
			continue
		}
		if allMarkdownHeadingUnderline(trimmed, '=') {
			signature.Sections = append(signature.Sections, "setext-1\x00"+strings.TrimSpace(lines[index-1]))
		} else if allMarkdownHeadingUnderline(trimmed, '-') {
			signature.Sections = append(signature.Sections, "setext-2\x00"+strings.TrimSpace(lines[index-1]))
		}
	}
	for _, link := range scanStructuredMarkdownLinksCompat(source) {
		signature.Links = append(signature.Links, fmt.Sprintf("%s\x00%t\x00%s\x00%s\x00%s", link.Kind, link.Embed, link.Target, link.Path, link.Fragment))
	}
	for key, value := range snapshot.Frontmatter {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if _, protected := identityKeys[normalizedKey]; protected {
			encoded, err := json.Marshal(value)
			if err != nil {
				return sourceGraphSignature{}, err
			}
			signature.Identities = append(signature.Identities, "frontmatter:"+normalizedKey+"\x00"+string(encoded))
		}
	}
	topLevelEnd := len(snapshot.Content)
	if len(snapshot.Sections) > 0 && snapshot.Sections[0] != nil {
		topLevelEnd = snapshot.Sections[0].StartByte
	}
	topLevelStart := snapshot.FrontmatterRange.End
	if topLevelStart < 0 || topLevelStart > topLevelEnd {
		topLevelStart = 0
	}
	appendProtectedInlineIdentities(&signature.Identities, "note", snapshot.Content[topLevelStart:topLevelEnd], identityKeys)
	var appendSectionIdentities func([]*ontology.SectionNode, string)
	appendSectionIdentities = func(sections []*ontology.SectionNode, parent string) {
		for index, section := range sections {
			if section == nil {
				continue
			}
			owner := fmt.Sprintf("%s/%d:%s:%s", parent, index, section.Level, section.Title)
			appendProtectedInlineIdentities(&signature.Identities, owner, ontology.SectionOwnContent(section), identityKeys)
			appendSectionIdentities(section.Children, owner)
		}
	}
	appendSectionIdentities(snapshot.Sections, "section")
	sort.Strings(signature.Identities)
	return signature, nil
}

func appendProtectedInlineIdentities(out *[]string, owner, content string, identityKeys map[string]struct{}) {
	for key, values := range ontology.SectionInlineProperties(content) {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if _, protected := identityKeys[normalizedKey]; !protected {
			continue
		}
		for _, value := range values {
			*out = append(*out, "inline:"+owner+"\x00"+normalizedKey+"\x00"+strings.TrimSpace(value))
		}
	}
}

func protectedIdentifierKeys(schemas ...*ontology.Schema) map[string]struct{} {
	keys := map[string]struct{}{
		"type": {}, "id": {}, "identifier": {}, "aliases": {}, "alias": {}, "slug": {},
	}
	for _, schema := range schemas {
		if schema == nil {
			continue
		}
		for _, noteType := range schema.Types {
			if noteType == nil {
				continue
			}
			for _, field := range noteType.Fields {
				if field == nil || !field.IsIdentifier {
					continue
				}
				keys[strings.ToLower(strings.TrimSpace(field.Name))] = struct{}{}
				keys[strings.ToLower(strings.TrimSpace(field.Source))] = struct{}{}
				for _, alias := range field.SourceAliases {
					keys[strings.ToLower(strings.TrimSpace(alias))] = struct{}{}
				}
			}
		}
	}
	delete(keys, "")
	return keys
}

func allMarkdownHeadingUnderline(value string, marker byte) bool {
	if len(value) < 3 {
		return false
	}
	for index := range len(value) {
		if value[index] != marker {
			return false
		}
	}
	return true
}
