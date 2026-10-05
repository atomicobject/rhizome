#!/usr/bin/env python3
"""Argument parsing and command dispatch for the release orchestrator."""

from __future__ import annotations

import argparse
from typing import Any, Sequence


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Reviewable, resumable release orchestration for Rhizome maintainers."
    )
    subparsers = parser.add_subparsers(dest="command", required=True)
    for name in ("plan", "dry-run", "cut-release"):
        command = subparsers.add_parser(name)
        command.add_argument("--base", default="")
        command.add_argument("--head", default="HEAD")
        command.add_argument("--version", default="")
        command.add_argument("--guidance", default="")
        command.add_argument("--new-plan", action="store_true")
        if name == "cut-release":
            command.add_argument("--accept-degraded-evidence", action="store_true")
    for name in ("build", "apply", "publish", "resume"):
        command = subparsers.add_parser(name)
        command.add_argument("--base", required=True)
        command.add_argument("--head", required=True)
        if name in {"apply", "resume"}:
            command.add_argument("--accept-degraded-evidence", action="store_true")
    return parser


def run_cli(release: Any, argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    if args.command in {"plan", "dry-run", "cut-release"}:
        common = {
            "base": args.base or None,
            "head": args.head,
            "version": args.version or None,
            "guidance": args.guidance,
            "new_plan": args.new_plan,
        }
        if args.command == "plan":
            run = release.plan(**common)
            print(f"\nPlan: {run.run_dir / 'plan.json'}")
        elif args.command == "dry-run":
            release.dry_run(**common)
            print("Release dry run completed without repository or publish mutation.")
        else:
            run = release.plan(**common)
            release.build(run.plan.base_tag, run.plan.head_commit)
            release.apply(
                run.plan.base_tag,
                run.plan.head_commit,
                accept_degraded_evidence=args.accept_degraded_evidence,
            )
            release.publish(run.plan.base_tag, run.plan.head_commit)
            print(f"Release {run.plan.selected_version} complete.")
    elif args.command == "build":
        release.build(args.base, args.head)
    elif args.command == "apply":
        release.apply(
            args.base,
            args.head,
            accept_degraded_evidence=args.accept_degraded_evidence,
        )
    elif args.command == "publish":
        release.publish(args.base, args.head)
    else:
        release.resume(
            args.base,
            args.head,
            accept_degraded_evidence=args.accept_degraded_evidence,
        )
    return 0
