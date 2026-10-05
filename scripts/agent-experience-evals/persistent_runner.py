#!/usr/bin/env python3
"""Thin entry point for the separate six-launch persistent code-mode campaign."""

from __future__ import annotations

import runner
import persistent_adapter
import persistent_campaign
import persistent_environment


def _adapter(overlay=None, overlay_hash=None):
    if overlay is None or overlay_hash is None:
        raise persistent_campaign.RunnerError("persistent campaign requires --environment")
    return persistent_adapter.Adapter(overlay, overlay_hash)


runner.load_manifest = persistent_campaign.load_manifest
runner.validate_manifest = persistent_campaign.validate_manifest
runner.selected_runs = persistent_campaign.selected_runs
runner.adapter_module = _adapter
runner.environment_overlay = persistent_environment


if __name__ == "__main__":
    raise SystemExit(runner.main())
