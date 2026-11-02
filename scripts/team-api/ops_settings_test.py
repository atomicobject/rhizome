#!/usr/bin/env python3
import argparse
import copy
import unittest
from unittest.mock import Mock

import ops_settings


CONFIG = {"paid_calls_enabled": False, "voyage_enabled": False, "typesafe_enabled": False,
          "person_limits": {"requests": 0, "input_bytes": 0, "charge_credits": 0},
          "global_limits": {"requests": 0, "input_bytes": 0, "charge_credits": 0},
          "eligibility_mode": "allowlist", "allowlist": ["synthetic@example.com"], "unrelated_secret": "must-not-be-reported"}


def args(*argv):
    parser = argparse.ArgumentParser()
    ops_settings.add_parsers(parser.add_subparsers(dest="mode", required=True))
    return parser.parse_args(argv)


class SettingsTests(unittest.TestCase):
    def test_read_returns_only_operator_settings(self):
        client = Mock()
        client.admin.return_value = CONFIG
        result = ops_settings.run(client, args("settings"))
        self.assertEqual(result["settings"]["allowlist_count"], 1)
        self.assertNotIn("unrelated_secret", result["settings"])
        self.assertNotIn("allowlist", result["settings"])
        client.admin.assert_called_once_with("config")

    def test_default_is_preview_with_only_requested_differences(self):
        client = Mock()
        client.admin.return_value = CONFIG
        result = ops_settings.run(client, args("settings-update", "--paid-calls", "on", "--voyage", "off", "--person-limits", "10,1000,2000"))
        self.assertFalse(result["applied"])
        self.assertEqual(result["changes"], {"paid_calls_enabled": True, "person_limits": {"requests": 10, "input_bytes": 1000, "charge_credits": 2000}})
        self.assertEqual(result["admin_arguments"], ["config", "--paid-calls", "on", "--person-limits", "10,1000,2000"])
        client.admin.assert_called_once_with("config")

    def test_apply_patches_once_preserves_unrequested_fields_and_verifies_audit(self):
        after = copy.deepcopy(CONFIG)
        after["voyage_enabled"] = True
        audit = {"at": "2026-11-03T01:02:03Z", "actor": "synthetic@example.com", "action": "config.update", "target": "config", "detail": "voyage_enabled=true"}
        client = Mock()
        client.admin.side_effect = [CONFIG, [], after, after, [audit]]
        result = ops_settings.run(client, args("settings-update", "--voyage", "on", "--apply"))
        self.assertTrue(result["applied"])
        self.assertEqual(result["audit"], audit)
        self.assertFalse(result["settings"]["paid_calls_enabled"])
        self.assertFalse(result["settings"]["typesafe_enabled"])
        self.assertEqual([call.args for call in client.admin.call_args_list], [
            ("config",), ("audit", "--limit", "100"), ("config", "--voyage", "on"), ("config",), ("audit", "--limit", "100")])

    def test_noop_apply_makes_no_mutation(self):
        client = Mock()
        client.admin.return_value = CONFIG
        result = ops_settings.run(client, args("settings-update", "--paid-calls", "off", "--apply"))
        self.assertFalse(result["applied"])
        self.assertEqual(result["changes"], {})
        client.admin.assert_called_once_with("config")

    def test_lost_patch_response_is_verified_without_retry(self):
        after = {**CONFIG, "paid_calls_enabled": True}
        audit = {"at": "new", "action": "config.update", "target": "config", "detail": "paid_calls_enabled=true"}
        client = Mock()
        client.admin.side_effect = [CONFIG, [], ValueError("connection closed"), after, [audit]]
        result = ops_settings.run(client, args("settings-update", "--paid-calls", "on", "--apply"))
        self.assertTrue(result["applied"])
        self.assertFalse(result["mutation_response_received"])
        self.assertEqual(sum(call.args == ("config", "--paid-calls", "on") for call in client.admin.call_args_list), 1)

    def test_old_matching_audit_or_wrong_readback_cannot_confirm_apply(self):
        after = {**CONFIG, "paid_calls_enabled": True}
        old = {"at": "old", "action": "config.update", "target": "config", "detail": "paid_calls_enabled=true"}
        for readback, baseline, audit in [(after, [old], [old]), (CONFIG, [], [old]), (after, [], [])]:
            client = Mock()
            client.admin.side_effect = [CONFIG, baseline, after, readback, audit]
            with self.assertRaisesRegex(ValueError, "Do not retry blindly"):
                ops_settings.run(client, args("settings-update", "--paid-calls", "on", "--apply"))
            self.assertEqual(sum(call.args == ("config", "--paid-calls", "on") for call in client.admin.call_args_list), 1)

    def test_invalid_limits_and_empty_patch_fail_before_mutation(self):
        for value in ["1,2", "-1,2,3", "1,2,no", "1,2,9223372036854775808"]:
            with self.assertRaises(argparse.ArgumentTypeError):
                ops_settings.parse_limits(value)
        self.assertEqual(ops_settings.parse_limits("0, 2,3"), {"requests": 0, "input_bytes": 2, "charge_credits": 3})
        client = Mock()
        client.admin.return_value = CONFIG
        with self.assertRaisesRegex(ValueError, "Choose at least one"):
            ops_settings.run(client, args("settings-update", "--apply"))
        client.admin.assert_called_once_with("config")

    def test_audit_detail_matches_gateway_sorting_and_limit_syntax(self):
        self.assertEqual(ops_settings.audit_detail({"voyage_enabled": True, "paid_calls_enabled": False,
                                                   "global_limits": {"requests": 9, "input_bytes": 10, "charge_credits": 11}}),
                         "global_limits=requests:9,input_bytes:10,charge_credits:11 paid_calls_enabled=false voyage_enabled=true")


if __name__ == "__main__":
    unittest.main()
