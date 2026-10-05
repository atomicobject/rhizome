"""Expiration checks for the synthetic evaluation repository."""

from datetime import datetime, timezone


UTC = timezone.utc


def parse_persisted_expiry(value: str) -> datetime:
    """Parse the persisted UTC ISO-8601 representation."""

    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("persisted expiry must include a UTC offset")
    return parsed.astimezone(UTC)


def is_valid(expiry: datetime, now: datetime | None = None) -> bool:
    """Return whether the record is valid at the current time."""

    current = (now or datetime.now(UTC)).astimezone(UTC)
    return current <= expiry.astimezone(UTC)
