#!/usr/bin/env python3
"""Build two unsigned native packages with a throwaway signed loopback updater feed."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import re
import subprocess
import sys
import tempfile

from assemble_desktop import assemble, fixture_base_url
from build_native_desktop import build, run
from desktop_candidates import CandidateError, MANIFEST, NATIVE_TARGETS, ROOT, TARGETS, sha256
from run_clean import scoped_environment
from updater_fixture import prepare

VERSIONS = ('0.0.1', '0.0.2')
GOARCH = {'x86_64': 'amd64', 'aarch64': 'arm64'}


def build_fixture(target: str, output: Path, *, root=ROOT, base_url='http://127.0.0.1:9444', source=None,
                  runner=subprocess.run, builder=build, assembler=assemble, prepare_key=prepare,
                  team_api_origin='', evidence_dir: Path | None = None) -> Path:
    if target not in NATIVE_TARGETS:
        raise CandidateError('upgrade fixtures support only the native desktop targets')
    base_url = fixture_base_url(base_url)
    output = output.resolve()
    if output.exists():
        raise CandidateError('upgrade fixture output must be fresh')
    evidence_dir = (evidence_dir or output.parent / (output.name + '-evidence')).resolve()
    env = scoped_environment(os.environ if source is None else source, 'build')
    env.pop('GORELEASER_CURRENT_TAG', None)
    team_api_origin = run(['node', '--input-type=module', '-e',
        'import {desktopTeamAPIOrigin,releaseTeamAPIOrigin} from "./desktop/scripts/team-api-origin.mjs"; console.log(desktopTeamAPIOrigin(process.env,releaseTeamAPIOrigin))'],
        root=root, env={**env, 'RZM_DESKTOP_TEAM_API_ORIGIN': team_api_origin}, runner=runner)
    commit = run(['git', 'rev-parse', 'HEAD'], root=root, env=env, runner=runner)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.upgrade-fixture-', dir=output.parent) as temporary:
        work = Path(temporary)
        staged = work / 'public'
        staged.mkdir()
        config = (root / '.goreleaser.yml').read_text(encoding='utf-8')
        if 'RZM_CI_RELEASE_VERSION' not in config:
            raise CandidateError('fixture builds require the canonical exact-version snapshot template')
        pattern = r'(?m)^(\s+- -X github\.com/atomicobject/rhizome/pkg/teamapi/session\.DefaultOrigin=)\S+$'
        config, count = re.subn(pattern, lambda match: match[1] + team_api_origin, config)
        if count != 3:
            raise CandidateError('fixture origin override no longer matches the canonical build definitions')
        fixture_config = work / 'goreleaser-fixture.yml'
        fixture_config.write_text(config, encoding='utf-8')
        overlay = prepare_key(work / 'key', base_url + '/latest.json', source=env, root=root, runner=runner)
        public_key = json.loads(overlay.read_text(encoding='utf-8'))['plugins']['updater']['pubkey']
        shutil.copyfile(overlay, staged / 'updater-test-overlay.json')
        (staged / 'updater.pub').write_text(public_key + '\n')
        for version in VERSIONS:
            cli = work / ('rzm-' + version + ('.exe' if target.startswith('windows-') else ''))
            build_env = {**env, 'GOOS': target.split('-')[0], 'GOARCH': GOARCH[target.split('-', 1)[1]],
                         'RZM_CI_RELEASE_VERSION': version}
            # GoReleaser's TARGET overrides GOOS/GOARCH; the workflow uses Tauri's spelling.
            build_env.pop('TARGET', None)
            build_id = 'rzm-windows-amd64' if target.startswith('windows-') else 'rzm-linux'
            run(['goreleaser', 'build', '--snapshot', '--clean', '--single-target', '--id', build_id,
                 '--skip=before,post-hooks', '--config', str(fixture_config), '--output', str(cli)],
                root=root, env=build_env, runner=runner, diagnostic_path=evidence_dir / version / 'goreleaser.log')
            candidate = work / ('candidate-' + version)
            builder(cli, version, commit, target, candidate, canonical_hash=sha256(cli), test_overlay=overlay,
                    source=env, root=root, runner=runner, evidence_dir=evidence_dir / version)
            feed = assembler([candidate], version, commit, {target: sha256(cli)}, staged / version,
                             production=False, required_targets={target}, test_public_key=public_key,
                             test_base_url=base_url + '/' + version, source={**env,
                                 'TAURI_SIGNING_PRIVATE_KEY_PATH': str(work / 'key/updater')}, root=root, runner=runner)
            shutil.copyfile(candidate / MANIFEST, feed.parent / MANIFEST)
        shutil.copyfile(staged / VERSIONS[-1] / 'latest.json', staged / 'latest.json')
        (staged / 'EVIDENCE.txt').write_text(
            f'Unsigned {target} installed-upgrade fixture, source {commit}.\n'
            f'Test versions {VERSIONS[0]} and {VERSIONS[1]}; updater key is ephemeral and private key is discarded.\n'
            f'Team API origin: {team_api_origin}. Browser sign-in is an interactive installed-app check.\n'
            'Allowed config differences: version, loopback updater endpoint, test public key, loopback HTTP transport.\n'
            'Native release package format and installer mode are preserved.\n'
            'Windows fixture packages are unsigned; production publishing rejects them.\n'
            'Serve this directory with: python3 -m http.server 9444 --bind 127.0.0.1\n'
            'Install 0.0.1, keep the server running, then invoke the app updater to install 0.0.2.\n'
            'Installed GUI/session/runtime preservation is a human acceptance check.\n')
        staged.rename(output)
    return output


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--target', choices=sorted(NATIVE_TARGETS), required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--repo-root', type=Path, default=ROOT)
    parser.add_argument('--base-url', default='http://127.0.0.1:9444')
    parser.add_argument('--team-api-origin', default='', help='override the checked-in canonical release origin')
    parser.add_argument('--evidence-dir', type=Path)
    args = parser.parse_args()
    try:
        print(build_fixture(args.target, args.output, root=args.repo_root.resolve(), base_url=args.base_url,
                            team_api_origin=args.team_api_origin, evidence_dir=args.evidence_dir))
    except (CandidateError, OSError, ValueError) as error:
        print(f'upgrade fixture: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
