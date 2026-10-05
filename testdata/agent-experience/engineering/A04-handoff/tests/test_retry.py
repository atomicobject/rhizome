import unittest

from src.retry import should_retry


class TwoPhaseRetryTests(unittest.TestCase):
    def test_zero_attempt_limit_has_no_retry(self):
        self.assertFalse(should_retry(0, max_attempts=0))

    def test_stops_at_the_configured_limit(self):
        self.assertFalse(should_retry(2, max_attempts=2))

    def test_allows_attempts_before_the_limit(self):
        self.assertTrue(should_retry(0, max_attempts=2))
        self.assertTrue(should_retry(1, max_attempts=2))

    def test_success_has_priority(self):
        self.assertFalse(should_retry(0, max_attempts=2, succeeded=True))


if __name__ == "__main__":
    unittest.main()
