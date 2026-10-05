import unittest

from src.retry import should_retry


class RetryLimitTests(unittest.TestCase):
    def test_allows_retry_before_limit(self):
        self.assertTrue(should_retry(0, max_attempts=3))
        self.assertTrue(should_retry(2, max_attempts=3))

    def test_stops_at_limit(self):
        self.assertFalse(should_retry(3, max_attempts=3))

    def test_success_never_retries(self):
        self.assertFalse(should_retry(0, max_attempts=3, succeeded=True))
        self.assertFalse(should_retry(2, max_attempts=3, succeeded=True))

    def test_rejects_negative_inputs(self):
        with self.assertRaises(ValueError):
            should_retry(-1)
        with self.assertRaises(ValueError):
            should_retry(0, max_attempts=-1)


if __name__ == "__main__":
    unittest.main()
