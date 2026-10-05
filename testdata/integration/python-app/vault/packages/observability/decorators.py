"""Lightweight decorator to exercise annotation anchors."""

from typing import Callable, Any, Dict
from functools import wraps


def instrument(tag: str = "") -> Callable:
    def decorator(func: Callable) -> Callable:
        @wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            # no-op recorder, but we keep metadata for anchor matching
            wrapper._instrument_meta = {"tag": tag}  # type: ignore[attr-defined]
            return func(*args, **kwargs)

        return wrapper

    return decorator

