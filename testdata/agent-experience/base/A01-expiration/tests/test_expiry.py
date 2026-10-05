import unittest
from datetime import datetime, timezone

from src.expiry import is_valid, parse_persisted_expiry


class ExpiryTests(unittest.TestCase):
    def setUp(self):
        self.expiry = parse_persisted_expiry("2026-09-07T16:00:00Z")

    def test_valid_before_exact_expiry(self):
        self.assertTrue(is_valid(self.expiry, datetime(2026, 9, 7, 15, 59, 59, tzinfo=timezone.utc)))

    def test_invalid_at_exact_expiry(self):
        self.assertFalse(is_valid(self.expiry, datetime(2026, 9, 7, 16, 0, 0, tzinfo=timezone.utc)))

    def test_invalid_after_expiry(self):
        self.assertFalse(is_valid(self.expiry, datetime(2026, 9, 7, 16, 0, 1, tzinfo=timezone.utc)))

    def test_persisted_value_is_utc_aware(self):
        self.assertEqual(self.expiry.tzinfo, timezone.utc)


if __name__ == "__main__":
    unittest.main()
