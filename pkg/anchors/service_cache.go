package codeanchor

import "container/list"

func (s *Service) cacheFileContext(path string, ctx FileContext, generation uint64) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if generation != s.cacheGeneration {
		return
	}
	if el, ok := s.fileCtxCache[path]; ok {
		el.Value = struct {
			key string
			val FileContext
		}{path, ctx}
		s.fileCtxList.MoveToFront(el)
		return
	}
	if s.cacheLimit > 0 && s.fileCtxList.Len() >= s.cacheLimit {
		// Evict LRU (tail)
		back := s.fileCtxList.Back()
		if back != nil {
			entry := back.Value.(struct {
				key string
				val FileContext
			})
			delete(s.fileCtxCache, entry.key)
			s.fileCtxList.Remove(back)
		}
	}
	el := s.fileCtxList.PushFront(struct {
		key string
		val FileContext
	}{path, ctx})
	s.fileCtxCache[path] = el
}

func (s *Service) cachedFileCtx(path string) (FileContext, uint64, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if el, ok := s.fileCtxCache[path]; ok {
		s.fileCtxList.MoveToFront(el)
		entry := el.Value.(struct {
			key string
			val FileContext
		})
		return entry.val, s.cacheGeneration, true
	}
	return FileContext{}, s.cacheGeneration, false
}

func (s *Service) invalidateFile(path string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cacheGeneration++
	if el, ok := s.fileCtxCache[path]; ok {
		s.fileCtxList.Remove(el)
		delete(s.fileCtxCache, path)
	}
}

func (s *Service) invalidateAllFiles() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cacheGeneration++
	if len(s.fileCtxCache) == 0 {
		return
	}
	s.fileCtxCache = make(map[string]*list.Element)
	s.fileCtxList = list.New()
}

// InvalidateFile clears cached context for a file.
func (s *Service) InvalidateFile(path string) {
	relPath, err := s.relCodePath(path)
	if err != nil {
		return
	}
	s.invalidateFile(relPath)
}

// CacheStats returns file-context cache hit/miss counters.
func (s *Service) CacheStats() (hits, misses uint64) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return s.cacheHits, s.cacheMisses
}
