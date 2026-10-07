// Package desktop is the process boundary between the native launcher and
// repository-selected Rhizome runtimes. It never exposes runtime control tokens.
package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const Protocol = 1
const MaxRequestBytes = 1 << 20

type Request struct {
	Protocol         int           `json:"protocol"`
	Operation        string        `json:"operation"`
	Folder           string        `json:"folder,omitempty"`
	Executable       string        `json:"executable,omitempty"`
	GlobalExecutable string        `json:"globalExecutable,omitempty"`
	Primary          string        `json:"primary,omitempty"`
	Setup            *SetupChoices `json:"setup,omitempty"`
	Key              string        `json:"key,omitempty"`
	Edits            *ScopeEdits   `json:"edits,omitempty"`
	// Restart makes open replace a live headless runtime with a fresh one.
	Restart bool `json:"restart,omitempty"`
	// Folders and Repositories select what the status operation reports.
	Folders      []string        `json:"folders,omitempty"`
	Repositories []RepositoryRef `json:"repositories,omitempty"`
}

type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (p *Problem) Error() string         { return p.Message }
func problem(code, message string) error { return &Problem{Code: code, Message: message} }

type Response struct {
	Protocol int      `json:"protocol"`
	Result   any      `json:"result,omitempty"`
	Error    *Problem `json:"error,omitempty"`
}

// Serve consumes exactly one bounded request. Errors still produce a complete
// JSON response; the native host uses that response even on a nonzero exit.
func Serve(ctx context.Context, s *Service, input io.Reader, output io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(input, MaxRequestBytes+1))
	var req Request
	if err == nil && len(data) > MaxRequestBytes {
		err = fmt.Errorf("request exceeds 1 MiB")
	}
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&req)
		if err == nil {
			var extra any
			if e := decoder.Decode(&extra); e != io.EOF {
				err = fmt.Errorf("expected exactly one JSON request")
			}
		}
	}
	response := Response{Protocol: Protocol}
	if err != nil {
		response.Error = &Problem{Code: "invalid_request", Message: "Send one valid JSON request of at most 1 MiB."}
	} else {
		response = s.Handle(ctx, req)
	}
	if err := json.NewEncoder(output).Encode(response); err != nil {
		return err
	}
	if response.Error != nil {
		return response.Error
	}
	return nil
}

func (s *Service) Handle(ctx context.Context, req Request) Response {
	response := Response{Protocol: Protocol}
	var err error
	if req.Key != "" && req.Operation != "initialize" {
		err = problem("invalid_request", "A search key is accepted only during initialization.")
	} else if req.Protocol != Protocol {
		err = problem("invalid_request", "Unsupported desktop bridge protocol.")
	} else {
		switch req.Operation {
		case "inspect":
			response.Result, err = s.Inspect(req.Folder)
		case "repository":
			response.Result, err = s.Repository(req.Folder, req.Primary)
		case "status":
			response.Result, err = s.Status(ctx, req)
		case "seed":
			response.Result, err = s.Seed(ctx, req)
		case "trust":
			response.Result, err = s.Trust(req.Folder)
		case "open":
			response.Result, err = s.Open(ctx, req)
		case "stop":
			response.Result, err = s.Stop(ctx, req)
		case "setup-report":
			response.Result, err = s.SetupReport(ctx, req)
		case "scope", "scope-edit":
			response.Result, err = s.Scope(ctx, req)
		case "initialize":
			response.Result, err = s.Initialize(ctx, req)
		case "global-status":
			response.Result, err = s.GlobalStatus(ctx, req.GlobalExecutable)
		case "global-install", "global-update":
			response.Result, err = s.InstallGlobal(ctx)
		default:
			err = problem("invalid_request", "Unknown desktop bridge operation.")
		}
	}
	if err != nil {
		response.Error = asProblem(err)
	}
	return response
}

func asProblem(err error) *Problem {
	var p *Problem
	if errors.As(err, &p) {
		return p
	}
	return &Problem{Code: "runtime_error", Message: err.Error()}
}
