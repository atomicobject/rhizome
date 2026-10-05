package semantic

import (
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

type plannedTask[ID comparable, Payload any] struct {
	id          ID
	fingerprint string
	payload     Payload
}

type plannedWorkPlan[Task any, State any] struct {
	tasks     []Task
	state     State
	TotalWork int
	CacheHits int
	skipEmbed bool
}

func (p plannedWorkPlan[Task, State]) ShouldSkipEmbed() bool {
	return p.skipEmbed
}

func (p plannedWorkPlan[Task, State]) TaskCount() int {
	return len(p.tasks)
}

type codeTaskPayload struct {
	ownerKind string
	chunks    []SemanticChunk
}

type codePlanState struct {
	states          map[codeindex.AnchorID]codeindex.ItemEmbeddingState
	forceReembed    bool
	reuseCache      *codeReuseCache
	sourceHighWater time.Time
}

type codeTask = plannedTask[codeindex.AnchorID, codeTaskPayload]
type SyncPlan = plannedWorkPlan[codeTask, codePlanState]

func mergeCodeTasks(taskSets ...[]codeTask) []codeTask {
	total := 0
	for _, tasks := range taskSets {
		total += len(tasks)
	}
	if total == 0 {
		return nil
	}
	out := make([]codeTask, 0, total)
	seen := make(map[codeindex.AnchorID]struct{}, total)
	for _, tasks := range taskSets {
		for _, task := range tasks {
			if _, ok := seen[task.id]; ok {
				continue
			}
			seen[task.id] = struct{}{}
			out = append(out, task)
		}
	}
	return out
}

type noteTaskPayload struct {
	info        embeddings.NoteFileInfo
	sections    []codeanchor.IntelDocSection
	allChunkIdx []int
	reuseChunks []embeddings.ChunkInput
	reuseVecs   []embeddings.Embedding
	embedChunks []embeddings.ChunkInput
	embedTexts  []string
}

type notePlanState struct {
	ids                []embeddings.NoteID
	deleteIDs          []embeddings.NoteID
	typedRawPrunePaths []string
	incremental        bool
	useLazyPruning     bool
	sourceHighWater    time.Time
}

type noteTask = plannedTask[embeddings.NoteID, noteTaskPayload]
type NotePlan = plannedWorkPlan[noteTask, notePlanState]
