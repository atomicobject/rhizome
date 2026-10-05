# Todo service code context

The Python package `todoapp` exposes a minimal CLI to add tasks and sync them. The code links to notes using wikilinks and @mentions so coderefs are discoverable. Anchors defined in [[notes/task-flow]] map to `add_task` (task intake) and the in-memory store's `persist` function.

Modules of interest:
- `todoapp.services.tasks` handles validation and task lifecycle
- `todoapp.storage.memory` keeps an in-memory backing store
- `todoapp.services.sync` publishes changes to a fake queue
