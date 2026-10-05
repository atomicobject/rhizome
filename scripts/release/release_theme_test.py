#!/usr/bin/env python3

import unittest

from release_theme import ReleaseTheme, merge_curated_themes


class MergeCuratedThemesTest(unittest.TestCase):
    def test_generated_theme_covering_curated_source_is_not_duplicated(self):
        generated = ReleaseTheme(
            "- **Safer validation repairs.** Repairs are transactional and recoverable.",
            "Made validation repairs transactional and recoverable.",
            ("unreleased:1", "effort:EFF-0047"),
        )

        merged = merge_curated_themes(
            ("Added deterministic, journaled validation repair transactions.",),
            (generated,),
        )

        self.assertEqual(merged, (generated,))

    def test_curated_entry_omitted_by_model_is_preserved(self):
        generated = ReleaseTheme(
            "- **Faster startup.** Index readiness is visible and retryable.",
            "Made startup index readiness visible and retryable.",
            ("pr:146",),
        )

        merged = merge_curated_themes(
            ("Added deterministic validation repair transactions.",),
            (generated,),
        )

        self.assertEqual(len(merged), 2)
        self.assertEqual(
            merged[0],
            ReleaseTheme(
                "- Added deterministic validation repair transactions.",
                "Added deterministic validation repair transactions.",
                ("unreleased:1",),
            ),
        )
        self.assertEqual(merged[1], generated)


if __name__ == "__main__":
    unittest.main()
