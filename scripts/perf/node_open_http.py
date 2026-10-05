"""Replay the checked-in browser detail query against a disposable local server."""

import argparse
import json
from pathlib import Path
import re
import time
from urllib.request import Request, urlopen


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--samples", type=int, default=3)
    parser.add_argument("--ref", action="append", required=True)
    args = parser.parse_args()
    source = Path(__file__).resolve().parents[2] / "web/src/api/graphql/operations.ts"
    match = re.search(r"PUBLIC_NODE_DETAIL_QUERY\s*=\s*/\* GraphQL \*/\s*`([^`]+)`", source.read_text())
    if not match:
        parser.error("could not extract PUBLIC_NODE_DETAIL_QUERY")
    args.output.mkdir(parents=True, exist_ok=True)
    rows = []
    for index, ref in enumerate(args.ref):
        for sample in range(args.samples):
            request = Request(
                f"http://127.0.0.1:{args.port}/api/v1/graphql",
                data=json.dumps({"query": match[1], "variables": {"ref": ref}}).encode(),
                headers={"Content-Type": "application/json"},
            )
            start = time.perf_counter()
            with urlopen(request, timeout=120) as response:
                raw = response.read()
            elapsed = (time.perf_counter() - start) * 1000
            result = json.loads(raw)
            (args.output / f"{index}-{sample}-payload.json").write_bytes(raw)
            if result.get("errors") or not (result.get("data") or {}).get("node"):
                raise RuntimeError(f"invalid detail response for {ref}; inspect retained payload")
            row = {"ref": ref, "sample": sample, "ms": elapsed}
            rows.append(row)
            print(json.dumps(row), flush=True)
            (args.output / "results.json").write_text(json.dumps(rows, indent=2))


if __name__ == "__main__":
    main()
