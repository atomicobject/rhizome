import unittest
from datetime import datetime, timezone

from src.expiry import is_valid, parse_persisted_expiry


class ExpiryTests(unittest.TestCase):
    def test_exact_expiry_is_invalid(self):
        expiry = parse_persisted_expiry("2026-09-07T16:00:00Z")
        self.assertFalse(is_valid(expiry, datetime(2026, 9, 7, 16, tzinfo=timezone.utc)))

    def test_persisted_values_are_utc_aware(self):
        self.assertEqual(
            parse_persisted_expiry("2026-09-07T16:00:00Z").tzinfo,
            timezone.utc,
        )


if __name__ == "__main__":
    unittest.main()
