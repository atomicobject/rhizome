"""Native-isolation Codex adapter for the persistent code-mode campaign."""

from __future__ import annotations

from pathlib import Path
import subprocess
from typing import Any

import clean_codex_adapter


class Adapter(clean_codex_adapter.Adapter):
    source_files = (
        "persistent_adapter.py", "persistent_environment.py", "persistent_campaign.py",
        "clean_codex_adapter.py", "adapter_support.py", "codex_adapter.py",
    )

    def preflight(self, manifest: dict, run: dict) -> dict[str, Any]:
        result = super().preflight(manifest, run)
        if not result["ready"]:
            return result
        try:
            repo = Path(run["repo"])
            env = self.environment(manifest, run)
            method_probe = "rzm agent --help"
            if run["method"] == "persistent":
                method_probe = "rzm agent code --help >/dev/null && rzm agent code serve --help"
            script = (
                'test "$(command -v rzm)" = "$1" && '
                'python3 -c "import sys; assert sys.version_info >= (3, 11)" && '
                'node -e "if (Number(process.versions.node.split(\'.\')[0]) < 20) process.exit(1)" && '
                + method_probe
            )
            process = subprocess.run(
                self._prefix(manifest, run) + [
                    "sandbox", "-P", "evaluation", "--include-managed-config", "-C", str(repo),
                    "--", "/bin/sh", "-c", script, "persistent-tool-probe", str(repo / "scripts/rzm"),
                ],
                cwd=repo, env=env, capture_output=True, text=True, timeout=60,
            )
            if process.returncode:
                raise ValueError("method tool probe failed: " + process.stderr[-1000:])
            result["toolchain"] = {
                "passed": True,
                "method": run["method"],
                "python": subprocess.check_output(["/opt/homebrew/bin/python3", "--version"], text=True).strip(),
                "node": subprocess.check_output(["/opt/homebrew/bin/node", "--version"], text=True).strip(),
                "code_mode_serve_checked": run["method"] == "persistent",
            }
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            result["ready"] = False
            result["errors"].append(str(error))
        return result
