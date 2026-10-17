package sourcetxn

// InspectPending returns detached read-only journal metadata. Mutation always
// enters through Open and its discovery, policy and quarantine classification.
func InspectPending(root string) ([]JournalInfo, error) {
	js, err := discoverRepairJournals(writerContext{VaultPath: root})
	if err != nil {
		return nil, err
	}
	out := make([]JournalInfo, len(js))
	for i, j := range js {
		out[i] = info(j)
	}
	return out, nil
}

// PendingEvidence reports pending and quarantined source transactions without
// acquiring publication or recovery authority.
func PendingEvidence(root string) ([]JournalEvidence, error) {
	return detectPendingRepairJournals(writerContext{VaultPath: root})
}

// ResolvePath checks an authored vault-relative source path.
func ResolvePath(root, relative string) (string, error) {
	return repairAbsPath(writerContext{VaultPath: root}, relative)
}

// CompletionTarget checks a caller-supplied receipt target and returns its
// vault-relative and absolute paths. It grants no publication authority.
func CompletionTarget(root, target string) (string, string, error) {
	return repairCompletionTarget(writerContext{VaultPath: root}, target)
}
