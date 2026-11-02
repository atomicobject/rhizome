#!/usr/bin/env python3
"""Synthetic operator reports. No credentials, network, or AWS resources needed."""
import argparse
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("ops", Path(__file__).with_name("ops.py"))
ops = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ops)


def usage(requests=0, credits=0, unknown=0):
    return {"requests": requests, "input_bytes": requests * 10, "reserved_credits": credits,
            "settled_credits": 3, "unknown_outcomes": unknown}


def event(name, attrs, metrics, identifier="one"):
    return {"eventId": identifier, "message": json.dumps({"event": name, "time": "2026-01-01T01:00:00Z",
                                                         "attributes": attrs, "metrics": metrics})}


class UsageTests(unittest.TestCase):
    def test_daily_aggregation_keeps_credits_distinct_and_exact(self):
        reports = [
            {"date": "2026-01-01", "global": {"usage": usage(2, 9007199254740993), "limits": {"requests": 10}},
             "people": [{"sub": "s1", "email": "member@example.com", "usage": usage(1, 7, 1), "limits": {"requests": 10}}]},
            {"date": "2026-01-02", "global": {"usage": usage(4, 10), "limits": {"requests": 20}},
             "people": [{"sub": "s1", "email": "member@example.com", "usage": usage(3, 5), "limits": {"requests": 20}}]},
        ]
        result = ops.aggregate_usage(reports)
        self.assertEqual(result["total_usage"], {"requests": 6, "input_bytes": 60, "reserved_credits": 9007199254741003,
                                                 "settled_credits": 6, "unknown_outcomes": 0})
        self.assertEqual(result["people"][0]["usage"]["reserved_credits"], 12)
        selected = ops.aggregate_usage(reports, {"sub": "s1", "email": "member@example.com"})
        self.assertEqual(selected["total_usage"], {"requests": 4, "input_bytes": 40, "reserved_credits": 12,
                                                  "settled_credits": 6, "unknown_outcomes": 1})
        self.assertEqual(selected["days"][0]["people"][0]["limits"], {"requests": 10})

    def test_range_bounds_and_identity(self):
        self.assertEqual(ops.date_range("2026-01-30", "2026-02-01"), ["2026-01-30", "2026-01-31", "2026-02-01"])
        for first, last in [("2026-01-01", "2026-02-01"), ("2026-02-02", "2026-02-01"), ("2999-01-01", None)]:
            with self.assertRaises(ValueError):
                ops.date_range(first, last)
        people = [{"sub": "s1", "email": "Member@example.com"}]
        self.assertEqual(ops.resolve_person(people, "member@example.com")["sub"], "s1")
        with self.assertRaises(ValueError):
            ops.resolve_person(people, "member")


class LogTests(unittest.TestCase):
    def test_truncated_measurements_are_not_used_for_latency(self):
        result = ops.performance([{"event": "teamapi.request", "attributes": {"route": "/v1/meta"},
                                   "metrics": {"duration_ms": 9999}, "truncated": True}])
        self.assertEqual(result["http_requests_excluding_health"], 1)
        self.assertEqual(result["request_duration_ms"], {"samples": 0})
        self.assertEqual(result["truncated_events"], 1)

    def test_gateway_maintenance_failure_is_visible_without_raw_error(self):
        records = [{"eventId": "maintenance", "message": json.dumps({
            "time": "2026-01-01T01:00:00Z", "level": "ERROR", "msg": "ledger maintenance failed",
            "heartbeat": True, "error": "arbitrary private error text"})}]
        parsed, skipped = ops.parse_events(records)
        self.assertEqual(skipped, 0)
        self.assertEqual(parsed, [{"time": "2026-01-01T01:00:00Z", "event": "teamapi.maintenance_failed",
                                  "level": "ERROR", "attributes": {"heartbeat": True, "error_detail_omitted": True}}])
        self.assertNotIn("arbitrary private error text", json.dumps(parsed))

    def test_nested_timings_exclude_health_free_routes_and_duplicates(self):
        records = [
            event("teamapi.request", {"route": "GET /healthz", "status": 200}, {"duration_ms": 9999}, "h"),
            event("teamapi.request", {"route": "/v1/providers/voyage/v1/embeddings", "sub": "s1", "status": 200}, {"duration_ms": 120, "authentication_ms": 4}, "r"),
            event("teamapi.provider_request", {"route": "/v1/providers/voyage/v1/embeddings", "sub": "s1"}, {"duration_ms": {"upstream": 100, "total": 116, "reservation": 8}}, "p"),
            event("teamapi.provider_request", {"route": "/v1/providers/typesafe/v1/models", "sub": "s1"}, {"duration_ms": {"total": 9999}}, "f"),
            {"eventId": "broken", "message": "not JSON"},
        ]
        parsed, skipped = ops.parse_events(records + [records[1]])
        result = ops.performance(parsed)
        self.assertEqual(skipped, 1)
        self.assertEqual(result["http_requests_excluding_health"], 1)
        self.assertEqual(result["paid_provider_attempts"], 1)
        self.assertEqual(result["request_duration_ms"], {"samples": 1, "p50": 120, "p95": 120, "p99": 120})
        self.assertEqual(result["provider_duration_ms"]["upstream"], {"samples": 1, "p50": 100, "p95": 100, "p99": 100})
        self.assertEqual(result["provider_duration_ms"]["settlement"], {"samples": 0})

    def test_user_and_request_filters_apply_to_nested_attributes(self):
        records = [event("teamapi.request", {"sub": "s1", "request_id": "r1"}, {}, "1"),
                   event("teamapi.request", {"sub": "s2", "request_id": "r1"}, {}, "2"),
                   event("teamapi.request", {"sub": "s1", "request_id": "r2"}, {}, "3")]
        result, _ = ops.parse_events(records, {"sub": "s1"}, "r1")
        self.assertEqual([x["attributes"] for x in result], [{"sub": "s1", "request_id": "r1"}])

    def report(self, pages, cap=5):
        client = object.__new__(ops.Ops)
        client.args = argparse.Namespace(hours=1, max_events=cap, user=None, request_id=None, mode="logs")
        client.outputs = {"LogGroup": "synthetic-log-group"}
        with patch.object(client, "aws", side_effect=pages) as aws:
            result = client.logs()
        return result, aws

    def test_empty_paginated_pages_are_followed(self):
        result, aws = self.report([{"events": [], "nextToken": "a"}, {"events": [event("auth.signin", {}, {})]}])
        self.assertTrue(result["complete"])
        self.assertEqual(len(result["events"]), 1)
        self.assertIn("a", aws.call_args_list[1].args)

    def test_bound_is_reported_as_partial(self):
        result, aws = self.report([{"events": [event("auth.signin", {}, {})], "nextToken": "a"}], cap=1)
        self.assertFalse(result["complete"])
        self.assertEqual(aws.call_count, 1)


class BoundaryTests(unittest.TestCase):
    def test_missing_admin_session_reports_json_error_code(self):
        result = subprocess.CompletedProcess(["rzm"], 1, stdout=json.dumps({
            "error": {"code": "sign_in_required", "message": "private diagnostic"}}), stderr="")
        with patch.object(ops.subprocess, "run", return_value=result):
            with self.assertRaisesRegex(ValueError, "sign_in_required") as error:
                ops.command(["rzm", "auth", "admin", "usage"])
        self.assertNotIn("private diagnostic", str(error.exception))

    def test_status_requires_requested_successful_revision_and_running_task(self):
        client = object.__new__(ops.Ops)
        client.args = argparse.Namespace(stack="synthetic")
        client.outputs = {"ServiceArn": "arn:aws:ecs:us-east-2:123456789012:service/default/synthetic", "Endpoint": "example.com"}
        client.stack = {"StackStatus": "UPDATE_COMPLETE", "Parameters": [{"ParameterKey": "ImageTag", "ParameterValue": "abc"}]}
        def report(revision="rev-new", deployment="SUCCESSFUL", task_status="RUNNING"):
            pages = [
                {"StackResourceDetail": {"PhysicalResourceId": "task:new"}},
                {"services": [{"desiredCount": 1, "runningCount": 1, "pendingCount": 0, "currentServiceRevisions": [{"arn": revision}]}]},
                {"service": {"activeConfigurations": [{"taskDefinitionArn": "task:new", "serviceRevisionArn": "rev-new"}]}},
                {"serviceDeployments": [{"targetServiceRevisionArn": "rev-new", "createdAt": "2026-01-01", "status": deployment}]},
                {"taskArns": ["task-one"]},
                {"tasks": [{"taskArn": "task-one", "taskDefinitionArn": "task:new", "lastStatus": task_status,
                            "containers": [{"name": "gateway", "image": "synthetic:abc", "imageDigest": "sha256:synthetic"}]}]},
            ]
            with patch.object(client, "aws", side_effect=pages) as aws, patch.object(ops, "health_check", return_value={"status": 200, "ok": True}):
                result = client.status()
            self.assertEqual([call.args[:2] for call in aws.call_args_list], [
                ("cloudformation", "describe-stack-resource"), ("ecs", "describe-services"),
                ("ecs", "describe-express-gateway-service"), ("ecs", "list-service-deployments"),
                ("ecs", "list-tasks"), ("ecs", "describe-tasks")])
            return result
        self.assertTrue(report()["revision_verified"])
        self.assertFalse(report(revision="rev-old")["revision_verified"])
        self.assertFalse(report(deployment="ROLLBACK_SUCCESSFUL")["revision_verified"])
        self.assertFalse(report(task_status="PENDING")["revision_verified"])

    def test_capacity_does_not_turn_missing_metrics_into_zero(self):
        client = object.__new__(ops.Ops)
        client.args = argparse.Namespace(hours=1)
        client.outputs = {"ServiceArn": "arn:aws:ecs:us-east-2:123456789012:service/default/synthetic", "TableName": "synthetic-table"}
        with patch.object(client, "aws", return_value={"Datapoints": []}) as aws:
            result = client.capacity()
        self.assertEqual(len(result["metrics"]), 4)
        self.assertTrue(all(not metric["data_available"] and metric["datapoints"] == [] for metric in result["metrics"]))
        self.assertEqual(aws.call_args_list[0].args[-3:], ("Average", "Maximum", "SampleCount"))
        self.assertEqual(aws.call_args_list[-1].args[-2:], ("--statistics", "Sum"))

    def test_wrong_account_stops_before_stack_or_gateway_reads(self):
        args = argparse.Namespace(account="123456789012", region="us-east-2", stack="synthetic")
        with patch.object(ops, "command", return_value={"Account": "999999999999", "Arn": "synthetic"}) as command:
            with self.assertRaisesRegex(ValueError, "expected 123456789012"):
                ops.Ops(args)
        self.assertEqual(command.call_count, 1)
        self.assertEqual(command.call_args.args[0][1:3], ["sts", "get-caller-identity"])

    def test_cli_invalid_bounds_do_not_contact_aws(self):
        argv = ["ops.py", "--account", "123456789012", "--region", "us-east-2", "--stack", "synthetic", "logs", "--hours", "25"]
        with patch.object(ops.sys, "argv", argv), patch.object(ops, "command") as command, patch.object(ops.sys, "stderr", io.StringIO()):
            self.assertEqual(ops.main(), 1)
        command.assert_not_called()

    def test_endpoint_is_https_origin(self):
        self.assertEqual(ops.origin("synthetic.example.com"), "https://synthetic.example.com")
        for value in ["", "http://synthetic.example.com", "https://person:password@example.com", "https://example.com/api", "https://example.com?key=x"]:
            with self.assertRaises(ValueError):
                ops.origin(value)


if __name__ == "__main__":
    unittest.main()
