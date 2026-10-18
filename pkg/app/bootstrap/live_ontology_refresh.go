package bootstrap

// RequestOntologyRefresh asks the existing runtime writer to repair an
// uncertified read model. The read caller never acquires writer authority.
func (rt *LiveRuntime) RequestOntologyRefresh() bool {
	watcher := rt.liveWatcher.Load()
	if watcher == nil || watcher.lane == nil {
		return false
	}
	watcher.retainWatchInputs(nil, true, map[string]int64{"ontology_readiness": 1})
	return true
}
