"""Retry decision helper for one bounded operation."""


def should_retry(attempts_completed: int, max_attempts: int = 3, succeeded: bool = False) -> bool:
    """Return whether another attempt may start."""

    if attempts_completed < 0:
        raise ValueError("attempt count cannot be negative")
    if max_attempts < 0:
        raise ValueError("maximum attempts cannot be negative")
    if succeeded:
        return False
    return attempts_completed <= max_attempts
