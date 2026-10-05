// Package harnesscheck implements the headless harness status and smoke
// workflows used by the root CLI diagnostics.
package harnesscheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

type Options struct {
	Model  string
	Effort string
	Cwd    string
	Prompt string
}

type Summary struct {
	Status   harness.Status  `json:"status"`
	Generate json.RawMessage `json:"generate"`
	Turn     Turn            `json:"turn"`
}

type Turn struct {
	Events int                `json:"events"`
	Text   string             `json:"text"`
	Usage  harness.TokenUsage `json:"usage"`
}

var smokeSchema = json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`)

func Select(drivers map[harness.Kind]harness.Harness, requested string) (harness.Harness, error) {
	kind := harness.Kind(strings.TrimSpace(requested))
	driver := drivers[kind]
	if driver == nil {
		return nil, fmt.Errorf("harness %q is not registered", requested)
	}
	return driver, nil
}

// Status returns either one selected status or the stable, sorted status list.
func Status(ctx context.Context, drivers map[harness.Kind]harness.Harness, requested string) (any, error) {
	if requested != "" {
		driver, err := Select(drivers, requested)
		if err != nil {
			return nil, err
		}
		status, err := driver.Status(ctx)
		if err != nil {
			return nil, withPhase(harness.ErrInitialize, err)
		}
		return status, nil
	}

	kinds := make([]string, 0, len(drivers))
	for kind := range drivers {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	statuses := make([]harness.Status, 0, len(kinds))
	for _, name := range kinds {
		status, err := drivers[harness.Kind(name)].Status(ctx)
		if err != nil {
			return nil, withPhase(harness.ErrInitialize, err)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// Smoke runs one generation and one streamed turn. Every event is passed to
// writeEvent before the final summary is returned.
func Smoke(ctx context.Context, driver harness.Harness, options Options, writeEvent func(harness.Event) error) (Summary, error) {
	status, err := driver.Status(ctx)
	if err != nil {
		return Summary{}, withPhase(harness.ErrInitialize, err)
	}
	if !status.Installed {
		return Summary{}, statusPhase(harness.ErrNotInstalled, status)
	}
	if !status.LoggedIn {
		return Summary{}, statusPhase(harness.ErrNotLoggedIn, status)
	}
	if status.LastError != "" {
		return Summary{}, harness.Phase(harness.ErrInitialize, errors.New(status.LastError))
	}

	generated, err := driver.Generate(ctx, harness.GenerateRequest{
		Prompt: "Return the JSON object {\"answer\":\"pong\"}.",
		Schema: smokeSchema,
		Model:  options.Model,
		Effort: options.Effort,
	})
	if err != nil {
		return Summary{}, err
	}
	session, err := driver.StartSession(ctx, harness.SessionOptions{
		PermissionMode: harness.PermissionApprovalRequired,
		Model:          options.Model,
		Effort:         options.Effort,
		Cwd:            options.Cwd,
	})
	if err != nil {
		return Summary{}, err
	}

	turn := Turn{}
	streamDone := make(chan error, 1)
	go func() {
		var streamErr error
		for event := range session.Events() {
			turn.Events++
			if event.Kind == harness.EventAssistantTextDelta {
				turn.Text += event.Text
			}
			if event.Kind == harness.EventTokenUsage {
				turn.Usage = event.Usage
			}
			if event.Kind == harness.EventApprovalRequested {
				if err := session.Respond(event.RequestID, harness.DecisionDeny); streamErr == nil && err != nil {
					streamErr = withPhase(harness.ErrTurnStart, err)
				}
			}
			if writeEvent != nil {
				if err := writeEvent(event); streamErr == nil && err != nil {
					streamErr = withPhase(harness.ErrTransportClosed, err)
				}
			}
		}
		streamDone <- streamErr
	}()

	turnErr := session.SendTurn(ctx, options.Prompt)
	stopErr := session.Stop()
	streamErr := <-streamDone
	if turnErr != nil {
		return Summary{}, withPhase(harness.ErrTurnStart, turnErr)
	}
	if stopErr != nil {
		return Summary{}, withPhase(harness.ErrTransportClosed, stopErr)
	}
	if streamErr != nil {
		return Summary{}, streamErr
	}
	return Summary{Status: status, Generate: generated, Turn: turn}, nil
}

func statusPhase(phase error, status harness.Status) error {
	detail := strings.TrimSpace(status.LastError)
	detail = strings.TrimPrefix(detail, phase.Error()+": ")
	if detail == "" || detail == phase.Error() {
		return phase
	}
	return harness.Phase(phase, errors.New(detail))
}

func withPhase(phase, err error) error {
	var phaseErr *harness.PhaseError
	if errors.As(err, &phaseErr) {
		return err
	}
	return harness.Phase(phase, err)
}
