package worker

import "example.com/polyglot/todo"

// Worker that calls PushUpdates for functionUse anchor coverage.
// - [[notes/task-flow]]
// - @notes/task-flow
func Run() {
	todo.PushUpdates(map[string]any{"id": "demo"})
}
