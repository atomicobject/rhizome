#!/usr/bin/env python3
"""Real committed-tree fixtures with injected, public-only GitHub reads."""

from __future__ import annotations

import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from release_gateway_source import REPOSITORY, REQUIRED, SourceError, resolve_source


DOCKER = """FROM golang AS build
COPY go.mod go.sum ./
COPY pkg/diagnostics ./pkg/diagnostics
COPY pkg/fileio ./pkg/fileio
COPY pkg/typesafe ./pkg/typesafe
COPY pkg/teamapi/wire ./pkg/teamapi/wire
COPY services/team-api/go.mod services/team-api/go.sum ./services/team-api/
COPY services/team-api ./
FROM scratch
COPY --from=build --chown=65532:65532 /out/api /api
"""


class Repository:
    def __init__(self, path: Path, origin: str = REPOSITORY) -> None:
        self.path = path
        path.mkdir()
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "test@example.com")
        self.git("remote", "add", "origin", f"https://github.com/{origin}.git")
        for name in REQUIRED:
            target = name if name in {"go.mod", "go.sum"} else f"{name}/fixture.txt"
            self.write(target, "synthetic input\n")
        self.write("services/team-api/Dockerfile", DOCKER)
        self.write("services/team-api/go.mod", "module synthetic\n")
        self.write("services/team-api/go.sum", "synthetic sums\n")
        self.commit("base")

    def git(self, *args: str) -> str:
        return subprocess.run(
            ("git", "-C", str(self.path), *args), check=True,
            capture_output=True, text=True,
        ).stdout

    def write(self, name: str, content: str) -> None:
        target = self.path / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content)

    def commit(self, message: str) -> str:
        self.git("add", "--all")
        self.git("commit", "--allow-empty", "-m", message)
        return self.git("rev-parse", "HEAD").strip()

    @property
    def head(self) -> str:
        return self.git("rev-parse", "HEAD").strip()


class GitHub:
    def __init__(self, public: Repository) -> None:
        self.public = public
        self.release, self.main = public.head, public.head
        self.calls: list[tuple] = []
        self.truncated = False
        self.fail_path: str | None = None

    def __call__(self, argv) -> str:
        self.calls.append(tuple(argv))
        if argv[0] == "git":
            return subprocess.run(argv, check=True, capture_output=True, text=True).stdout
        if argv[:2] != ("gh", "api"):
            raise AssertionError(argv)
        path = argv[2]
        if path == self.fail_path:
            raise RuntimeError("PRIVATE_SECRET must never appear in errors")
        if path in {f"repos/{REPOSITORY}", "repos/private/client"}:
            return json.dumps({
                "full_name": path.removeprefix("repos/"),
                "private": path == "repos/private/client",
            })
        prefix = f"repos/{REPOSITORY}/git/"
        if not path.startswith(prefix):
            raise AssertionError(f"unexpected remote operation: {path}")
        route = path.removeprefix(prefix)
        if route.startswith("ref/heads/"):
            sha = self.release if route == "ref/heads/release" else self.main
            if sha is None:
                raise RuntimeError("404")
            return json.dumps({"object": {"type": "commit", "sha": sha}})
        kind, sha = route.split("/")
        if kind == "commits":
            metadata = self.public.git("show", "-s", "--format=%T%n%P", sha).splitlines()
            return json.dumps({
                "sha": sha, "tree": {"sha": metadata[0]},
                "parents": [{"sha": parent} for parent in metadata[1].split()],
            })
        if kind == "trees":
            entries = []
            for line in self.public.git("ls-tree", "-z", sha).split("\0"):
                if not line:
                    continue
                metadata, name = line.split("\t")
                mode, object_kind, oid = metadata.split()
                entries.append({"path": name, "mode": mode, "type": object_kind, "sha": oid})
            return json.dumps({"sha": sha, "truncated": self.truncated, "tree": entries})
        raise AssertionError(path)


class SourceAgreementTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.public = Repository(self.root / "public")
        self.github = GitHub(self.public)

    def tearDown(self) -> None:
        self.temp.cleanup()

    def resolve(self, repo=None, sha=None, **kwargs):
        repo = repo or self.public
        return resolve_source(repo.path, sha or repo.head, runner=self.github, **kwargs)

    def test_public_selection_uses_committed_tree_and_deterministic_fingerprint(self) -> None:
        expected = self.resolve()
        self.public.write("go.mod", "uncommitted input must be ignored\n")
        self.assertEqual(self.resolve(), expected)
        self.assertEqual(expected["repository"], REPOSITORY)
        self.assertEqual(expected["source_commit"], self.github.release)
        self.assertEqual(len(expected["fingerprint"]), 64)
        self.assertTrue(all(call[0] in {"git", "gh"} for call in self.github.calls))

    def test_public_release_must_equal_current_release_even_on_retry(self) -> None:
        saved = self.public.head
        self.github.release = "a" * 40
        with self.assertRaisesRegex(SourceError, "current public release"):
            self.resolve(saved_source=saved)

    def test_missing_public_release_is_clear_and_creates_nothing(self) -> None:
        self.github.release = None
        before = self.public.git("show-ref")
        with self.assertRaisesRegex(SourceError, "existing release flow"):
            self.resolve()
        self.assertEqual(self.public.git("show-ref"), before)
        self.assertFalse(any("dispatch" in str(call) for call in self.github.calls))

    def test_private_separate_history_pins_source_and_compares_only_build_inputs(self) -> None:
        private = Repository(self.root / "private", "private/client")
        private.write("client-private.txt", "not sent anywhere\n")
        private_sha = private.commit("independent private release")
        self.assertNotEqual(private_sha, self.public.head)
        selected = self.resolve(private, internal=True)
        self.assertEqual(selected["release_commit"], private_sha)
        self.assertEqual(selected["source_commit"], self.public.head)
        self.public.write("unrelated.txt", "new public release\n")
        self.github.main = self.public.commit("advance main")
        self.github.release = self.github.main
        self.assertEqual(
            self.resolve(private, internal=True, saved_source=selected["source_commit"]),
            selected,
        )
        self.assertFalse(any(private_sha in str(call) for call in self.github.calls if call[0] == "gh"))

    def test_hotfix_deploys_release_not_main_merge(self) -> None:
        base = self.public.head
        self.public.write("main.txt", "main\n")
        main = self.public.commit("main work")
        self.public.git("checkout", "-b", "release", base)
        self.public.write("client.txt", "fix\n")
        release = self.public.commit("hotfix")
        self.public.git("checkout", "main")
        self.public.git("merge", "--no-ff", "release", "-m", "main-first merge")
        self.github.main, self.github.release = self.public.head, release
        self.assertEqual(self.public.git("show", "-s", "--format=%P", "HEAD").strip(), f"{main} {release}")
        self.assertEqual(self.resolve(sha=release)["source_commit"], release)

    def test_unintegrated_release_is_rejected(self) -> None:
        base = self.public.head
        self.public.write("main.txt", "main\n")
        self.github.main = self.public.commit("main work")
        self.public.git("checkout", "-b", "release", base)
        self.public.write("fix.txt", "fix\n")
        self.github.release = self.public.commit("hotfix")
        with self.assertRaisesRegex(SourceError, "canonical main integration"):
            self.resolve()

    def test_private_mismatch_names_path_and_hashes_not_content(self) -> None:
        private = Repository(self.root / "private", "private/client")
        private.write("pkg/typesafe/fixture.txt", "PRIVATE_SECRET")
        private.commit("different gateway")
        with self.assertRaisesRegex(SourceError, "pkg/typesafe") as caught:
            self.resolve(private, internal=True)
        self.assertNotIn("PRIVATE_SECRET", str(caught.exception))

    def test_optional_ignore_absence_and_content_both_matter(self) -> None:
        before = self.resolve()["fingerprint"]
        self.public.write(".dockerignore", "synthetic-ignore\n")
        self.github.release = self.github.main = self.public.commit("ignore")
        self.assertNotEqual(self.resolve()["fingerprint"], before)
        private = Repository(self.root / "private", "private/client")
        with self.assertRaisesRegex(SourceError, "missing.*.dockerignore"):
            self.resolve(private, internal=True)
        private.write(".dockerignore", "different-ignore\n")
        private.commit("ignore")
        with self.assertRaisesRegex(SourceError, "differs.*.dockerignore"):
            self.resolve(private, internal=True)

    def test_required_missing_and_git_file_mode_mismatch_block_agreement(self) -> None:
        private = Repository(self.root / "private", "private/client")
        private.git("update-index", "--chmod=+x", "go.mod")
        private.git("commit", "-m", "mode")
        with self.assertRaisesRegex(SourceError, "differs: go.mod"):
            self.resolve(private, internal=True)
        self.public.git("update-index", "--force-remove", "go.sum")
        self.public.git("commit", "-m", "missing")
        self.github.release = self.github.main = self.public.head
        with self.assertRaisesRegex(SourceError, "missing.*go.sum"):
            self.resolve()

    def test_truncated_tree_and_lookup_failure_block_without_leaking_diagnostics(self) -> None:
        self.github.truncated = True
        with self.assertRaisesRegex(SourceError, "incomplete"):
            self.resolve()
        self.github.truncated = False
        self.github.fail_path = f"repos/{REPOSITORY}/git/commits/{self.public.head}"
        with self.assertRaises(SourceError) as caught:
            self.resolve()
        self.assertNotIn("PRIVATE_SECRET", str(caught.exception))

    def test_mode_cannot_be_swapped_or_origin_overridden(self) -> None:
        with self.assertRaisesRegex(SourceError, "private release repository"):
            self.resolve(internal=True)
        private = Repository(self.root / "private", "private/client")
        with self.assertRaisesRegex(SourceError, "canonical public repository"):
            self.resolve(private)
        self.public.git("remote", "set-url", "origin", "https://credential@github.com/atomicobject/rhizome")
        with self.assertRaisesRegex(SourceError, "identify a GitHub"):
            self.resolve()

    def test_new_copy_globs_add_and_unsupported_syntax_are_rejected(self) -> None:
        for addition in (
            "COPY private ./private", "COPY pkg/* ./pkg", 'COPY ["go.mod", "./"]',
            "COPY --link go.mod ./", "COPY\tprivate\t./", "ADD extra ./",
            "COPY --from=unreviewed /secret /secret", "COPY \\\n+go.mod ./",
            "RUN --mount=type=bind,source=outside-manifest,target=/input cat /input",
            "# syntax=unreviewed/frontend:latest",
        ):
            with self.subTest(addition=addition):
                self.public.write("services/team-api/Dockerfile", DOCKER + addition + "\n")
                self.github.release = self.github.main = self.public.commit("copy change")
                with self.assertRaisesRegex(SourceError, "Dockerfile"):
                    self.resolve()

    def test_current_repository_dockerfile_syntax_is_supported(self) -> None:
        from release_gateway_source import validate_copy_sources
        real = Path(__file__).resolve().parents[2] / "services/team-api/Dockerfile"
        validate_copy_sources(real.read_text())


if __name__ == "__main__":
    unittest.main()
