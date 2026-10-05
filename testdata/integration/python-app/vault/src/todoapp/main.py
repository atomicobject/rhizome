"""Entry point for the TODO CLI.

This file mirrors how a small team might wire a command handler. Product intent is captured in [[notes/product-brief]], while engineering details stay in [[communities/engineering]].
"""

from todoapp.services.tasks import add_task
from todoapp.services.sync import push_updates


def cli_add(title: str, owner: str, sync: bool = True) -> dict:
    """Handle CLI args and call the service layer."""
    task = add_task(title=title, owner=owner)
    if sync:
        push_updates(task)
    return task


def cli_dry_run(title: str, owner: str) -> dict:
    """Create a task without syncing for testing."""
    return add_task(title=title, owner=owner, sync=False)


if __name__ == "__main__":
    example = cli_add("Write integration test", owner="dev")
    print(example)
