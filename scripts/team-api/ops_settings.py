"""Preview and verify bounded runtime settings through the existing admin CLI."""
import argparse
import json

SWITCHES = {"paid_calls": "paid_calls_enabled", "voyage": "voyage_enabled", "typesafe": "typesafe_enabled"}
LIMITS = ("requests", "input_bytes", "charge_credits")


def parse_limits(value):
    parts = value.split(",")
    try:
        if len(parts) != 3:
            raise ValueError()
        numbers = [int(part.strip()) for part in parts]
        if any(n < 0 or n > 9223372036854775807 for n in numbers):
            raise ValueError()
    except ValueError as exc:
        raise argparse.ArgumentTypeError("Use three non-negative int64 values: requests,input_bytes,charge_credits") from exc
    return dict(zip(LIMITS, numbers))


def add_parsers(subparsers):
    subparsers.add_parser("settings", help="read current paid-call switches and daily limits")
    update = subparsers.add_parser("settings-update", help="preview a runtime change; --apply writes and verifies it")
    for flag in SWITCHES:
        update.add_argument("--" + flag.replace("_", "-"), choices=("on", "off"))
    for flag in ("person_limits", "global_limits"):
        update.add_argument("--" + flag.replace("_", "-"), type=parse_limits,
                            help="requests,input_bytes,charge_credits, bounded by startup ceilings")
    update.add_argument("--apply", action="store_true", help="apply the exact previewed runtime patch once")


def read_settings(config):
    result = {}
    for name in SWITCHES.values():
        if not isinstance(config.get(name), bool):
            raise ValueError(f"Runtime configuration is missing a valid {name}")
        result[name] = config[name]
    for name in ("person_limits", "global_limits"):
        limits = config.get(name, {})
        if any(isinstance(limits.get(k), bool) or not isinstance(limits.get(k), int) or limits[k] < 0 for k in LIMITS):
            raise ValueError(f"Runtime configuration is missing valid {name}")
        result[name] = {k: limits[k] for k in LIMITS}
    if "eligibility_mode" in config:
        result["eligibility_mode"] = config["eligibility_mode"]
    if isinstance(config.get("allowlist"), list):
        result["allowlist_count"] = len(config["allowlist"])
    return result


def requested_changes(args, current):
    requested = {}
    for flag, name in SWITCHES.items():
        if (value := getattr(args, flag, None)) is not None:
            requested[name] = value == "on"
    for name in ("person_limits", "global_limits"):
        if (value := getattr(args, name, None)) is not None:
            requested[name] = value
    if not requested:
        raise ValueError("Choose at least one settings-update flag; use settings for a read-only report")
    return {k: v for k, v in requested.items() if v != current[k]}


def config_arguments(changes):
    args = ["config"]
    reverse_switches = {name: "--" + flag.replace("_", "-") for flag, name in SWITCHES.items()}
    for name, value in changes.items():
        if name in reverse_switches:
            args += [reverse_switches[name], "on" if value else "off"]
        else:
            args += ["--" + name.replace("_", "-"), ",".join(str(value[k]) for k in LIMITS)]
    return args


def audit_detail(changes):
    parts = []
    for name, value in changes.items():
        text = str(value).lower() if isinstance(value, bool) else ",".join(f"{key}:{value[key]}" for key in LIMITS)
        parts.append(f"{name}={text}")
    return " ".join(sorted(parts))


def fingerprint(entry):
    return json.dumps(entry, sort_keys=True)


def run(client, args):
    current = read_settings(client.admin("config"))
    if args.mode == "settings":
        return {"settings": current, "note": "Startup ceilings are not exposed by this runtime API."}
    changes = requested_changes(args, current)
    result = {"mode": "apply" if args.apply else "preview", "applied": False,
              "current": current, "changes": changes, "admin_arguments": config_arguments(changes)}
    if not changes or not args.apply:
        result["note"] = "No settings were changed. Pass --apply to apply requested differences." if changes else "Requested settings already match; no update is needed."
        return result
    baseline = {fingerprint(e) for e in client.admin("audit", "--limit", "100")}
    response_received = True
    try:
        client.admin(*config_arguments(changes))
    except Exception:
        # A lost response can follow a committed patch. Verify without resending.
        response_received = False
    try:
        after = read_settings(client.admin("config"))
        entries = client.admin("audit", "--limit", "100")
        matching = next((e for e in entries if e.get("action") == "config.update" and e.get("target") == "config"
                         and e.get("detail") == audit_detail(changes) and fingerprint(e) not in baseline), None)
        if any(after[name] != value for name, value in changes.items()) or matching is None:
            raise ValueError("Requested settings or a new matching audit entry could not be verified")
    except Exception as exc:
        raise ValueError("Settings apply outcome is unverified. Do not retry blindly. Read settings and auth admin audit before another update.") from exc
    return {**result, "applied": True, "settings": after,
            "mutation_response_received": response_received,
            "audit": {k: matching[k] for k in ("at", "actor", "action", "target", "detail") if k in matching},
            "note": "Requested fields match runtime readback and a new config.update audit entry. Switch caching may take up to 15 seconds."}
