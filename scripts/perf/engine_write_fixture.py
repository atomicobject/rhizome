#!/usr/bin/env python3
"""Run reproducible fresh-index, unchanged-index, and one-note-edit samples."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sqlite3
import time


def create_fixture(root):
    root.mkdir()
    for directory in (".rhizome", "notes", "src"):
        (root / directory).mkdir()
    (root / ".rhizome/config.yml").write_text(
        "code:\n  enabled: true\n  go:\n    roots: [src]\n"
        "noteEmbeddings:\n  enabled: false\ncodeEmbeddings:\n  enabled: false\n"
    )
    for i in range(1000):
        (root / "notes" / f"n{i:04d}.md").write_text(
            f"---\naliases: [alias{i}]\ntags: [bench]\n---\n"
            f"# Note {i}\nSee [[n0000]].\n"
        )
    for i in range(100):
        (root / "src" / f"f{i:04d}.go").write_text(
            f"package bench\n// F{i} refers to [[n0000]].\n"
            f"func F{i}() int {{ return {i} }}\n"
        )
    (root / "go.mod").write_text("module example.com/bench\n\ngo 1.24\n")


def persisted_counts(root):
    tables = ("notes", "files", "symbols", "intel_code_anchors", "doc_links",
              "graph_doc_scores", "graph_doc_edges")
    database = root / ".rhizome/db.sqlite"
    with sqlite3.connect(database.as_uri() + "?mode=ro", uri=True) as connection:
        return {table: connection.execute(f"SELECT count(*) FROM {table}").fetchone()[0]
                for table in tables}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True,
                        help="New output directory; existing paths are rejected")
    parser.add_argument("--samples", type=int, default=3)
    args = parser.parse_args()
    if args.samples < 1:
        parser.error("--samples must be positive")
    binary = args.binary.resolve(strict=True)
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    env = dict(os.environ, GOMAXPROCS="4", RZM_SKIP_REPO_DELEGATE="1")
    evidence = {
        "binary": str(binary),
        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "version": subprocess.check_output([str(binary), "--version"], env=env, text=True).strip(),
        "gomaxprocs": 4,
        "fixture": {"notes": 1000, "code_files": 100, "embeddings": False},
        "cold_definition": "fresh database; operating-system file caches are not flushed",
        "runs": [],
    }
    for sample in range(args.samples):
        root = output / f"fixture-{sample}"
        create_fixture(root)
        initial_counts = None
        for phase in ("cold", "noop-0", "noop-1", "noop-2", "note-edit"):
            if phase == "note-edit":
                with (root / "notes/n0001.md").open("a") as note:
                    note.write("\nAn added paragraph for the incremental index sample.\n")
            log_path = output / f"sample-{sample}-{phase}.log"
            with log_path.open("w") as log:
                started = time.perf_counter()
                result = subprocess.run([str(binary), "index", "--timings"],
                                        cwd=root, env=env, stdout=log, stderr=subprocess.STDOUT)
                elapsed = time.perf_counter() - started
            record = {"sample": sample, "phase": phase, "seconds": elapsed,
                      "exit_code": result.returncode, "log": str(log_path)}
            if result.returncode == 0:
                counts = persisted_counts(root)
                record["persisted_counts"] = counts
                if initial_counts is None:
                    initial_counts = counts
                record["counts_stable"] = counts == initial_counts
                record["expected_source_counts"] = (counts["notes"] == 1000
                                                    and counts["files"] == 100
                                                    and counts["symbols"] == 100)
            evidence["runs"].append(record)
            (output / "evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
            print(json.dumps(record), flush=True)
            if result.returncode:
                raise SystemExit(result.returncode)
            if not record["counts_stable"] or not record["expected_source_counts"]:
                raise SystemExit("Persisted fixture counts changed; inspect evidence.json")


if __name__ == "__main__":
    main()
