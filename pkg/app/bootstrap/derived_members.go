package bootstrap

import (
	"context"
	"log/slog"
	"slices"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func memberPaths(members []codeanchor.DerivedWork) []string {
	paths := make([]string, 0, len(members))
	for _, member := range members {
		paths = append(paths, member.Path)
	}
	return paths
}

func consumedPaths(inputs map[string][]string) []string {
	var paths []string
	for _, sources := range inputs {
		paths = append(paths, sources...)
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func independentMembers(members []codeanchor.DerivedWork, inputs map[string][]string, changed []string) []codeanchor.DerivedWork {
	var live []codeanchor.DerivedWork
	for _, member := range members {
		if !slices.ContainsFunc(inputs[member.Path], func(path string) bool { return slices.Contains(changed, path) }) {
			live = append(live, member)
		}
	}
	return live
}

// captureWitness runs in the same lease as planning and records every consumed
// path, including RECORD hosts whose own tickets are outside this group.
func (s *derivedScheduler) captureWitness(ctx context.Context, members []codeanchor.DerivedWork, inputs map[string][]string, changed []string) ([]codeanchor.DerivedWork, error) {
	live := independentMembers(members, inputs, changed)
	fingerprints, err := s.watcher.intelStore.DerivedSourceFingerprints(ctx, members[0].Kind, consumedPaths(inputs))
	if err != nil {
		return nil, err
	}
	s.witness = make(map[string]map[string]string, len(live))
	for _, member := range live {
		witness := map[string]string{}
		for _, path := range inputs[member.Path] {
			witness[path] = fingerprints[path]
		}
		s.witness[member.Path] = witness
	}
	return live, nil
}

func (s *derivedScheduler) currentMembers(ctx context.Context, members []codeanchor.DerivedWork) ([]codeanchor.DerivedWork, error) {
	w := s.watcher
	inputs := map[string][]string{}
	var changed []string
	for _, member := range members {
		current, err := w.intelStore.CurrentDerivedWork(ctx, member)
		if err != nil {
			return nil, err
		}
		if !current {
			changed = append(changed, member.Path)
		}
		inputs[member.Path] = []string{member.Path}
		if s.witness != nil {
			inputs[member.Path] = nil
			for path := range s.witness[member.Path] {
				inputs[member.Path] = append(inputs[member.Path], path)
			}
			if _, ok := s.witness[member.Path]; !ok {
				changed = append(changed, member.Path)
				inputs[member.Path] = []string{member.Path}
			}
		}
	}
	if s.witness != nil {
		paths := consumedPaths(inputs)
		fingerprints, err := w.intelStore.DerivedSourceFingerprints(ctx, members[0].Kind, paths)
		if err != nil {
			return nil, err
		}
		for _, witness := range s.witness {
			for path, fingerprint := range witness {
				if fingerprints[path] != fingerprint {
					changed = append(changed, path)
				}
			}
		}
		// Global witnesses cover inventory changes; exact paths also reject disk
		// edits whose structural publication has not arrived yet.
		paths = slices.DeleteFunc(paths, func(path string) bool { return path == "" })
		var ahead []string
		if members[0].Kind == codeanchor.DerivedCode {
			_, ahead, err = w.publishedCodeLoader(ctx, paths)
		} else if members[0].Kind != codeanchor.DerivedGraph {
			_, ahead, err = w.publishedNoteReader(ctx, paths)
		}
		if err != nil {
			return nil, err
		}
		changed = append(changed, ahead...)
	}
	return independentMembers(members, inputs, changed), nil
}

func (s *derivedScheduler) withLease(parent context.Context, members []codeanchor.DerivedWork, ack bool, phase string, run func(context.Context, []codeanchor.DerivedWork) ([]codeanchor.DerivedWork, error)) ([]codeanchor.DerivedWork, error) {
	if len(members) == 0 {
		return nil, errDerivedSuperseded // publication held back every member's result
	}
	var live []codeanchor.DerivedWork
	err := s.inLane(parent, phase, func(ctx context.Context) error {
		var err error
		live, err = s.currentMembers(ctx, members)
		if err != nil {
			return err
		}
		if len(live) > 0 && run != nil {
			live, err = run(ctx, live)
		}
		var dropped []codeanchor.DerivedWork
		for _, member := range members {
			if !slices.Contains(live, member) {
				dropped = append(dropped, member)
			}
		}
		if len(dropped) > 0 {
			if delayErr := s.watcher.intelStore.RetryDerivedWorkBatch(ctx, dropped, time.Now().Add(time.Second), false); delayErr != nil {
				return delayErr
			}
		}
		s.members = live
		if err != nil {
			return err
		}
		if len(live) == 0 {
			return errDerivedSuperseded
		}
		if !ack {
			return nil
		}
		matched, err := s.watcher.intelStore.AckDerivedWorkMatched(ctx, live)
		if err != nil {
			return err
		}
		indexingperf.AddCount(parent, indexingperf.DerivedAcknowledged, int64(len(matched)))
		if len(matched) == 0 {
			return nil
		}
		watcherEvent(ctx, slog.LevelInfo, "derived.acknowledged", append(derivedAttrs(matched[0]), slog.Int("tickets", len(matched)))...)
		if s.watcher.publishEvent != nil {
			data := map[string]any{"source": "vault-runtime", "domains": []string{string(matched[0].Kind)}, "count": len(matched)}
			if matched[0].Path != "" {
				data["paths"] = memberPaths(matched)
			}
			s.watcher.publishEvent(appruntime.GlobalEventIndexChanged, data)
		}
		return nil
	})
	return live, err
}
