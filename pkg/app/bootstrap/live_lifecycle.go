package bootstrap

// Runtime shutdown. Closers run in reverse registration order, so the election
// lock (registered first, during election) is released last: the manifest, the
// registry entry, and every store are gone before another process can win the
// vault (SPEC-0104).

// Close releases all resources in reverse order of creation.
func (rt *LiveRuntime) Close() error {
	rt.closeOnce.Do(func() {
		if rt.cancelCtx != nil {
			rt.cancelCtx()
		}
		// Initializers and callbacks may finish writes after cancellation.
		// Drain them while their stores and runtime ownership still exist.
		rt.workers.Wait()
		// Stop indexing jobs before the stores they use are closed; the lane's
		// own closer (registered at promotion) then finds it already closed.
		if l := rt.Lane(); l != nil {
			l.Close()
		}
		rt.closeMu.Lock()
		rt.closed = true
		fns := append([]func(){}, rt.closers...)
		rt.closeMu.Unlock()

		// Run in reverse order
		for i := len(fns) - 1; i >= 0; i-- {
			fns[i]()
		}
	})
	return nil
}

func (rt *LiveRuntime) addCloser(fn func()) {
	if fn == nil {
		return
	}
	runNow := false
	rt.closeMu.Lock()
	if rt.closed {
		runNow = true
	} else {
		rt.closers = append(rt.closers, fn)
	}
	rt.closeMu.Unlock()
	if runNow {
		fn()
	}
}

func (rt *LiveRuntime) isClosed() bool {
	if rt == nil {
		return true
	}
	select {
	case <-rt.ctx.Done():
		return true
	default:
		return false
	}
}

// startWorker tracks runtime-owned work. Roots start before construction
// returns; later workers start only from an enrolled worker, keeping the count
// positive while nested work is registered. Workers must not call Close.
func (rt *LiveRuntime) startWorker(fn func()) {
	rt.workers.Add(1)
	go func() {
		defer rt.workers.Done()
		fn()
	}()
}
