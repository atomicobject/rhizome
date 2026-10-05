"""Fake sync module.

References [[notes/sync-strategy]] to show coderefs from comments.
"""

from typing import Dict, Any

from observability.decorators import instrument


@instrument(tag="sync")
def push_updates(task: Dict[str, Any]) -> None:
    """Pretend to send a task to a queue."""
    # @notes/sync-strategy keeps the desired sync invariants.
    task["synced"] = True
