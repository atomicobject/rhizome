package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func execValuesBatch(ctx context.Context, tx *sql.Tx, prefix string, values [][]any, valuesPerRow int, suffix string) error {
	if len(values) == 0 {
		return nil
	}
	rowsPerBatch := sqliteValuesBatchMaxParams / valuesPerRow
	if rowsPerBatch < 1 {
		rowsPerBatch = 1
	}
	for start := 0; start < len(values); start += rowsPerBatch {
		end := start + rowsPerBatch
		if end > len(values) {
			end = len(values)
		}
		batch := values[start:end]
		args := make([]any, 0, len(batch)*valuesPerRow)
		var sql strings.Builder
		sql.WriteString(prefix)
		for i, row := range batch {
			if i > 0 {
				sql.WriteByte(',')
			}
			sql.WriteByte('(')
			for j := 0; j < valuesPerRow; j++ {
				if j > 0 {
					sql.WriteByte(',')
				}
				sql.WriteByte('?')
			}
			sql.WriteByte(')')
			args = append(args, row...)
		}
		sql.WriteString(suffix)
		if _, err := tx.ExecContext(ctx, sql.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func selectIDsByValues(ctx context.Context, tx *sql.Tx, tableName, idColumn, valueColumn string, values []string) (map[string]int64, error) {
	values = normalizeNonEmptyStrings(values)
	out := make(map[string]int64, len(values))
	if len(values) == 0 {
		return out, nil
	}
	rowsPerBatch := sqliteValuesBatchMaxParams
	for start := 0; start < len(values); start += rowsPerBatch {
		end := start + rowsPerBatch
		if end > len(values) {
			end = len(values)
		}
		batch := values[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT %s, %s FROM %s WHERE %s IN (%s)`, idColumn, valueColumn, tableName, valueColumn, holders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var value string
			if err := rows.Scan(&id, &value); err != nil {
				rows.Close()
				return nil, err
			}
			out[value] = id
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
