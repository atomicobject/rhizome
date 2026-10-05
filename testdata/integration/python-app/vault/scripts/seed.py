"""Seed script to populate demo tasks.

Kept small so the integration test can show how a team might keep scripts alongside the vault.
"""

from todoapp.main import cli_add


def seed_demo() -> None:
    cli_add("Walk the dog", owner="demo", sync=False)
    cli_add("Write release plan", owner="pm", sync=True)


if __name__ == "__main__":
    seed_demo()
