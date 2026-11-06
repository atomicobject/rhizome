#!/usr/bin/env python3
"""Exact GitHub Actions identity checks for the release gateway gate."""

from __future__ import annotations

import json
import subprocess
from typing import Callable, Sequence


REPOSITORY = "atomicobject/rhizome"
WORKFLOW = "team-api-deploy.yml"
WORKFLOW_PATH = f".github/workflows/{WORKFLOW}"
API = f"repos/{REPOSITORY}/actions"
WAIT_SECONDS = 4 * 60 * 60
DISCOVERY_SECONDS = 10 * 60
POLL_SECONDS = 15


class GatewayError(RuntimeError):
    """The gateway could not be proven ready for client publication."""


def command(argv: Sequence[str]) -> str:
    try:
        return subprocess.run(argv, check=True, capture_output=True, text=True, timeout=120).stdout
    except (OSError, subprocess.SubprocessError) as error:
        raise GatewayError("GitHub gateway command failed; retained receipt permits reconciliation") from error


def api(runner: Callable, endpoint: str, *options: str):
    try:
        value = json.loads(runner(("gh", "api", endpoint, *options)))
    except Exception as error:
        raise GatewayError("GitHub gateway API failed or returned invalid JSON") from error
    return value


def workflow_id(runner: Callable, *, workflow: str = WORKFLOW) -> int:
    result = api(runner, f"{API}/workflows/{workflow}")
    if (not isinstance(result, dict) or result.get("path") != f".github/workflows/{workflow}"
            or type(result.get("id")) is not int or result["id"] <= 0):
        raise GatewayError("GitHub returned an unexpected gateway workflow")
    return result["id"]


def run_title(attempt: dict, source: dict) -> str:
    return f"Deploy team API {attempt['request_id']} {source['source_commit']}"


def validate_run(run: dict, attempt: dict, source: dict, expected_workflow: int, *,
                 workflow: str = WORKFLOW, title: str | None = None) -> dict:
    if not isinstance(run, dict):
        raise GatewayError("GitHub returned an invalid deployment run")
    run_id = run.get("id")
    expected_url = f"https://github.com/{REPOSITORY}/actions/runs/{run_id}"
    if (type(run_id) is not int or run_id <= 0
            or run.get("display_title") != (title if title is not None else run_title(attempt, source))
            or run.get("event") != "workflow_dispatch"
            or run.get("head_branch") != "main"
            or run.get("workflow_id") != expected_workflow
            or run.get("path") != f".github/workflows/{workflow}"
            or not isinstance(run.get("repository"), dict)
            or run["repository"].get("full_name") != REPOSITORY
            or run.get("html_url") != expected_url
            or (attempt.get("run_id") is not None and attempt["run_id"] != run_id)
            or (attempt.get("run_url") is not None and attempt["run_url"] != expected_url)):
        raise GatewayError("Gateway deployment run identity does not match the saved request")
    return run


def find_run(runner: Callable, attempt: dict, source: dict, expected_workflow: int, *,
             workflow: str = WORKFLOW, title: str | None = None):
    pages = api(runner, f"{API}/workflows/{workflow}/runs?event=workflow_dispatch&branch=main&per_page=100",
                "--paginate", "--slurp")
    if not isinstance(pages, list) or not pages:
        raise GatewayError("GitHub returned an invalid deployment run listing")
    runs = []
    totals = []
    for page in pages:
        if (not isinstance(page, dict) or type(page.get("total_count")) is not int
                or page["total_count"] < 0 or not isinstance(page.get("workflow_runs"), list)):
            raise GatewayError("GitHub returned an incomplete deployment run listing")
        totals.append(page["total_count"])
        runs.extend(page["workflow_runs"])
    if len(runs) < max(totals) or any(not isinstance(run, dict) for run in runs):
        raise GatewayError("GitHub deployment run listing was truncated")
    # Match the title before checking other identity fields: a wrong-ref/title collision
    # must fail instead of being silently ignored as an unrelated run.
    expected_title = title if title is not None else run_title(attempt, source)
    matches = [run for run in runs if run.get("display_title") == expected_title]
    if len(matches) > 1:
        raise GatewayError("Multiple deployment runs match the saved gateway request")
    return validate_run(matches[0], attempt, source, expected_workflow, workflow=workflow, title=title) if matches else None


def fetch_run(runner: Callable, attempt: dict, source: dict, expected_workflow: int, *,
              workflow: str = WORKFLOW, title: str | None = None):
    return validate_run(api(runner, f"{API}/runs/{attempt['run_id']}"), attempt, source, expected_workflow,
                        workflow=workflow, title=title)


def successful(run: dict) -> bool:
    status = run.get("status")
    if status == "completed":
        if run.get("conclusion") != "success":
            raise GatewayError(f"Gateway deployment did not succeed ({run.get('conclusion') or 'unknown outcome'})")
        return True
    if status not in {"queued", "in_progress", "waiting", "pending", "requested"}:
        raise GatewayError("GitHub returned an unknown deployment run status")
    return False
