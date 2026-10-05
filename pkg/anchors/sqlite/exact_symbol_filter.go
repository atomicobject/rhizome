package sqlite

import "strings"

func exactSymbolFilterSQL(symbols []string, symbolExpr, fqnExpr string) (string, []any) {
	if len(symbols) == 0 {
		return "", nil
	}
	var clauses []string
	var args []any
	for _, symbol := range symbols {
		if symbol == "" {
			continue
		}
		clauses = append(clauses, "("+symbolExpr+" COLLATE BINARY = ? OR "+fqnExpr+" COLLATE BINARY = ? OR substr("+fqnExpr+", -length(?)) COLLATE BINARY = ?)")
		suffix := "." + symbol
		args = append(args, symbol, symbol, suffix, suffix)
	}
	if len(clauses) == 0 {
		return "0", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}
