package markdownsyntax

func SplitTextWithOverlap(body string, maxChars, overlap int) []string {
	if maxChars <= 0 {
		return []string{body}
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= maxChars {
		overlap = maxChars / 5
	}
	r := []rune(body)
	if len(r) <= maxChars {
		return []string{body}
	}
	var parts []string
	start := 0
	for start < len(r) {
		end := start + maxChars
		if end > len(r) {
			end = len(r)
		}
		parts = append(parts, string(r[start:end]))
		if end == len(r) {
			break
		}
		start = end - overlap
		if start < 0 {
			start = 0
		}
	}
	return parts
}
