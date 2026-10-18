package ontology

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func ontologyFingerprint(doc *noteDoc, meta semdb.NoteMetadataRow, schemaHash string, incidentRows []semdb.GraphDocEdge) string {
	hasher := sha256.New()
	writeFingerprintPart := func(parts ...string) {
		for _, part := range parts {
			_, _ = hasher.Write([]byte(part))
			_, _ = hasher.Write([]byte{0})
		}
	}
	writeFingerprintPart(schemaHash, doc.Path, meta.ContentHash, doc.TypeName)
	keys := make([]string, 0, len(doc.Frontmatter))
	for key := range doc.Frontmatter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeFingerprintPart("fm", key, canonicalValueString(doc.Frontmatter[key]))
	}
	inlineKeys := make([]string, 0, len(doc.Inline))
	for key := range doc.Inline {
		inlineKeys = append(inlineKeys, key)
	}
	sort.Strings(inlineKeys)
	for _, key := range inlineKeys {
		values := append([]string(nil), doc.Inline[key]...)
		sort.Strings(values)
		for _, value := range values {
			writeFingerprintPart("inline", key, value)
		}
	}
	tags := append([]string(nil), doc.Tags...)
	sort.Strings(tags)
	for _, tag := range tags {
		writeFingerprintPart("tag", tag)
	}
	graphParts := make([]string, 0, len(incidentRows))
	for _, row := range incidentRows {
		graphParts = append(graphParts, strings.Join([]string{row.SrcPath, row.DstPath, row.Kind}, "\x1f"))
	}
	sort.Strings(graphParts)
	for _, part := range graphParts {
		writeFingerprintPart("graph", part)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func canonicalValueString(v interface{}) string {
	switch value := v.(type) {
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, canonicalValueString(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []string:
		parts := append([]string(nil), value...)
		sort.Strings(parts)
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}
