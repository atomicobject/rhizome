#!/usr/bin/env python3
"""Compare two binaries on fresh and incremental indexing of linked notes.

Uses only disposable fixture copies. Keeps logs and databases for inspection.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess
import tempfile
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("baseline", type=Path)
    parser.add_argument("revision", type=Path)
    parser.add_argument("--pairs", type=int, default=5)
    args = parser.parse_args()
    if args.pairs < 1:
        parser.error("--pairs must be positive")
    binaries = {"base": args.baseline.resolve(), "revision": args.revision.resolve()}
    source = Path(__file__).resolve().parents[2] / "testdata/integration/python-app/vault"
    root = Path(tempfile.mkdtemp(prefix="graph-revision-index-"))
    results = {key: [] for key in ("base", "revision", "base_incremental", "revision_incremental")}
    env = {**os.environ, "GOMAXPROCS": "2", "RZM_SKIP_REPO_DELEGATE": "1"}

    def index(label, target, log):
        start = time.perf_counter()
        result = subprocess.run(
            [str(binaries[label]), "index", "--vault", str(target)],
            cwd=target, env=env, capture_output=True, text=True, check=False,
        )
        elapsed = (time.perf_counter() - start) * 1000
        log.write_text(result.stdout + result.stderr)
        if result.returncode:
            raise RuntimeError(f"Index failed; inspect {log}")
        return elapsed

    for run in range(args.pairs):
        for label in (("base", "revision") if run % 2 == 0 else ("revision", "base")):
            target = root / f"{label}-{run}"
            shutil.copytree(source, target)
            for i in range(500):
                links = "\n".join(f"[[scaled-{(i+j+1)%500}]]" for j in range(20))
                (target / f"scaled-{i}.md").write_text(f"# Scaled {i}\n\n{links}")
            results[label].append(index(label, target, root / f"{label}-{run}.log"))
            for i in range(50):
                links = "\n".join(f"[[scaled-{(i+j+50)%500}]]" for j in range(20))
                (target / f"scaled-{i}.md").write_text(f"# Changed {i}\n\n{links}")
            results[f"{label}_incremental"].append(
                index(label, target, root / f"{label}-{run}-incremental.log")
            )
    print(json.dumps({
        "fixtureRoot": str(root), "binaries": {k: str(v) for k, v in binaries.items()},
        "results_ms": results,
        "median_ms": {k: statistics.median(v) for k, v in results.items()},
    }, indent=2))


if __name__ == "__main__":
    main()
