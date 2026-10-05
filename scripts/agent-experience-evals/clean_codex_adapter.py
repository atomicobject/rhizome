"""Clean-home Codex adapter for the three-call pilot continuation."""

from __future__ import annotations

import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from typing import Any

from adapter_support import sha256, validate_run_sources
from codex_adapter import DISABLED_FEATURES, validate_prompt

RUNTIME_READS = ("/opt/homebrew", "/Library/Developer/CommandLineTools")
ACCESS_PROBE_SCRIPT = (
    "import os,pathlib,sys\nfixture,binary,sentinel,meta1,meta2,*targets=sys.argv[1:]\n"
    "with open(fixture,'rb') as f: assert f.read(1)\n"
    "with open(binary,'rb') as f: assert f.read(1)\n"
    "with open(fixture,'ab'): pass\n"
    "fail=[]\nfor target in targets:\n kind,raw=target.split(':',1)\n try:\n"
    "  with open(raw,'rb') as f: f.read(1)\n"
    "  if kind=='d': os.listdir(raw)\n"
    "  fail.append('read:'+raw)\n except PermissionError: pass\n"
    " except OSError as error: fail.append('error:'+raw+':'+type(error).__name__)\n"
    "for raw in (meta1,meta2):\n try:\n  with open(raw,'ab'): pass\n  fail.append('write:'+raw)\n"
    " except PermissionError: pass\n except OSError as error: fail.append('error:'+raw+':'+type(error).__name__)\n"
    "try: pathlib.Path(sentinel).write_text('changed'); fail.append('write:'+sentinel)\n"
    "except PermissionError: pass\nexcept OSError as error: fail.append('error:'+sentinel+':'+type(error).__name__)\n"
    "print(fail); sys.exit(bool(fail))"
)


def probe_targets(paths: list[Path]) -> list[str]:
    """Classify paths before sandbox metadata becomes intentionally unavailable."""
    return [("f:" if path.is_file() else "d:") + str(path)
            for path in paths if path.is_file() or path.is_dir()]


def _encoded(value: Any) -> str:
    if isinstance(value, dict):
        return "{" + ",".join(json.dumps(key) + "=" + json.dumps(item)
                              for key, item in value.items()) + "}"
    return json.dumps(value)


class Adapter:
    source_files = ("clean_codex_adapter.py", "environment_overlay.py", "adapter_support.py", "codex_adapter.py")

    def __init__(self, overlay: dict[str, Any], overlay_hash: str):
        self.overlay = overlay
        self.overlay_hash = overlay_hash

    def entry(self, run: dict) -> dict[str, Any]:
        try:
            return self.overlay["runs"][run["id"]]
        except KeyError as error:
            raise ValueError("run is outside the clean-home continuation") from error

    def effective_run(self, run: dict) -> dict[str, Any]:
        effective = dict(run)
        effective["original_repo"] = run["repo"]
        effective["repo"] = self.entry(run)["execution_repo"]
        return effective

    def environment(self, manifest: dict, run: dict) -> dict[str, str]:
        entry = self.entry(run)
        keep = ("USER", "LOGNAME", "LANG", "LC_ALL", "TZ", "TMPDIR")
        env = {key: os.environ[key] for key in keep if key in os.environ}
        env.update({
            "HOME": entry["clean_home"], "CODEX_HOME": entry["codex_home"],
            "PATH": os.pathsep.join((str(Path(run["repo"]) / "scripts"), "/opt/homebrew/bin",
                                     "/Library/Developer/CommandLineTools/usr/bin", "/usr/bin", "/bin")),
            "RZM_SKIP_REPO_DELEGATE": "1",
            "PYTHONDONTWRITEBYTECODE": "1", "NO_COLOR": "1",
        })
        return env

    def protected_paths(self, manifest: dict, run: dict, result_root: Path | None = None) -> list[Path]:
        entry = self.entry(run)
        codex_home = Path(entry["codex_home"])
        paths = [Path(item) for item in self.overlay.get("protected_paths", [])]
        paths += [Path(item) for item in entry["protected_paths"]]
        paths += [Path(item) for item in run["isolation"].get("denied_roots", [])]
        paths += [Path(item) for item in run["isolation"].get("denied_files", [])]
        paths += [Path(other["repo"]) for other in manifest["runs"]]
        paths += [Path(self.overlay["support_root"]), codex_home / "auth.json", codex_home / "config.toml",
                  codex_home / "isolation-sentinel"]
        paths += [codex_home / name for name in ("skills", "logs", "state", "sessions", "sqlite")]
        paths += [Path(other["clean_home"]) for run_id, other in self.overlay["runs"].items()
                  if run_id != run["id"]]
        if result_root is not None:
            paths.append(result_root)
        repo = Path(run["repo"]).resolve()
        unique = []
        for path in paths:
            resolved = path.resolve()
            if repo == resolved or repo.is_relative_to(resolved):
                raise ValueError("protected path overlaps selected fixture")
            if resolved not in unique:
                unique.append(resolved)
        return unique

    def _prefix(self, manifest: dict, run: dict, result_root: Path | None = None) -> list[str]:
        entry = self.entry(run)
        codex_home = Path(entry["codex_home"])
        codex = Path(run["isolation"]["codex"])
        result_root = Path(self.overlay["output_root"]) if result_root is None else result_root
        filesystem = {":minimal": "read", str(Path(run["repo"]).resolve()): "write",
                      str(Path(run["binary"]).resolve()): "read"}
        filesystem.update({path: "read" for path in RUNTIME_READS})
        filesystem.update({str(path) + "{,/**}": "deny"
                           for path in self.protected_paths(manifest, run, result_root)})
        env = self.environment(manifest, run)
        config = {
            "model": "gpt-5.6-luna", "model_reasoning_effort": "xhigh", "model_provider": "openai",
            "default_permissions": "evaluation", "approval_policy": "never",
            "forced_login_method": "chatgpt", "cli_auth_credentials_store": "file",
            "developer_instructions": "", "web_search": "disabled", "allow_login_shell": False,
            "shell_environment_policy.inherit": "none", "shell_environment_policy.set": env,
            "permissions.evaluation.workspace_roots": {str(Path(run["repo"]).resolve()): True},
            "permissions.evaluation.filesystem": filesystem,
            "permissions.evaluation.network.enabled": False,
        }
        prefix = [str(codex)]
        for key, value in config.items():
            prefix += ["-c", key + "=" + _encoded(value)]
        skills = sorted(str(path.resolve()) for path in codex_home.rglob("SKILL.md"))
        clean_agents = Path(entry["clean_home"]) / ".agents"
        if clean_agents.is_dir():
            skills += sorted(str(path.resolve()) for path in clean_agents.rglob("SKILL.md"))
        prefix += ["-c", "skills.config=[" + ",".join(
            "{path=" + json.dumps(path) + ",enabled=false}" for path in skills) + "]"]
        for feature in DISABLED_FEATURES:
            prefix += ["--disable", feature]
        return prefix

    def command(self, manifest: dict, run: dict, result_dir: Path) -> list[str]:
        return self._prefix(manifest, run, result_dir.parent) + [
            "exec", "--ignore-user-config", "--strict-config", "--ephemeral", "--json",
            "-C", run["repo"], "--output-last-message", str(result_dir / "final.txt"), "-",
        ]

    def provenance(self, run: dict) -> dict[str, Any]:
        entry = self.entry(run)
        return {"schema_version": 1, "adapter": "clean_codex_adapter",
                "overlay_sha256": self.overlay_hash,
                "manifest_sha256": self.overlay["manifest_sha256"], "run_id": run["id"],
                "original_repo": run.get("original_repo"), "execution_repo": run["repo"],
                "clean_home": entry["clean_home"], "codex_home": entry["codex_home"],
                "config_sha256": entry["config_sha256"],
                "environment_keys": sorted(self.environment({}, run))}

    def preflight(self, manifest: dict, run: dict) -> dict[str, Any]:
        result: dict[str, Any] = {"ready": False, "model_calls": 0, "errors": [],
                                  "environment": self.provenance(run)}
        try:
            if manifest["model"] != "gpt-5.6-luna" or manifest["reasoning"] != "xhigh":
                raise ValueError("only Luna xhigh is permitted")
            repo = validate_run_sources(run)
            entry = self.entry(run)
            codex_home = Path(entry["codex_home"])
            config = codex_home / "config.toml"
            if sha256(config) != entry["config_sha256"]:
                raise ValueError("clean Codex config changed")
            env = self.environment(manifest, run)
            home_sentinel = codex_home / "isolation-sentinel"
            home_sentinel.write_text("non-secret")
            prefix = self._prefix(manifest, run)
            def probe(tail: list[str], timeout: int = 60) -> subprocess.CompletedProcess:
                process = subprocess.run(prefix + tail, cwd=repo, env=env, capture_output=True,
                                         text=True, timeout=timeout)
                if process.returncode:
                    raise ValueError(f"zero-call probe failed ({tail[0]}): {process.stderr[-1000:]}")
                return process
            login = subprocess.run([prefix[0], "login", "status"], cwd=repo, env=env,
                                   capture_output=True, text=True, timeout=30)
            if login.returncode or "Logged in using ChatGPT" not in login.stdout + login.stderr:
                raise ValueError("clean home is not authenticated with ChatGPT")
            result["authentication"] = {"method": "ChatGPT", "passed": True}
            probe(["sandbox", "-P", "evaluation", "--include-managed-config", "-C", str(repo),
                   "--", "/usr/bin/true"])
            result["native_smoke"] = {"passed": True}
            expected_launcher = str(repo / "scripts" / "rzm")
            shell = probe([
                "sandbox", "-P", "evaluation", "--include-managed-config", "-C", str(repo), "--",
                "/bin/sh", "-c",
                'test "$(command -v rzm)" = "$1" && rzm --version && '
                "python3 -c 'import sys; assert sys.version_info >= (3, 11)'",
                "launcher-probe", expected_launcher,
            ])
            result["shell_runtime"] = {"launcher": expected_launcher, "python_minimum": "3.11",
                                       "passed": True, "stdout": shell.stdout.strip()}
            support = Path(self.overlay["support_root"]) / run["id"]
            support.mkdir(parents=True, exist_ok=True)
            sentinel = support / "write-denial-sentinel"
            sentinel.write_text("unchanged")
            protected = [path for path in self.protected_paths(manifest, run)
                         if path != codex_home / "auth.json"]
            targets = probe_targets(protected)
            fixture_file = repo / next(iter(run["fixture"]))
            metadata = [repo / ".git/config", repo / ".agents/skills/rhizome/SKILL.md"]
            access = probe(["sandbox", "-P", "evaluation", "--include-managed-config", "-C", str(repo),
                            "--", sys.executable, "-c", ACCESS_PROBE_SCRIPT, str(fixture_file), run["binary"],
                            str(sentinel), *map(str, metadata), *targets])
            if sentinel.read_text() != "unchanged":
                sentinel.write_text("unchanged")
                raise ValueError("protected write-denial probe failed")
            result["access_probe"] = {"denied_paths": len(targets), "passed": True,
                                      "stdout": access.stdout.strip()}
            raw = probe(["-C", str(repo), "debug", "prompt-input", run["prompt"]]).stdout
            result["discovery"] = validate_prompt(json.loads(raw), repo, run["prompt"])
            mcp = json.loads(probe(["mcp", "list", "--json"]).stdout)
            if any(server.get("enabled", True) for server in mcp):
                raise ValueError("an MCP server is enabled")
            result["mcp_servers"] = []
            features = probe(["features", "list"]).stdout
            for feature in DISABLED_FEATURES:
                if not re.search(r"^" + re.escape(feature) + r"\s+.*\sfalse\s*$", features, re.MULTILINE):
                    raise ValueError(f"feature is not disabled: {feature}")
            catalog = json.loads(probe(["debug", "models", "--bundled"]).stdout)
            models = catalog if isinstance(catalog, list) else catalog.get("models", [])
            luna = next((model for model in models if model.get("slug") == "gpt-5.6-luna"), None)
            if not luna or "xhigh" not in {item.get("effort") for item in luna.get("supported_reasoning_levels", [])}:
                raise ValueError("local catalog does not advertise Luna xhigh")
            result["model_advertisement"] = {"slug": "gpt-5.6-luna", "reasoning": "xhigh"}
            result["ready"] = True
        except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
            result["errors"].append(str(error))
        return result
