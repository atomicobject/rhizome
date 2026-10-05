"""Local Codex adapter: zero-call discovery before an explicitly budgeted run.

The Darwin wrapper only adds read denials. Codex still enforces workspace-write
and the user's existing exec policy. Personal homes/configuration are not moved.
"""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tomllib

from adapter_support import inventory, sha256, validate_run_sources


DISABLED_FEATURES = (
    "plugins", "apps", "memories", "multi_agent", "hooks", "recommended_plugins",
    "browser_use", "browser_use_external", "computer_use", "image_generation",
    "in_app_browser", "workspace_dependencies", "skill_mcp_dependency_install",
)


def prepare_isolation(repo: Path, directory: Path, forbidden_roots: list[Path]) -> dict:
    """Create a per-run immutable policy; does not call Codex or a model."""
    if sys.platform != "darwin" or not Path("/usr/bin/sandbox-exec").is_file():
        raise ValueError("this adapter requires Darwin sandbox-exec; no isolation fallback")
    repo = repo.resolve()
    directory.mkdir(parents=True, exist_ok=False)
    codex_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
    disabled_skills = sorted({str(p.resolve()) for root in
                             (codex_home / "skills", Path.home() / ".agents/skills")
                             if root.is_dir() for p in root.rglob("SKILL.md")})
    config_path = codex_home / "config.toml"
    config = tomllib.loads(config_path.read_text()) if config_path.exists() else {}
    mcp_names = sorted(config.get("mcp_servers", {}))
    if any("url" in server for server in config.get("mcp_servers", {}).values()):
        raise ValueError("HTTP MCP config needs an explicit disabled adapter; no silent fallback")
    denied_files = [codex_home / "AGENTS.md", codex_home / "AGENTS.override.md", config_path]
    # Parent instruction files must not enter this fixture's context either.
    for parent in repo.parents:
        denied_files.extend([parent / "AGENTS.md", parent / "AGENTS.override.md"])
    profile = directory / "isolation.sb"
    lines = ["(version 1)", "(allow default)"]
    lines.extend(f"(deny file-read-data (literal {json.dumps(str(p))}))" for p in denied_files)
    denied_roots = forbidden_roots + [codex_home / name for name in
        ("skills", "plugins", "memories", "sessions", "automations", "archived_sessions",
         "cache", "logs", "sqlite", "dictation-history", "shell_snapshots", "attachments",
         "browser", "computer-use", "mcp-oauth-locks", "handoffs", "node_repl",
         "skysight", "ambient-suggestions" )] + [Path.home() / ".agents"]
    # Codex requires listing this directory even with multi_agent disabled;
    # deny each role's contents while preserving that startup listing.
    if (codex_home / "agents").is_dir():
        denied_roots.extend((codex_home / "agents").iterdir())
    for pattern in ("*.sqlite*", "*history*", "*global-state*", "*continuity*", "session_index*", "*credentials*"):
        denied_roots.extend(codex_home.glob(pattern))
    for root in denied_roots:
        root = root.resolve()
        if repo.is_relative_to(root):
            raise ValueError("forbidden root overlaps fixture")
        lines.append(f"(deny file-read-data (subpath {json.dumps(str(root))}))")
    profile.write_text("\n".join(lines) + "\n")
    # debug subcommands lack exec's --ignore-user-config. Their zero-call
    # discovery probe needs config readable; every feature/MCP override remains.
    probe_profile = directory / "discovery.sb"
    config_denial = f"(deny file-read-data (literal {json.dumps(str(config_path))}))"
    probe_profile.write_text("\n".join(line for line in lines if line != config_denial) + "\n")
    return {"profile": str(profile), "profile_sha256": sha256(profile),
            "probe_profile": str(probe_profile), "probe_profile_sha256": sha256(probe_profile),
            "disabled_skills": disabled_skills, "disabled_mcp_servers": mcp_names,
            "denied_roots": [str(p.resolve()) for p in denied_roots],
            "denied_files": [str(p) for p in denied_files],
            "codex": str(Path(shutil.which("codex") or "codex").resolve()),
            "personal_config_sha256": sha256(config_path) if config_path.exists() else None}


def environment(manifest: dict, run: dict) -> dict[str, str]:
    # Keep normal authentication/home identity; discard provider secrets and app
    # session pointers. No environment dump is published in evidence.
    keep = ("HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TZ", "TMPDIR", "CODEX_HOME")
    env = {key: os.environ[key] for key in keep if key in os.environ}
    env["PATH"] = os.pathsep.join((str(Path(run["repo"]) / "scripts"),
                                  "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"))
    env["RZM_SKIP_REPO_DELEGATE"] = "1"
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    env["NO_COLOR"] = "1"
    return env


def _prefix(manifest: dict, run: dict) -> list[str]:
    isolation = run["isolation"]
    profile = Path(isolation["profile"])
    if sha256(profile) != isolation["profile_sha256"]:
        raise ValueError("isolation profile changed")
    config = {
        "model": "gpt-5.6-luna", "model_reasoning_effort": "xhigh",
        "model_provider": "openai", "sandbox_mode": "workspace-write",
        "developer_instructions": "", "web_search": "disabled",
        "shell_environment_policy.inherit": "none",
        "shell_environment_policy.set": environment(manifest, run),
        "allow_login_shell": False,
    }
    # JSON strings/booleans are valid TOML scalars. Tables/arrays use explicit TOML.
    args = ["/usr/bin/sandbox-exec", "-f", str(profile), isolation["codex"]]
    for key, value in config.items():
        if isinstance(value, dict):
            encoded = "{" + ",".join(json.dumps(k) + "=" + json.dumps(v) for k, v in value.items()) + "}"
        else:
            encoded = json.dumps(value)
        args += ["-c", key + "=" + encoded]
    skills = ",".join("{path=" + json.dumps(p) + ",enabled=false}" for p in isolation["disabled_skills"])
    args += ["-c", "skills.config=[" + skills + "]"]
    for name in isolation["disabled_mcp_servers"]:
        if not re.fullmatch(r"[A-Za-z0-9_-]+", name):
            raise ValueError("unsupported MCP config name; cannot prove override")
        args += ["-c", "mcp_servers." + name + ".enabled=false",
                 "-c", "mcp_servers." + name + '.command="/usr/bin/false"',
                 "-c", "mcp_servers." + name + ".args=[]"]
    for feature in DISABLED_FEATURES:
        args += ["--disable", feature]
    return args


def command(manifest: dict, run: dict, result_dir: Path) -> list[str]:
    return _prefix(manifest, run) + [
        "exec", "--ignore-user-config", "--strict-config", "--ephemeral", "--json",
        "-m", "gpt-5.6-luna", "-s", "workspace-write", "-C", run["repo"],
        "--output-last-message", str(result_dir / "final.txt"), "-",
    ]


def validate_prompt(items: list, repo: Path, prompt: str) -> dict:
    """Validate the actual zero-call prompt, not an assumed discovery setting."""
    texts = [c.get("text", "") for item in items for c in item.get("content", [])]
    joined = "\n".join(texts)
    roots = dict(re.findall(r"- `(r\d+)` = `([^`]+)`", joined))
    paths = []
    for alias, suffix in re.findall(r"\(file: (r\d+)/([^\)]+)\)", joined):
        if alias not in roots:
            raise ValueError("unresolved skill root")
        path = (Path(roots[alias]) / suffix).resolve()
        if not path.is_relative_to(repo.resolve()):
            raise ValueError(f"ambient skill exposed: {path}")
        paths.append(str(path.relative_to(repo)))
    if ".agents/skills/rhizome/SKILL.md" not in paths:
        raise ValueError("installed Rhizome skill not visible")
    if any(marker in joined for marker in ("<recommended_plugins>", "## Memory", "<app-context>")):
        raise ValueError("ambient plugin, memory, or app instructions exposed")
    instructions = [text for text in texts if text.startswith("# AGENTS.md instructions")]
    expected = (repo / "AGENTS.md").read_text().strip()
    if len(instructions) != 1 or expected not in instructions[0]:
        raise ValueError("fixture AGENTS missing or additional global instructions exposed")
    # AGENTS loader wraps one source in INSTRUCTIONS; reject extra prose in it.
    body = instructions[0].split("<INSTRUCTIONS>", 1)
    if len(body) != 2 or body[1].split("</INSTRUCTIONS>", 1)[0].strip() != expected:
        raise ValueError("AGENTS content differs from installed fixture bytes")
    if not texts or texts[-1] != prompt:
        raise ValueError("preflight task prompt differs")
    return {"skills": paths, "instructions_sha256": sha256(repo / "AGENTS.md"),
            "prompt_sha256": hashlib.sha256(prompt.encode()).hexdigest()}


def preflight(manifest: dict, run: dict) -> dict:
    result = {"ready": False, "model_calls": 0, "errors": []}
    try:
        if manifest["model"] != "gpt-5.6-luna" or manifest["reasoning"] != "xhigh":
            raise ValueError("only Luna xhigh is permitted")
        repo = validate_run_sources(run)
        prefix = _prefix(manifest, run)
        env = environment(manifest, run)
        # Check actual read access in the same additive sandbox used by Codex.
        # sandbox-exec loads its profile before applying that profile to children.
        denied = [p for p in run["isolation"]["denied_files"] if Path(p).is_file()]
        for root in run["isolation"]["denied_roots"]:
            path = Path(root)
            if path.exists():
                denied.append(str(path))
        script = "import os,sys; failures=[]\nfor p in sys.argv[1:]:\n try:\n  os.listdir(p) if os.path.isdir(p) else open(p,'rb').read(1)\n  failures.append(p)\n except PermissionError: pass\nprint(len(failures)); sys.exit(bool(failures))"
        access = subprocess.run(prefix[:3] + [sys.executable, "-c", script, *denied],
                                env=env, capture_output=True, text=True, timeout=30)
        if access.returncode:
            raise ValueError("ambient/support sandbox read-denial probe failed: " + access.stderr[-500:])
        result["read_denial_probe"] = {"checked_paths": len(denied), "passed": True}
        probe_profile = Path(run["isolation"]["probe_profile"])
        if sha256(probe_profile) != run["isolation"]["probe_profile_sha256"]:
            raise ValueError("discovery probe profile changed")
        probe_prefix = prefix.copy()
        probe_prefix[2] = str(probe_profile)
        def probe(tail: list[str]) -> str:
            proc = subprocess.run(probe_prefix + tail, cwd=repo, env=env, capture_output=True,
                                  text=True, timeout=60)
            if proc.returncode:
                raise ValueError(f"zero-call probe failed ({tail[0]}): {proc.stderr[-1000:]}")
            return proc.stdout
        # Loading the prompt is insufficient: task tools create their own
        # sandbox. macOS rejects that nested sandbox on the current host.
        # Fail before spending a model reservation, rather than bypassing it.
        probe(["sandbox", "macos", "--", "/usr/bin/true"])
        result["task_sandbox_probe"] = {"passed": True}
        raw = probe(["debug", "prompt-input", run["prompt"]])
        items = json.loads(raw)
        result["discovery"] = validate_prompt(items, repo, run["prompt"])
        result["prompt_input"] = items
        mcp = json.loads(probe(["mcp", "list", "--json"]))
        if any(server.get("enabled", True) for server in mcp):
            raise ValueError("an ambient MCP server is enabled")
        result["mcp_servers"] = [{"name": s.get("name"), "enabled": s.get("enabled")} for s in mcp]
        features = probe(["features", "list"])
        for feature in DISABLED_FEATURES:
            if not re.search(r"^" + re.escape(feature) + r"\s+.*\sfalse\s*$", features, re.MULTILINE):
                raise ValueError(f"feature is not disabled: {feature}")
        result["disabled_features"] = list(DISABLED_FEATURES)
        result["tool_inventory_evidence"] = "Optional surfaces disabled by configuration; this CLI has no supported zero-call effective builtin-tool inventory. Discovery permits config read; execution ignores and denies config."
        catalog = json.loads(probe(["debug", "models", "--bundled"]))
        models = catalog if isinstance(catalog, list) else catalog.get("models", [])
        luna = next((m for m in models if m.get("slug") == "gpt-5.6-luna"), None)
        if not luna or "xhigh" not in {x.get("effort") for x in luna.get("supported_reasoning_levels", [])}:
            raise ValueError("local catalog does not advertise Luna xhigh")
        result["model_advertisement"] = {"slug": luna["slug"], "reasoning": "xhigh",
                                         "entitlement": "not proven until authorized request"}
        result["codex_version"] = probe(["--version"]).strip()
        result["ready"] = True
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as exc:
        result["errors"].append(str(exc))
    return result
