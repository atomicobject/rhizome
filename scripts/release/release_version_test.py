#!/usr/bin/env python3
"""Regression tests for the release version recommendation menu."""

from __future__ import annotations

import unittest

from release_version import choose_release_version


class ReleaseVersionTest(unittest.TestCase):
    def choose(self, recommendation: str, answers: list[str]):
        output: list[str] = []
        prompts: list[str] = []

        def answer(prompt: str) -> str:
            prompts.append(prompt)
            return answers.pop(0)

        version = choose_release_version(
            "v0.49.0",
            recommendation,
            supplied=None,
            environ={},
            input_fn=answer,
            output_fn=output.append,
        )
        return version, output, prompts

    def test_enter_accepts_recommended_minor_increment(self) -> None:
        version, output, prompts = self.choose("minor", [""])

        self.assertEqual(version, "v0.50.0")
        self.assertIn("1. minor  v0.50.0  (recommended, default)", output)
        self.assertEqual(prompts, ["Choose version [1-4, Enter for recommended]: "])

    def test_enter_accepts_recommended_patch_increment(self) -> None:
        version, output, _ = self.choose("patch", [""])

        self.assertEqual(version, "v0.49.1")
        self.assertIn("1. patch  v0.49.1  (recommended, default)", output)
        self.assertIn("2. minor  v0.50.0", output)

    def test_numbered_and_custom_choices_match_legacy_menu(self) -> None:
        second, _, _ = self.choose("minor", ["2"])
        major, _, _ = self.choose("minor", ["3"])
        custom, _, _ = self.choose("minor", ["4", "v0.49.7"])

        self.assertEqual(second, "v0.49.1")
        self.assertEqual(major, "v1.0.0")
        self.assertEqual(custom, "v0.49.7")


if __name__ == "__main__":
    unittest.main()
