package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// roundTrip sends a control request and waits for its response.
func (s *session) roundTrip(ctx context.Context, id string, request message, phase error) (message, error) {
	waiter := make(chan message, 1)
	s.stateMu.Lock()
	s.responses[id] = waiter
	if buffered, ok := s.responseBacklog[id]; ok {
		delete(s.responseBacklog, id)
		waiter <- buffered
	}
	s.stateMu.Unlock()
	defer func() {
		s.stateMu.Lock()
		delete(s.responses, id)
		s.stateMu.Unlock()
	}()
	if err := s.transport.Send(ctx, request); err != nil {
		if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, io.EOF) {
			// The read loop may already have failed the session (bad version,
			// decode error) and closed the transport; report that cause.
			return message{}, s.transportError()
		}
		return message{}, classifyError(ctx, phase, err)
	}
	select {
	case msg := <-waiter:
		return msg, nil
	case <-ctx.Done():
		return message{}, classifyError(ctx, phase, ctx.Err())
	case <-s.lifetime.Done():
		return message{}, s.transportError()
	}
}

// admission says why a turn or model change cannot start; stateMu is held.
// A model change and a turn never overlap, so a change cannot land mid-turn.
func (s *session) admission() error {
	switch {
	case s.stopped:
		return harness.Phase(harness.ErrTransportClosed, errors.New("session stopped"))
	case s.running:
		return harness.Phase(harness.ErrTurnStart, errors.New("turn already running"))
	case s.changingModel:
		return harness.Phase(harness.ErrTurnStart, errors.New("model change in progress"))
	}
	return nil
}

// SetModel sends set_model and apply_flag_settings, verified against Claude
// Code 2.1.325, for whichever of the two changed. An empty model selects
// Claude Code's "default" model and an empty effort clears the override.
func (s *session) SetModel(ctx context.Context, model, effort string) error {
	s.stateMu.Lock()
	err := s.admission()
	if err == nil {
		s.changingModel = true
	}
	modelChanged, effortChanged := model != s.model, effort != s.effort
	s.stateMu.Unlock()
	if err != nil {
		return err
	}
	defer func() {
		s.stateMu.Lock()
		s.changingModel = false
		s.stateMu.Unlock()
	}()
	// Claude gets as long to answer as it gets to end a turn after an interrupt.
	ctx, cancel := context.WithTimeout(ctx, s.interruptTimeout)
	defer cancel()
	if modelChanged {
		name := model
		if name == "" {
			name = "default"
		}
		if err := s.control(ctx, map[string]any{"subtype": "set_model", "model": name}); err != nil {
			return err
		}
		s.stateMu.Lock()
		s.model = model
		s.stateMu.Unlock()
	}
	if effortChanged {
		var level any
		if effort != "" {
			level = effort
		}
		if err := s.control(ctx, map[string]any{"subtype": "apply_flag_settings", "settings": map[string]any{"effortLevel": level}}); err != nil {
			return err
		}
		s.stateMu.Lock()
		s.effort = effort
		s.stateMu.Unlock()
	}
	return nil
}

func (s *session) control(ctx context.Context, request map[string]any) error {
	id := fmt.Sprintf("control-%d", s.controlSeq.Add(1))
	body, _ := json.Marshal(request)
	msg, err := s.roundTrip(ctx, id, message{Type: "control_request", RequestID: id, Request: body}, harness.ErrTurnStart)
	if err != nil && ctx.Err() != nil {
		// Claude may still apply a change it did not answer, even during a
		// later turn, so the process cannot be trusted with another turn.
		_ = s.Stop()
		return harness.Phase(harness.ErrTimeout, fmt.Errorf("Claude did not answer %s", request["subtype"]))
	}
	if err != nil {
		return err
	}
	var reply struct {
		Subtype string `json:"subtype"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(msg.Response, &reply); err != nil {
		return harness.Phase(harness.ErrDecode, err)
	}
	if reply.Subtype == "error" {
		return harness.Phase(harness.ErrCommandFailed, fmt.Errorf("Claude Code refused %s: %s", request["subtype"], reply.Error))
	}
	return nil
}
