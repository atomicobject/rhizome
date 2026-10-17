package store

import (
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

func scanOntologyNodeFieldValueRows(rows *sql.Rows) ([]readmodel.FieldValueRow, error) {
	var out []readmodel.FieldValueRow
	for rows.Next() {
		var row readmodel.FieldValueRow
		var valueBool sql.NullBool
		var valueInt sql.NullInt64
		var valueReal sql.NullFloat64
		var valueDate sql.NullString
		var valueDateTime sql.NullString
		if err := rows.Scan(
			&row.NodeID,
			&row.NotePath,
			&row.TypeName,
			&row.FieldName,
			&row.FieldKind,
			&row.SourceKind,
			&row.ValueKind,
			&row.ValueText,
			&row.ValueNorm,
			&valueBool,
			&valueInt,
			&valueReal,
			&valueDate,
			&valueDateTime,
			&row.TargetNodeID,
			&row.TargetRefJSON,
			&row.TargetNotePath,
			&row.TargetTypeName,
			&row.TargetSourceLocator,
			&row.ListOrdinal,
			&row.SchemaHash,
			&row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if valueBool.Valid {
			row.ValueBool = &valueBool.Bool
		}
		if valueInt.Valid {
			row.ValueInt = &valueInt.Int64
		}
		if valueReal.Valid {
			row.ValueReal = &valueReal.Float64
		}
		if valueDate.Valid {
			row.ValueDate = &valueDate.String
		}
		if valueDateTime.Valid {
			row.ValueDateTime = &valueDateTime.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
