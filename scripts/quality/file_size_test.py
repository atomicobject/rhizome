import unittest

from scripts.quality.file_size import violations


class FileSizeTest(unittest.TestCase):
    def test_only_existing_oversized_files_may_exceed_limit(self):
        self.assertEqual([], violations({"small.go": 500, "old.go": 700}, {"old.go": 750}))
        self.assertEqual(
            ["new.go: 501 lines, limit 500; split the file", "old.go: 751 lines, limit 750; split the file"],
            violations({"new.go": 501, "old.go": 751}, {"old.go": 750}),
        )


if __name__ == "__main__":
    unittest.main()
