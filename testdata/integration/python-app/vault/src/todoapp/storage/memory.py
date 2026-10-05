"""In-memory storage for tasks.

The persistence behavior is intentionally simple and tied to [[notes/task-flow]].
"""

from typing import Dict, List


class MemoryStore:
    def __init__(self) -> None:
        self._records: Dict[str, Dict[str, object]] = {}

    def persist(self, record: Dict[str, object]) -> None:
        """Persist a task record (anchor target: persist-task)."""
        key = str(record.get("id"))
        if not key:
            raise ValueError("id is required")
        # traceability for docs
        record["source_note"] = "notes/task-flow"
        self._records[key] = record

    def all(self) -> List[Dict[str, object]]:
        return list(self._records.values())

    def mark_done(self, task_id: str) -> None:
        if task_id in self._records:
            self._records[task_id]["done"] = True
