#!/usr/bin/env python3
"""Reject new oversized Go files and growth of existing oversized files."""

import json
from pathlib import Path
import subprocess
import sys

LIMIT = 500
ROOT = Path(__file__).resolve().parents[2]


def violations(sizes, baseline):
    return [
        f"{path}: {lines} lines, limit {max(LIMIT, baseline.get(path, 0))}; split the file"
        for path, lines in sorted(sizes.items())
        if lines > max(LIMIT, baseline.get(path, 0))
    ]


def main():
    baseline = json.loads((Path(__file__).with_name("file_size_baseline.json")).read_text())
    paths = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "cmd", "pkg", "tools"],
        cwd=ROOT,
    ).decode().split("\0")
    sizes = {
        path: len((ROOT / path).read_bytes().splitlines())
        for path in set(paths)
        if path.endswith(".go")
        and not path.endswith("_test.go")
        and "testdata" not in Path(path).parts
        and (ROOT / path).is_file()
    }
    findings = violations(sizes, baseline)
    if findings:
        print("\n".join(findings), file=sys.stderr)
    return bool(findings)


if __name__ == "__main__":
    sys.exit(main())
