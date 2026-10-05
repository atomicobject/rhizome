package views

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

func fingerprintDefinition(def viewconfig.ViewDefinition) string {
	return fingerprint(def)
}

func fingerprintSource(def viewconfig.ViewDefinition, rows []TableRow) string {
	type rowIdentity struct {
		Ref       string `json:"ref"`
		UpdatedAt int64  `json:"updatedAt,omitempty"`
	}
	ids := make([]rowIdentity, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, rowIdentity{Ref: nodeRefKey(row.Ref), UpdatedAt: row.UpdatedAt})
	}
	return fingerprint(map[string]any{"source": def.SourceSpec, "rows": ids})
}

func fingerprintExecution(definition string, source string, state ExecutionState, variant string) string {
	return fingerprint(map[string]any{"definition": definition, "source": source, "state": state, "variant": variant})
}

func fingerprint(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
