"""Retry helper used by the two-phase handoff fixture."""


def should_retry(attempts_completed: int, max_attempts: int = 2, succeeded: bool = False) -> bool:
    """Return whether another attempt may start."""

    if attempts_completed < 0 or max_attempts < 0:
        raise ValueError("attempt counts cannot be negative")
    if succeeded:
        return False
    return attempts_completed <= max_attempts
