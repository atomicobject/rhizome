import unittest

from src.retention import retention_days


class RetentionTests(unittest.TestCase):
    def test_current_contract_is_thirty_days(self):
        self.assertEqual(retention_days(), 30)


if __name__ == "__main__":
    unittest.main()
