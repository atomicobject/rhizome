package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/tasks"
)

// TasksAPI is the part of the scheduled-tasks service the Tasks page uses
// (SPEC-0127 US8). *tasks.Service implements it.
type TasksAPI interface {
	Status(ctx context.Context) (tasks.Report, error)
	LiveState(ctx context.Context) (tasks.LiveState, error)
	Runs(task string) ([]tasks.RunView, error)
	RunDetail(id string, after int64) (tasks.RunDetail, error)
	PrecheckRun(ctx context.Context, itemID string) error
	Snooze(ctx context.Context, itemID string, until time.Time) error
	Skip(ctx context.Context, itemID string) error
	Discard(ctx context.Context, itemID string) error
	Done(ctx context.Context, itemID string) error
	SetMode(ctx context.Context, task, mode string) error
	SetPaused(ctx context.Context, paused bool) error
	RecordLaunchError(ctx context.Context, itemID, reason string) error
}

var (
	taskRunIDPattern  = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)
	taskIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`) // taskdef's ID rule
	taskItemIDPattern = regexp.MustCompile(`^[0-9]{1,18}$`)
)

// launchTaskRun starts `rzm tasks run <item>` detached in the vault, the
// way the runtime starts `rzm tasks run-due`.
func launchTaskRun(vaultPath, itemID string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return appruntime.StartDetached(exe, vaultPath, "tasks", "run", itemID)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if s.cfg.TasksOpener == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("tasks unavailable"))
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tasks"), "/"), "/")
	if parts[0] == "" {
		parts = nil
	}
	get, post := r.Method == http.MethodGet, r.Method == http.MethodPost
	var serve func(TasksAPI)
	switch {
	case len(parts) == 0 && get:
		serve = func(api TasksAPI) { s.serveTasksStatus(w, r, api) }
	case len(parts) == 1 && parts[0] == "state" && get:
		serve = func(api TasksAPI) {
			state, err := api.LiveState(r.Context())
			writeTasksResult(w, state, err)
		}
	case len(parts) == 1 && parts[0] == "runs" && get:
		task := r.URL.Query().Get("task")
		if !taskIDPattern.MatchString(task) {
			writeError(w, http.StatusBadRequest, errors.New("a valid task id is required"))
			return
		}
		serve = func(api TasksAPI) {
			runs, err := api.Runs(task)
			if runs == nil {
				runs = []tasks.RunView{}
			}
			writeTasksResult(w, map[string]any{"runs": runs}, err)
		}
	case len(parts) == 2 && parts[0] == "runs" && get:
		id := parts[1]
		if !taskRunIDPattern.MatchString(id) {
			writeError(w, http.StatusBadRequest, errors.New("invalid run id"))
			return
		}
		after, err := parseTasksAfter(r.URL.Query().Get("after"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		serve = func(api TasksAPI) {
			detail, err := api.RunDetail(id, after)
			if errors.Is(err, tasks.ErrRunNotFound) || errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, fmt.Errorf("run %s not found", id))
				return
			}
			writeTasksResult(w, detail, err)
		}
	case len(parts) == 3 && parts[0] == "items" && post:
		itemID, action := parts[1], parts[2]
		if !taskItemIDPattern.MatchString(itemID) {
			writeError(w, http.StatusBadRequest, errors.New("invalid item id"))
			return
		}
		serve = s.taskItemAction(w, r, itemID, action)
	case len(parts) == 2 && parts[0] == "modes" && post:
		task := parts[1]
		if !taskIDPattern.MatchString(task) {
			writeError(w, http.StatusBadRequest, errors.New("invalid task id"))
			return
		}
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		serve = func(api TasksAPI) { writeTasksAction(w, api.SetMode(r.Context(), task, req.Mode)) }
	case len(parts) == 1 && (parts[0] == "pause" || parts[0] == "resume") && post:
		paused := parts[0] == "pause"
		serve = func(api TasksAPI) { writeTasksAction(w, api.SetPaused(r.Context(), paused)) }
	case get || post:
		writeError(w, http.StatusNotFound, errors.New("unknown tasks route"))
		return
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if serve == nil {
		return
	}
	api, err := s.cfg.TasksOpener(r.Context(), s.cfg.VaultPath)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	serve(api)
}

func (s *Server) serveTasksStatus(w http.ResponseWriter, r *http.Request, api TasksAPI) {
	report, err := api.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// taskItemAction validates an item action's request and returns how to
// serve it, or writes the error and returns nil.
func (s *Server) taskItemAction(w http.ResponseWriter, r *http.Request, itemID, action string) func(TasksAPI) {
	ctx := r.Context()
	switch action {
	case "run":
		return func(api TasksAPI) {
			if err := api.PrecheckRun(ctx, itemID); err != nil {
				writeError(w, http.StatusConflict, err)
				return
			}
			launchedAt := time.Now().UTC()
			launch := s.tasksLaunch
			if launch == nil {
				launch = launchTaskRun
			}
			if err := launch(s.cfg.VaultPath, itemID); err != nil {
				_ = api.RecordLaunchError(ctx, itemID, err.Error())
				writeError(w, http.StatusInternalServerError, fmt.Errorf("start task run: %w", err))
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"launchedAt": launchedAt.Format(time.RFC3339Nano)})
		}
	case "snooze":
		var req struct {
			Until time.Time `json:"until"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Until.IsZero() {
			writeError(w, http.StatusBadRequest, errors.New(`snooze needs {"until": RFC3339}`))
			return nil
		}
		return func(api TasksAPI) { writeTasksAction(w, api.Snooze(ctx, itemID, req.Until)) }
	case "skip":
		return func(api TasksAPI) { writeTasksAction(w, api.Skip(ctx, itemID)) }
	case "discard":
		return func(api TasksAPI) { writeTasksAction(w, api.Discard(ctx, itemID)) }
	case "done":
		return func(api TasksAPI) { writeTasksAction(w, api.Done(ctx, itemID)) }
	}
	writeError(w, http.StatusNotFound, fmt.Errorf("unknown item action %q", action))
	return nil
}

func parseTasksAfter(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	after, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || after < 0 {
		return 0, errors.New("after must be a non-negative integer")
	}
	return after, nil
}

func writeTasksResult(w http.ResponseWriter, payload any, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// writeTasksAction answers an item, mode, or pause action. Its errors are
// the person's to fix (an item in the wrong state, an unknown mode), except
// a run in progress.
func writeTasksAction(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, struct{}{})
	case errors.Is(err, tasks.ErrBusy):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}
