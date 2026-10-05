package codeanchor

import "log"

func summaryHasUsefulExtraction(summary FileSummary) bool {
	return len(summary.Symbols) > 0 || len(summary.Calls) > 0 || len(summary.MemberRefs) > 0 || len(summary.Annotations) > 0 || len(summary.Supers) > 0
}

func logParseErrorIfPoor(lang, filePath string, summary FileSummary, detail string) {
	if summaryHasUsefulExtraction(summary) {
		return
	}
	if detail != "" {
		log.Printf("codeanchor: %s parse errors for %s (%s)", lang, filePath, detail)
		return
	}
	log.Printf("codeanchor: %s parse errors for %s", lang, filePath)
}
