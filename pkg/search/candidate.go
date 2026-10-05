package search

// ensureCandidateKey populates a stable, cached key for the candidate handle.
func ensureCandidateKey(c Candidate) (Candidate, string) {
	if c.handleKey == "" {
		c.handleKey = c.Handle.String()
	}
	return c, c.handleKey
}
