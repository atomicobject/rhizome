package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// LocalVersion is the definition in the selected checkout, independent of scheduling.
type LocalVersion struct {
	State string `json:"state"`
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
}

type localTask struct {
	def     Definition
	version LocalVersion
	data    []byte
}

func (s *Service) localTasks(ctx context.Context) (map[string]localTask, error) {
	out := map[string]localTask{}
	if s.opts.Checkout == "" {
		return out, nil
	}
	data, err := gitBytes(ctx, s.opts.Checkout, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for _, path := range strings.Split(string(data), "\x00") {
		if taskdef.IsTaskFile(path) {
			paths[path] = true
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		raw, err := os.ReadFile(filepath.Join(s.opts.Checkout, path))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		identity, _ := ParseDefinition(path, []byte("name: Local task\nevery: 1d\nprompt: Test\n"))
		if identity.ID == "" {
			continue
		}
		local := localTask{data: raw, version: LocalVersion{State: "new", Path: path}}
		if err == nil {
			local.def, err = ParseDefinition(path, raw)
		}
		if err != nil {
			local.version.State, local.version.Error = "invalid", err.Error()
		}
		if prior, ok := out[identity.ID]; ok {
			prior.version.State, prior.version.Error = "invalid", fmt.Sprintf("task id %q is also declared by %s", identity.ID, path)
			out[identity.ID] = prior
		} else {
			out[identity.ID] = local
		}
	}
	return out, nil
}

func (s *Service) addLocalTasks(ctx context.Context, report *Report, defs []Definition) error {
	locals, err := s.localTasks(ctx)
	if err != nil {
		return err
	}
	for i := range report.Tasks {
		ts := &report.Tasks[i]
		local, ok := locals[ts.ID]
		if !ok {
			continue
		}
		if local.version.State != "invalid" {
			local.version.State = "differs"
			for _, def := range defs {
				if def.ID == ts.ID {
					raw, err := s.opts.Repo.ShowFile(ctx, report.Commit, def.Path)
					if err == nil && bytes.Equal(raw, local.data) {
						local.version.State = "same"
					}
					break
				}
			}
		}
		ts.Local = &local.version
		delete(locals, ts.ID)
	}
	ids := make([]string, 0, len(locals))
	for id := range locals {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		local := locals[id]
		name := local.def.Name
		if name == "" {
			name = id
		}
		report.Tasks = append(report.Tasks, TaskStatus{ID: id, Name: name, Scope: local.def.Scope, Every: local.def.Every, CheckOnly: strings.TrimSpace(local.def.Prompt) == "", Local: &local.version})
	}
	return nil
}

// TestSource is a local task admitted for testing in its canonical checkout.
type TestSource struct {
	File     string
	Checkout string
}

// PrecheckTest resolves a discovered task and checks admission without executing it.
func (s *Service) PrecheckTest(ctx context.Context, task string) (TestSource, error) {
	if reason := s.untrusted(); reason != "" {
		return TestSource{}, errors.New(reason)
	}
	locals, err := s.localTasks(ctx)
	if err != nil {
		return TestSource{}, err
	}
	local, ok := locals[task]
	if !ok {
		return TestSource{}, fmt.Errorf("no local task %q in this checkout", task)
	}
	if local.version.Error != "" {
		return TestSource{}, errors.New(local.version.Error)
	}
	file := filepath.Join(s.opts.Checkout, local.version.Path)
	source, _, err := s.testSource(ctx, file)
	if err != nil {
		return TestSource{}, err
	}
	release, err := s.lockRun(ctx, 0)
	if errors.Is(err, errLocked) {
		return TestSource{}, ErrBusy
	}
	if err != nil {
		return TestSource{}, err
	}
	return TestSource{File: file, Checkout: source}, release()
}

// RecordTestLaunchError makes a failed detached launch visible in test history.
func (s *Service) RecordTestLaunchError(ctx context.Context, task, reason string) error {
	a := s.newAttempt(true)
	return s.store.Update(ctx, func(st *State) error {
		r := a.record(task, OutcomeFailed, s.opts.Now())
		r.Message = reason
		st.Record(r)
		return nil
	})
}
