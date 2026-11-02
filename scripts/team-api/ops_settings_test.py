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
    def test_provider_dollars_are_exact_and_reject_non_decimal_or_overflow(self):
        for text, expected in [("2.50", 2500000000), ("0.000000001", 1), ("0", 0),
                               ("9223372036.854775807", 9223372036854775807)]:
            self.assertEqual(ops_settings.parse_usd(text), expected)
            self.assertEqual(ops_settings.parse_usd(ops_settings.format_usd(expected)), expected)
        for text in ["-1", "1e2", "NaN", "Infinity", "1.0000000001", "9223372036.854775808", "1_000", "1,25"]:
            with self.assertRaises(argparse.ArgumentTypeError):
                ops_settings.parse_usd(text)

    def test_provider_preview_includes_only_changed_nested_keys_and_exact_usd(self):
        config = {**CONFIG, "provider_daily_nanodollars": {"voyage": 0, "typesafe": 1250000000},
                  "pricing_assumptions": {"typesafe": {"provider": "typesafe", "model": "jev-latest", "version": "reviewed-v1",
                                                       "nanodollars_per_token": 7, "unrelated_secret": "omit"}}}
        client = Mock()
        client.admin.return_value = config
        result = ops_settings.run(client, args("settings-update", "--voyage-daily-usd", "2.50", "--typesafe-daily-usd", "1.25"))
        self.assertEqual(result["changes"], {"provider_daily_nanodollars": {"voyage": 2500000000}})
        self.assertEqual(result["changes_provider_daily_usd"], {"voyage": "2.5"})
        self.assertEqual(result["admin_arguments"], ["config", "--voyage-daily-usd", "2.5"])
        self.assertEqual(result["current"]["provider_daily_usd"], {"voyage": "0", "typesafe": "1.25"})
        self.assertEqual(result["current"]["pricing_assumptions"]["typesafe"],
                         {"provider": "typesafe", "model": "jev-latest", "version": "reviewed-v1", "nanodollars_per_token": 7})
        client.admin.assert_called_once_with("config")

    def test_provider_apply_verifies_requested_key_while_preserving_other_provider(self):
        before = {**CONFIG, "provider_daily_nanodollars": {"voyage": 0, "typesafe": 1250000000}}
        after = {**CONFIG, "provider_daily_nanodollars": {"voyage": 2500000000, "typesafe": 1750000000}}
        audit = {"at": "new", "action": "config.update", "target": "config", "detail": "provider_daily_nanodollars.voyage=2500000000"}
        client = Mock()
        client.admin.side_effect = [before, [], after, after, [audit]]
        result = ops_settings.run(client, args("settings-update", "--voyage-daily-usd", "2.50", "--apply"))
        self.assertTrue(result["applied"])
        self.assertEqual(result["settings"]["provider_daily_nanodollars"], {"voyage": 2500000000, "typesafe": 1750000000})
        self.assertEqual(client.admin.call_args_list[2].args, ("config", "--voyage-daily-usd", "2.5"))

    def test_provider_wrong_readback_or_other_provider_audit_is_unverified(self):
        before = {**CONFIG, "provider_daily_nanodollars": {"voyage": 0, "typesafe": 0}}
        desired = {**CONFIG, "provider_daily_nanodollars": {"voyage": 1, "typesafe": 0}}
        for readback, detail in [(before, "provider_daily_nanodollars.voyage=1"),
                                 (desired, "provider_daily_nanodollars.typesafe=1")]:
            client = Mock()
            client.admin.side_effect = [before, [], desired, readback,
                                       [{"at": "new", "action": "config.update", "target": "config", "detail": detail}]]
            with self.assertRaisesRegex(ValueError, "Do not retry blindly"):
                ops_settings.run(client, args("settings-update", "--voyage-daily-usd", "0.000000001", "--apply"))

    def test_old_gateway_cannot_preview_dollar_changes_as_supported(self):
        client = Mock()
        client.admin.return_value = CONFIG
        with self.assertRaisesRegex(ValueError, "deploy a compatible gateway"):
            ops_settings.run(client, args("settings-update", "--voyage-daily-usd", "2.5"))
        client.admin.assert_called_once_with("config")
        client.admin.return_value = {**CONFIG, "provider_daily_nanodollars": None}
        preview = ops_settings.run(client, args("settings-update", "--voyage-daily-usd", "2.5"))
        self.assertEqual(preview["changes"], {"provider_daily_nanodollars": {"voyage": 2500000000}})

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
