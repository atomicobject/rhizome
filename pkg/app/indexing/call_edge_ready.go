package indexing

import (
	"reflect"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

type callEdgeReadyIndex struct {
	pendingSymbols   map[string]struct{}
	pendingModules   map[string]struct{}
	pendingFallbacks map[string][]func(string) bool

	deferredResidual codeanchor.CallEdgeResidual

	readySet   map[string]struct{}
	readyQueue []string

	autoQueuedGeneration map[string]uint64
	generation           uint64
}

func newCallEdgeReadyIndex() callEdgeReadyIndex {
	return callEdgeReadyIndex{
		pendingSymbols:       make(map[string]struct{}),
		pendingModules:       make(map[string]struct{}),
		pendingFallbacks:     make(map[string][]func(string) bool),
		readySet:             make(map[string]struct{}),
		autoQueuedGeneration: make(map[string]uint64),
	}
}

func (b *callEdgeReadyIndex) hasDeferred() bool {
	if b == nil {
		return false
	}
	return b.deferredResidual.HasPending()
}

func (b *callEdgeReadyIndex) residual() codeanchor.CallEdgeResidual {
	if b == nil {
		return codeanchor.CallEdgeResidual{}
	}
	return b.deferredResidual
}

func (b *callEdgeReadyIndex) clearResidual() {
	if b == nil {
		return
	}
	b.deferredResidual = codeanchor.CallEdgeResidual{}
	clear(b.pendingSymbols)
	clear(b.pendingModules)
	clear(b.pendingFallbacks)
}

func (b *callEdgeReadyIndex) seedResidual(residual codeanchor.CallEdgeResidual) {
	if b == nil || !residual.HasPending() {
		return
	}
	merged := codeanchor.MergeCallEdgeResidual(b.deferredResidual, residual)
	if reflect.DeepEqual(b.deferredResidual.SymbolKeys(), merged.SymbolKeys()) &&
		reflect.DeepEqual(b.deferredResidual.ModuleKeys(), merged.ModuleKeys()) &&
		equalFallbackKeys(b.deferredResidual.Fallbacks, merged.Fallbacks) {
		b.deferredResidual = merged
		return
	}
	b.deferredResidual = merged
	b.generation++
	clear(b.pendingSymbols)
	clear(b.pendingModules)
	clear(b.pendingFallbacks)
	for _, key := range b.deferredResidual.SymbolKeys() {
		b.pendingSymbols[key] = struct{}{}
	}
	for _, module := range b.deferredResidual.ModuleKeys() {
		b.pendingModules[module] = struct{}{}
	}
	for key, filters := range b.deferredResidual.FallbackFiltersBySymbolKey() {
		b.pendingFallbacks[key] = append([]func(string) bool(nil), filters...)
	}
}

func equalFallbackKeys(a, b []codeanchor.ReverseIndexFallback) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(values []codeanchor.ReverseIndexFallback) []string {
		residual := codeanchor.CallEdgeResidual{Fallbacks: values}
		out := make([]string, 0, len(residual.FallbackFiltersBySymbolKey()))
		for key := range residual.FallbackFiltersBySymbolKey() {
			out = append(out, key)
		}
		return normalizeProgressPaths(out)
	}
	return reflect.DeepEqual(key(a), key(b))
}

func (b *callEdgeReadyIndex) enqueueDirect(paths []string) {
	if b == nil {
		return
	}
	for _, path := range normalizeProgressPaths(paths) {
		b.enqueuePath(path, true)
	}
}

func (b *callEdgeReadyIndex) enqueuePlannerReady(paths []string) {
	if b == nil {
		return
	}
	for _, path := range normalizeProgressPaths(paths) {
		if b.generation > 0 && b.autoQueuedGeneration[path] == b.generation {
			continue
		}
		b.enqueuePath(path, true)
	}
}

func (b *callEdgeReadyIndex) noteDurableFootprints(footprints []codeanchor.DurableRefFootprint) {
	if b == nil || (len(b.pendingSymbols) == 0 && len(b.pendingModules) == 0 && len(b.pendingFallbacks) == 0) {
		return
	}
	for _, footprint := range footprints {
		path := strings.TrimSpace(strings.ReplaceAll(footprint.Path, "\\", "/"))
		if path == "" {
			continue
		}
		if b.autoQueuedGeneration[path] == b.generation {
			continue
		}
		if !b.matchesFootprint(path, footprint) {
			continue
		}
		b.enqueuePath(path, true)
	}
}

func (b *callEdgeReadyIndex) drainReady() []string {
	if b == nil || len(b.readyQueue) == 0 {
		return nil
	}
	out := append([]string(nil), b.readyQueue...)
	b.readyQueue = nil
	clear(b.readySet)
	return out
}

func (b *callEdgeReadyIndex) markRebuilt(paths []string) {
	if b == nil || b.generation == 0 {
		return
	}
	for _, path := range normalizeProgressPaths(paths) {
		b.autoQueuedGeneration[path] = b.generation
	}
}

func (b *callEdgeReadyIndex) enqueuePath(path string, markGeneration bool) {
	if path == "" {
		return
	}
	if _, ok := b.readySet[path]; ok {
		return
	}
	b.readySet[path] = struct{}{}
	b.readyQueue = append(b.readyQueue, path)
	if markGeneration && b.generation > 0 {
		b.autoQueuedGeneration[path] = b.generation
	}
}

func (b *callEdgeReadyIndex) matchesFootprint(path string, footprint codeanchor.DurableRefFootprint) bool {
	for _, key := range footprint.SymbolKeys {
		if _, ok := b.pendingSymbols[key]; ok {
			return true
		}
		if filters, ok := b.pendingFallbacks[key]; ok && fallbackPathAllowed(path, filters) {
			return true
		}
	}
	for _, module := range footprint.Modules {
		if _, ok := b.pendingModules[module]; ok {
			return true
		}
	}
	return false
}

func fallbackPathAllowed(path string, filters []func(string) bool) bool {
	if len(filters) == 0 {
		return true
	}
	for _, filter := range filters {
		if filter == nil || filter(path) {
			return true
		}
	}
	return false
}
