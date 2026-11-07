package indexwriter

import (
	"context"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/stretchr/testify/require"
)

func TestCodePriorityDoesNotDependOnMetricNameOrFlushOrder(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"renamed metric", "reordered threshold", "reordered idle"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.DefaultPolicy = flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Millisecond}
			cfg.CodeIndex = flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour}
			cfg.CodePriority = time.Hour
			flushed := 0
			q := &Writer{ctx: context.Background(), cfg: cfg, handlers: Handlers{
				ApplyCodeIndexBatch: func(context.Context, []codeanchor.CodeIndexWork) error { return nil },
				ApplyNoteMetaBatch:  func(context.Context, []embeddings.NoteFileInfo) error { flushed++; return nil },
			}}
			s := newQueueState(q)
			code := s.codeKind.(*typedPayloadKind[codeanchor.CodeIndexWork])
			notes := s.kinds[2].(*typedPayloadKind[embeddings.NoteFileInfo])
			if scenario == "renamed metric" {
				code.op = "renamed.code.operation"
				code.policy.Rows = 1
			} else {
				s.kinds[0], s.kinds[len(s.kinds)-1] = s.kinds[len(s.kinds)-1], s.kinds[0]
				cfg.CodePriority = 0
				q.cfg = cfg
			}
			if scenario == "reordered idle" {
				notes.immediate = false
			}
			now := time.Now()
			_, err := code.offer(q, s, queuedPayload{value: codeanchor.CodeIndexWork{Path: "busy.go"}}, now)
			require.NoError(t, err)
			_, err = notes.offer(q, s, queuedPayload{value: embeddings.NoteFileInfo{Path: "note.md"}}, now)
			require.NoError(t, err)
			require.NoError(t, s.flushExpired(q, now.Add(time.Second)))
			require.Zero(t, flushed, "semantic writes must stay deferred while code work is hot")
			require.NoError(t, s.flushAll(q))
			require.Equal(t, 1, flushed, "a barrier must still drain the deferred semantic write")
		})
	}
}
