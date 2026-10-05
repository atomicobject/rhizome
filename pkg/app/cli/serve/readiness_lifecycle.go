package serve

// startWorker admits a coordinator-owned callback while holding the same
// mutex Drain uses to close admission. Nested workers may start from Bridge,
// but none can be added after Drain begins waiting.
func (c *ReadinessCoordinator) startWorker(fn func()) {
	if c == nil || fn == nil {
		return
	}
	c.mu.Lock()
	if c.draining {
		c.mu.Unlock()
		return
	}
	c.workers.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.workers.Done()
		fn()
	}()
}

// Drain joins validation and readiness callbacks before their stores or the
// runtime election are released. The caller cancels the serve context first.
func (c *ReadinessCoordinator) Drain() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.draining = true
	c.mu.Unlock()
	c.workers.Wait()
}
