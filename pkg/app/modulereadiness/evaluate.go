package modulereadiness

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/actions"
	"github.com/atomicobject/rhizome/pkg/vault/modulelifecycle"
)

func (c *Coordinator) evaluate(ctx context.Context, d modulelifecycle.Descriptor, key *string, trust string, s Snapshot, input map[string]any) Snapshot {
	fail := func(err error) Snapshot {
		if current, err := modulelifecycle.Fingerprint(c.opts.Root, d); err == nil {
			*key = current + trust
		}
		s.State = Failed
		s.Reason = err.Error()
		s.Recovery = []Recovery{{Kind: Retry, Label: "Retry"}}
		return s
	}
	schema := func() error {
		if c.opts.Schema != nil {
			return c.opts.Schema()
		}
		return nil
	}
	if err := schema(); err != nil {
		s.State = Blocked
		s.Reason = err.Error()
		s.Recovery = []Recovery{{Kind: Review, Label: "Review schema conflict", Paths: []string{".rhizome/ontology", ".rhizome/modules"}}, {Kind: Retry, Label: "Retry"}}
		return s
	}
	if input != nil {
		s.State = Working
		s.Reason = "Applying your selection"
		c.publish(d.Module, entry{*key, s})
		if err := c.action(ctx, d.Module+"/"+d.Setup, input, nil); err != nil {
			return fail(err)
		}
	}
	for step := 0; step < 4; step++ {
		before, err := modulelifecycle.Fingerprint(c.opts.Root, d)
		if err != nil {
			return fail(err)
		}
		var a Assessment
		action := d.Observe
		if step > 0 {
			action = d.Verify
		}
		actionErr := c.action(ctx, d.Module+"/"+action, nil, &a)
		after, err := modulelifecycle.Fingerprint(c.opts.Root, d)
		if err != nil {
			return fail(err)
		}
		if before != after {
			s.State = Checking
			return s
		}
		if actionErr != nil {
			return fail(actionErr)
		}
		if err = schema(); err != nil {
			s.State = Blocked
			s.Reason = err.Error()
			s.Recovery = []Recovery{{Kind: Review, Label: "Review schema conflict"}, {Kind: Retry, Label: "Retry"}}
			return s
		}
		current, err := modulelifecycle.Fingerprint(c.opts.Root, d)
		if err != nil {
			return fail(err)
		}
		if current != before {
			s.State = Checking
			return s
		}
		*key = current + trust
		s.Reason = a.Reason
		s.Recovery = a.Recovery
		switch a.Next {
		case NextReady:
			s.State = Ready
			return s
		case NextInput:
			s.State = RequiredInput
			return s
		case NextBlocked:
			s.State = Blocked
			return s
		case NextPrepare, NextIndex:
			if step == 3 {
				return fail(fmt.Errorf("setup did not converge: %s", a.Reason))
			}
			s.State = Working
			s.Recovery = nil
			c.publish(d.Module, entry{*key, s})
			if a.Next == NextPrepare {
				if err = c.action(ctx, d.Module+"/"+d.Setup, a.Input, nil); err != nil {
					return fail(err)
				}
			} else {
				if c.opts.Include != nil {
					if err = c.opts.Include(ctx, a.Includes); err != nil {
						return fail(err)
					}
				}
				if err = c.action(ctx, d.Module+"/"+d.Index, nil, nil); err != nil {
					return fail(err)
				}
			}
		default:
			return fail(fmt.Errorf("module assessment has invalid next state %q", a.Next))
		}
	}
	return fail(fmt.Errorf("setup did not finish"))
}
func (c *Coordinator) action(ctx context.Context, id string, input map[string]any, result *Assessment) error {
	envelope, err := c.opts.Runner.Run(ctx, id, input)
	if err != nil {
		return err
	}
	if envelope.Status != actions.StatusOK {
		return fmt.Errorf("%s: %s %s", id, envelope.Reason, envelope.StderrTail)
	}
	if result != nil {
		if err = json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("%s assessment: %w", id, err)
		}
	}
	return nil
}
