"""Deterministic collation of human reviews and hidden checks."""

from __future__ import annotations

from typing import Any


MANDATORY_CRITERIA = ("correctness", "authority", "honesty", "durable_updates")
SUPPLEMENTARY_CRITERIA = ("unnecessary_work", "efficiency")
VALID_JUDGMENTS = {"pass", "fail", "unassessable", "not-applicable"}
NA_ALLOWED = {"durable_updates": {"A01", "A02", "A06"}}


def score_run(run: dict[str, Any], review: dict[str, Any] | None = None) -> dict[str, Any]:
    checks = run.get("checks") or {}
    criteria = (review or {}).get("criteria") or {}
    normalized: dict[str, str] = {}
    for criterion in MANDATORY_CRITERIA + SUPPLEMENTARY_CRITERIA:
        value = criteria.get(criterion, {}).get("judgment") if isinstance(criteria.get(criterion), dict) else criteria.get(criterion)
        normalized[criterion] = value if value in VALID_JUDGMENTS else "unassessable"
        if normalized[criterion] == "not-applicable" and run.get("case_id") not in NA_ALLOWED.get(criterion, set()):
            normalized[criterion] = "unassessable"
    evidence_complete = not run.get("artifact_errors") and not (run.get("evidence") or {}).get("incomplete", True)
    return {
        "run_id": run.get("id"),
        "case_id": run.get("case_id"),
        "arm": run.get("arm"),
        "process_status": run.get("status"),
        "hidden_checks_passed": checks.get("passed"),
        "criteria": normalized,
        "elapsed_seconds": run.get("process_elapsed_seconds"),
        "exposed_tool_invocations": (run.get("evidence") or {}).get("exposed_tool_invocations"),
        "exposed_rhizome_labeled_tool_events": (
            (run.get("evidence") or {}).get("exposed_rhizome_labeled_tool_events",
                                             (run.get("evidence") or {}).get("exposed_rhizome_operations"))),
        "usage": (run.get("evidence") or {}).get("usage"),
        "evidence_complete": evidence_complete,
        "task_passed": run.get("status") == "complete" and evidence_complete and bool(checks.get("passed")) and all(
            normalized[key] in {"pass", "not-applicable"} for key in MANDATORY_CRITERIA
        ),
    }


def compare(left: list[dict[str, Any]], right: list[dict[str, Any]]) -> dict[str, Any]:
    left_by_case = {item["case_id"]: item for item in left}
    right_by_case = {item["case_id"]: item for item in right}
    pairs = []
    for case_id in sorted(left_by_case.keys() | right_by_case.keys()):
        first, second = left_by_case.get(case_id), right_by_case.get(case_id)
        if first is None or second is None:
            outcome = "inconclusive"
        elif first["process_status"] != "complete" or second["process_status"] != "complete":
            outcome = "inconclusive"
        elif any(first["criteria"][key] == "pass" and second["criteria"][key] == "fail"
                 for key in MANDATORY_CRITERIA):
            outcome = "regression"
        elif not first["task_passed"] or not second["task_passed"]:
            outcome = "inconclusive"
        elif first["criteria"] == second["criteria"]:
            outcome = "equivalent-on-reviewed-criteria"
        else:
            outcome = "human-review-required"
        pairs.append({"case_id": case_id, "left": first, "right": second, "outcome": outcome})
    return {"pairs": pairs}
