"""Task service layer.

Keeps validation small but links to the note [[notes/task-flow]] so coderefs can be indexed alongside the codeanchor for `add_task`.
"""

from __future__ import annotations

from datetime import datetime
from typing import Dict, List

from todoapp.services.sync import push_updates
from todoapp.storage.memory import MemoryStore
from todoapp.utils import slugify

store = MemoryStore()

def add_task(title: str, owner: str, sync: bool = True) -> Dict[str, object]:
    """Create a new task and persist it.

    See @notes/task-flow for the flow rationale.
    """
    if not title.strip():
        raise ValueError("title is required")

    task = {
        "id": slugify(title),
        "title": title,
        "owner": owner,
        "created_at": datetime.utcnow().isoformat(),
        "synced": False,
    }

    store.persist(task)

    if sync:
        push_updates(task)
        task["synced"] = True
    return task

def list_tasks() -> List[Dict[str, object]]:
    """Return all tasks from memory."""
    return store.all()

def complete_task(task_id: str) -> None:
    """Mark a task as done without removing it."""
    store.mark_done(task_id)
