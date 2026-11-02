#!/usr/bin/env python3
"""Team API reports and explicit settings changes using AWS CLI and rzm login."""
import argparse
import datetime as dt
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

import ops_settings

ROOT = Path(__file__).resolve().parents[2]
COUNTERS = ("requests", "input_bytes", "reserved_credits", "settled_credits", "unknown_outcomes")
PROVIDER_COUNTERS = ("requests", "reserved_nanodollars", "settled_nanodollars", "unknown_outcomes")
PAID_ROUTES = ("/v1/providers/voyage/v1/embeddings", "/v1/providers/typesafe/v1/systemone")


def command(args):
    try:
        result = subprocess.run(args, capture_output=True, text=True, timeout=90,
                                env={**os.environ, "AWS_PAGER": "", "RZM_SKIP_REPO_DELEGATE": "1"})
    except FileNotFoundError as exc:
        raise ValueError(f"Executable unavailable: {args[0]}") from exc
    if result.returncode:
        detail = result.stderr.strip()[:1200]
        if not detail:
            # rzm's JSON errors are written to stdout. Expose their code without
            # copying arbitrary subprocess output into an operational report.
            try:
                code = json.loads(result.stdout).get("error", {}).get("code", "")
                if isinstance(code, str) and re.fullmatch(r"[a-z_]{1,80}", code):
                    detail = code
            except (ValueError, AttributeError):
                pass
        raise ValueError(f"{args[0]} exited {result.returncode}: {detail or 'no diagnostic available'}")
    return json.loads(result.stdout)


def origin(value):
    value = value if value.startswith("https://") else "https://" + value
    url = urllib.parse.urlsplit(value)
    if url.scheme != "https" or not url.hostname or url.username or url.password or url.path not in ("", "/") or url.query or url.fragment:
        raise ValueError("Stack Endpoint must be a root HTTPS origin")
    return value.rstrip("/")


class Ops:
    def __init__(self, args):
        self.args = args
        identity = self.aws("sts", "get-caller-identity")
        if identity["Account"] != args.account:
            raise ValueError(f"AWS account is {identity['Account']}; expected {args.account}. Select the correct AWS profile.")
        self.identity = {"account": identity["Account"], "principal": identity["Arn"],
                         "region": args.region, "stack": args.stack}
        self.stack = self.aws("cloudformation", "describe-stacks", "--stack-name", args.stack)["Stacks"][0]
        self.outputs = {x["OutputKey"]: x["OutputValue"] for x in self.stack.get("Outputs", [])}

    def aws(self, *args):
        return command(["aws", *args, "--region", self.args.region, "--output", "json", "--no-cli-pager"])

    def admin(self, *args):
        endpoint = origin(self.outputs.get("Endpoint", ""))
        return command([str(self.args.rzm), "auth", "admin", *args, "--server", endpoint, "--json"])

    def service(self):
        arn = self.outputs.get("ServiceArn")
        if not arn:
            raise ValueError("Stack has no ServiceArn; serving is not enabled")
        cluster, service = arn.split(":service/", 1)[1].split("/", 1)
        return arn, cluster, service

    def status(self):
        arn, cluster, service = self.service()
        endpoint = origin(self.outputs.get("Endpoint", ""))
        expected = self.aws("cloudformation", "describe-stack-resource", "--stack-name", self.args.stack,
                            "--logical-resource-id", "TaskDefinition")["StackResourceDetail"]["PhysicalResourceId"]
        details = self.aws("ecs", "describe-services", "--cluster", cluster, "--services", service)
        if details.get("failures") or not details.get("services"):
            raise ValueError("ECS service lookup failed")
        current = details["services"][0]
        active = self.aws("ecs", "describe-express-gateway-service", "--service-arn", arn)["service"].get("activeConfigurations", [])
        desired = next((x for x in active if x["taskDefinitionArn"] == expected), None)
        history = self.aws("ecs", "list-service-deployments", "--cluster", cluster, "--service", service).get("serviceDeployments", [])
        matches = [x for x in history if desired and x["targetServiceRevisionArn"] == desired["serviceRevisionArn"]]
        latest = max(matches, key=lambda x: x["createdAt"], default={})
        tasks = self.aws("ecs", "list-tasks", "--cluster", cluster, "--service-name", service, "--desired-status", "RUNNING")["taskArns"]
        running = self.aws("ecs", "describe-tasks", "--cluster", cluster, "--tasks", *tasks) if tasks else {"tasks": []}
        if running.get("failures"):
            raise ValueError("ECS running task lookup failed")
        observed = [{k: x.get(k) for k in ("taskArn", "taskDefinitionArn", "lastStatus", "healthStatus", "startedAt")} |
                    {"containers": [{k: c.get(k) for k in ("name", "image", "imageDigest", "lastStatus")} for c in x.get("containers", [])]}
                    for x in running["tasks"]]
        health = health_check(endpoint)
        revisions = current.get("currentServiceRevisions", [])
        converged = (self.stack["StackStatus"] in ("CREATE_COMPLETE", "UPDATE_COMPLETE") and
                     latest.get("status") == "SUCCESSFUL" and current.get("desiredCount") == 1 and
                     current.get("runningCount") == 1 and current.get("pendingCount") == 0 and
                     len(revisions) == 1 and desired is not None and revisions[0]["arn"] == desired["serviceRevisionArn"] and
                     len(observed) == 1 and observed[0]["lastStatus"] == "RUNNING" and observed[0]["taskDefinitionArn"] == expected)
        return {"origin": endpoint, "stack_status": self.stack["StackStatus"], "health": health,
                "expected_task_definition": expected, "requested_image_tag": next((p["ParameterValue"] for p in self.stack.get("Parameters", []) if p["ParameterKey"] == "ImageTag"), None),
                "deployment_status": latest.get("status"), "revision_verified": converged,
                "desired": current.get("desiredCount"), "running": current.get("runningCount"),
                "pending": current.get("pendingCount"), "tasks": observed,
                "note": "Health checks do not test Google sign-in or paid providers. Revision verification requires the requested successful canary."}

    def usage(self):
        days = date_range(self.args.start, self.args.end)
        selected = resolve_person(self.admin("people"), self.args.user) if self.args.user else None
        reports = [self.admin("usage", "--date", day) for day in days]
        return aggregate_usage(reports, selected)

    def logs(self):
        start, end = time_window(self.args.hours)
        group = self.outputs.get("LogGroup")
        if not group:
            raise ValueError("Stack has no LogGroup output")
        events, complete = [], True
        token = None
        for _ in range(100):
            args = ["logs", "filter-log-events", "--log-group-name", group,
                    "--start-time", str(int(start.timestamp() * 1000)), "--end-time", str(int(end.timestamp() * 1000)),
                    "--limit", str(min(1000, self.args.max_events - len(events))), "--no-paginate"]
            if token:
                args += ["--next-token", token]
            page = self.aws(*args)
            events.extend(page.get("events", []))
            next_token = page.get("nextToken")
            if not next_token:
                break
            if len(events) >= self.args.max_events or next_token == token:
                complete = False
                break
            token = next_token
        else:
            complete = False
        selected = resolve_person(self.admin("people"), self.args.user) if self.args.user else None
        parsed, skipped = parse_events(events, selected, self.args.request_id)
        result = {"start": start.isoformat(), "end": end.isoformat(), "log_group": group,
                  "complete": complete, "scanned_events": len(events), "unparsed_or_other_events": skipped,
                  "retention_note": "Logs are retained for 30 days. A bounded partial window is not a complete traffic report."}
        if self.args.mode == "performance":
            result.update(performance(parsed))
        else:
            result["events"] = parsed
        return result

    def capacity(self):
        _, cluster, service = self.service()
        start, end = time_window(self.args.hours)
        targets = [("AWS/ECS", "CPUUtilization", "Percent", {"ClusterName": cluster, "ServiceName": service}),
                   ("AWS/ECS", "MemoryUtilization", "Percent", {"ClusterName": cluster, "ServiceName": service})]
        if lb := self.outputs.get("LoadBalancerArn"):
            dimension = {"LoadBalancer": lb.split(":loadbalancer/", 1)[1]}
            targets += [("AWS/ApplicationELB", "TargetResponseTime", "Seconds", dimension),
                        ("AWS/ApplicationELB", "RequestCount", "Count", dimension),
                        ("AWS/ApplicationELB", "HTTPCode_ELB_5XX_Count", "Count", dimension),
                        ("AWS/ApplicationELB", "HTTPCode_Target_5XX_Count", "Count", dimension)]
        if table := self.outputs.get("TableName"):
            targets += [("AWS/DynamoDB", "ReadThrottleEvents", "Count", {"TableName": table}),
                        ("AWS/DynamoDB", "WriteThrottleEvents", "Count", {"TableName": table})]
        results = []
        for namespace, metric, unit, dims in targets:
            points = self.aws("cloudwatch", "get-metric-statistics", "--namespace", namespace,
                              "--metric-name", metric, "--dimensions", *[f"Name={k},Value={v}" for k, v in dims.items()],
                              "--start-time", start.isoformat(), "--end-time", end.isoformat(), "--period", "300",
                              "--statistics", *(["Sum"] if unit == "Count" else ["Average", "Maximum", "SampleCount"])).get("Datapoints", [])
            results.append({"namespace": namespace, "metric": metric, "unit": unit, "dimensions": dims,
                            "datapoints": sorted(points, key=lambda x: x["Timestamp"]),
                            "data_available": bool(points)})
        return {"start": start.isoformat(), "end": end.isoformat(), "period_seconds": 300, "metrics": results,
                "note": "Missing datapoints mean unavailable or not emitted, never zero. ALB latency includes provider time. The template adds no custom metrics, Container Insights, alarms, or pager."}


def health_check(endpoint):
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None
    try:
        with urllib.request.build_opener(NoRedirect()).open(endpoint + "/healthz", timeout=20) as response:
            return {"status": response.status, "ok": response.status == 200}
    except urllib.error.HTTPError as exc:
        return {"status": exc.code, "ok": False}
    except (urllib.error.URLError, TimeoutError):
        return {"ok": False, "error": "HTTPS health request failed"}


def date_range(start, end):
    today = dt.datetime.now(dt.timezone.utc).date()
    first = dt.date.fromisoformat(start) if start else today
    last = dt.date.fromisoformat(end) if end else first
    if first > last or last > today or (last - first).days >= 31:
        raise ValueError("Usage needs an ordered UTC date range of at most 31 days, ending today or earlier")
    return [(first + dt.timedelta(days=i)).isoformat() for i in range((last - first).days + 1)]


def time_window(hours):
    if not 0 < hours <= 24:
        raise ValueError("Choose --hours greater than zero and at most 24")
    end = dt.datetime.now(dt.timezone.utc)
    return end - dt.timedelta(hours=hours), end


def resolve_person(people, user):
    matches = [p for p in people if p["sub"] == user or p["email"].casefold() == user.casefold()]
    if len(matches) != 1:
        raise ValueError("User must match exactly one known Google subject or email; inspect auth admin people")
    return {"sub": matches[0]["sub"], "email": matches[0]["email"]}


def add_usage(total, usage, counters=COUNTERS):
    for key in counters:
        value = usage[key]
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise ValueError(f"Invalid usage counter: {key}")
        total[key] += value


def add_provider_usage(total, providers):
    for provider, usage in (providers or {}).items():
        row = total.setdefault(provider, {**dict.fromkeys(PROVIDER_COUNTERS, 0), "fenced": False})
        add_usage(row, usage, PROVIDER_COUNTERS)
        if not isinstance(usage.get("fenced", False), bool):
            raise ValueError("Invalid provider accounting fence")
        row["fenced"] = row["fenced"] or usage.get("fenced", False)


def provider_dollars(providers):
    return {provider: {**usage, "reserved_usd": ops_settings.format_usd(usage["reserved_nanodollars"]),
                       "settled_usd": ops_settings.format_usd(usage["settled_nanodollars"])} for provider, usage in providers.items()}


def aggregate_usage(reports, selected=None):
    total = dict.fromkeys(COUNTERS, 0)
    people, providers = {}, {}
    for day in reports:
        if not selected:
            add_usage(total, day["global"]["usage"])
            add_provider_usage(providers, {p: entry["usage"] for p, entry in (day.get("providers") or {}).items()})
        for person in day["people"]:
            if selected and person["sub"] != selected["sub"]:
                continue
            row = people.setdefault(person["sub"], {"sub": person["sub"], "email": person["email"],
                                                   "usage": dict.fromkeys(COUNTERS, 0), "providers": {}})
            add_usage(row["usage"], person["usage"])
            add_provider_usage(row["providers"], person.get("providers", {}))
            if selected:
                add_usage(total, person["usage"])
                add_provider_usage(providers, person.get("providers", {}))
    for person in people.values():
        person["providers"] = provider_dollars(person["providers"])
    days = [{"date": day["date"], "global": day["global"],
             "providers": {p: {**entry, "daily_limit_usd": ops_settings.format_usd(entry["daily_limit_nanodollars"])}
                           for p, entry in (day.get("providers") or {}).items()},
             "pricing_assumptions": ops_settings.pricing_assumptions(day),
             "people": [p for p in day["people"] if not selected or p["sub"] == selected["sub"]]} for day in reports]
    return {"scope": selected or "global", "total_usage": total, "people": list(people.values()), "days": days,
            "providers": provider_dollars(providers),
            "accounting_fenced": any(entry["usage"].get("fenced", False) for day in reports for entry in (day.get("providers") or {}).values()),
            "accounting_fences": sorted({p for day in reports for p, entry in (day.get("providers") or {}).items() if entry["usage"].get("fenced", False)}),
            "notes": ["UTC admission-day accounting. Requests and bytes count admitted work, not every HTTP attempt.",
                      "Reserved credits include settled credits; do not add them together. Credits are not a provider bill or currency.",
                      "Provider reserved nanodollars include settled nanodollars. USD strings are exact conservative admission accounting, not a vendor invoice.",
                      "Unknown outcomes retain full reservations. Never clear receipts to retry an ambiguous paid call.",
                      "Accounting fences are ORed across dates, not summed. Global provider/day fences remain visible in user-filtered reports.",
                      "Daily limits and pricing assumptions shown are current, including for historical dates. Multi-day usage does not imply a summed daily budget.",
                      "Missing provider dollar data means unavailable, not zero. Usage has provider breakdowns but no model-level totals."]}


def parse_events(events, selected=None, request_id=None):
    parsed, skipped = [], 0
    seen = set()
    for item in events:
        if item.get("eventId") in seen:
            continue
        if item.get("eventId"):
            seen.add(item["eventId"])
        try:
            event = json.loads(item["message"])
        except (ValueError, KeyError):
            skipped += 1
            continue
        if not isinstance(event, dict):
            skipped += 1
            continue
        operational = {"ledger maintenance failed": "teamapi.maintenance_failed",
                       "diagnostics unavailable": "teamapi.diagnostics_unavailable"}
        if not event.get("event") and event.get("msg") in operational:
            # These slog failures explain loss of serving/diagnostics. Keep the
            # known message and safe fields; errors may contain arbitrary text.
            event = {"time": event.get("time", ""), "level": event.get("level"),
                     "event": operational[event["msg"]], "attributes": {
                         "heartbeat": event.get("heartbeat") is True,
                         "error_detail_omitted": "error" in event}}
        if not str(event.get("event", "")).startswith(("teamapi.", "auth.")):
            skipped += 1
            continue
        attrs = event.get("attributes", {})
        if not isinstance(attrs, dict) or not isinstance(event.get("metrics", {}), dict):
            skipped += 1
            continue
        if (selected and attrs.get("sub") != selected["sub"]) or (request_id and attrs.get("request_id") != request_id):
            continue
        parsed.append({k: event[k] for k in ("time", "event", "level", "version", "attributes", "metrics", "truncated") if k in event})
    return sorted(parsed, key=lambda x: x.get("time", "")), skipped


def distribution(values):
    values = sorted(v for v in values if isinstance(v, (int, float)) and not isinstance(v, bool) and math.isfinite(v) and v >= 0)
    return {"samples": len(values), **({f"p{p}": values[max(0, math.ceil(len(values) * p / 100) - 1)] for p in (50, 95, 99)} if values else {})}


def performance(events):
    requests = [e for e in events if e["event"] == "teamapi.request" and "healthz" not in e.get("attributes", {}).get("route", "")]
    providers = [e for e in events if e["event"] == "teamapi.provider_request" and e.get("attributes", {}).get("route") in PAID_ROUTES]
    groups, outcomes = {}, {}
    for event in requests:
        attrs = event.get("attributes", {})
        key = (attrs.get("route", ""), attrs.get("status", 0), attrs.get("gateway_code", ""))
        groups[key] = groups.get(key, 0) + 1
    for event in providers:
        attrs = event.get("attributes", {})
        key = (attrs.get("route", ""), attrs.get("model", ""), attrs.get("upstream_status", 0), attrs.get("gateway_code", ""))
        outcomes[key] = outcomes.get(key, 0) + 1
    timed_requests = [e for e in requests if not e.get("truncated")]
    timed_providers = [e for e in providers if not e.get("truncated")]
    return {"http_requests_excluding_health": len(requests), "paid_provider_attempts": len(providers),
            "aborted_requests": sum(bool(e.get("attributes", {}).get("aborted")) for e in requests),
            "http_results": [{"route": k[0], "status": k[1], "gateway_code": k[2], "count": v} for k, v in groups.items()],
            "provider_results": [{"route": k[0], "model": k[1], "upstream_status": k[2], "gateway_code": k[3], "count": v} for k, v in outcomes.items()],
            "request_duration_ms": distribution(e.get("metrics", {}).get("duration_ms") for e in timed_requests),
            "authentication_ms": distribution(e.get("metrics", {}).get("authentication_ms") for e in timed_requests),
            "provider_duration_ms": {phase: distribution(e.get("metrics", {}).get("duration_ms", {}).get(phase) for e in timed_providers)
                                     for phase in ("validation", "admission_wait", "reservation", "upstream", "settlement", "total")},
            "truncated_events": sum(bool(e.get("truncated")) for e in events),
            "note": "Nearest-rank percentiles in milliseconds. Missing and truncated measurements are excluded, not zero. HTTP and provider events describe overlapping work; do not add their counts."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--account", required=True, help="expected 12-digit AWS account")
    parser.add_argument("--region", required=True)
    parser.add_argument("--stack", required=True)
    parser.add_argument("--rzm", type=Path, default=ROOT / "scripts/rzm", help="rzm executable or project launcher")
    sub = parser.add_subparsers(dest="mode", required=True)
    ops_settings.add_parsers(sub)
    sub.add_parser("status")
    usage = sub.add_parser("usage")
    usage.add_argument("--start", help="first UTC date YYYY-MM-DD, default today")
    usage.add_argument("--end", help="last UTC date inclusive, default start")
    usage.add_argument("--user", help="exact Google subject or email")
    for mode in ("logs", "performance", "capacity"):
        child = sub.add_parser(mode)
        child.add_argument("--hours", type=float, default=1)
        if mode != "capacity":
            child.add_argument("--max-events", type=int, default=5000)
            child.add_argument("--user", help="exact Google subject or email")
            child.add_argument("--request-id")
    args = parser.parse_args()
    try:
        if not re.fullmatch(r"\d{12}", args.account) or not re.fullmatch(r"[A-Za-z][A-Za-z0-9-]{0,127}", args.stack):
            raise ValueError("Use a 12-digit account and CloudFormation stack name")
        if hasattr(args, "hours"):
            time_window(args.hours)
        if hasattr(args, "max_events") and not 1 <= args.max_events <= 10000:
            raise ValueError("--max-events must be between 1 and 10000")
        if args.mode == "usage":
            date_range(args.start, args.end)
        ops = Ops(args)
        if args.mode in ("settings", "settings-update"):
            result = ops_settings.run(ops, args)
        else:
            result = ops.logs() if args.mode in ("logs", "performance") else getattr(ops, args.mode)()
        print(json.dumps({"identity": ops.identity, **result}, indent=2))
    except (ValueError, KeyError, IndexError, subprocess.TimeoutExpired) as exc:
        print(json.dumps({"error": str(exc), "hint": "Check AWS login/account/region/stack and read permissions. Usage, settings, and user-filtered logs also require an existing admin rzm login."}), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
