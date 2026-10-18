package search

// HasEvidence uses the same type normalization as evidence scoring.
func HasEvidence(evidence []Evidence, types ...string) bool {
	for _, item := range evidence {
		key := normalizeEvidenceKey(item.Type)
		if key == "" {
			continue
		}
		for _, typ := range types {
			if key == normalizeEvidenceKey(typ) {
				return true
			}
		}
	}
	return false
}
