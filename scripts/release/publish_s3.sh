#!/usr/bin/env bash
set -euo pipefail

# Publish an internal release to the private S3 mirror.
#
# The bucket name and legacy suffix are private and never enter Git. They come
# from RZM_INTERNAL_S3_BUCKET and RZM_INTERNAL_S3_LEGACY_SUFFIX in the release
# 1Password Environment (./scripts/with-secrets release).
#
# Two layouts are published so every installed client can update:
#   legacy clients (v0.50.x and older) read latest.json and .tbz archives:
#     s3://$BUCKET/latest.json
#     s3://$BUCKET/rhizome-{os}-{arch}.tbz
#     s3://$BUCKET/{version}/rhizome-{os}-{arch}.tbz
#   current clients read GitHub-release-shaped JSON (pkg/app/update):
#     s3://$BUCKET/releases/latest
#     s3://$BUCKET/releases/tags/{version}
#
# This script reuses goreleaser's dist/ output (which already ran as part of the
# release) instead of rebuilding, so S3 and GitHub binaries are identical.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

if ! command -v aws >/dev/null 2>&1; then
  echo "error: aws CLI is required (install and configure it first)" >&2
  exit 1
fi

RELEASE_BUCKET="${RZM_INTERNAL_S3_BUCKET:-}"
LEGACY_SUFFIX="${RZM_INTERNAL_S3_LEGACY_SUFFIX:-}"
RELEASE_PREFIX="rhizome"
RELEASE_DIR="dist/releases"
INSTALLER_NAME="install-rzm.sh"

if [[ -z "$RELEASE_BUCKET" ]]; then
  echo "error: RZM_INTERNAL_S3_BUCKET is not set; run through ./scripts/with-secrets release" >&2
  exit 1
fi
if [[ -n "$LEGACY_SUFFIX" && ! "$LEGACY_SUFFIX" =~ ^[a-z0-9]{10}$ ]]; then
  echo "error: RZM_INTERNAL_S3_LEGACY_SUFFIX must be exactly 10 characters (a-z0-9)" >&2
  exit 1
fi
BASE_URL="https://${RELEASE_BUCKET}.s3.amazonaws.com"

# Release matrix. Stable aliases use rhizome-{os}-{arch}.tbz; the optional
# rhizome-{arch}-{os}-{suffix}.tbz objects serve the oldest installers.
RELEASE_TARGETS="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64"

aws_preflight() {
  local identity identity_fields account arn
  if ! identity="$(aws sts get-caller-identity --output json 2>&1)"; then
    echo "error: AWS identity check failed: ${identity:-unknown AWS CLI error}" >&2
    echo "hint: verify 'aws --version' and 'command -v aws', then run 'aws login' (or 'aws sso login') if needed." >&2
    exit 1
  fi
  if ! identity_fields="$(
    python3 -c \
      'import json, sys; value = json.load(sys.stdin); print(value.get("Account", ""), value.get("Arn", ""), sep="\t")' \
      <<<"$identity" 2>&1
  )"; then
    echo "error: AWS identity response was not valid JSON: $identity_fields" >&2
    exit 1
  fi
  account="${identity_fields%%$'\t'*}"
  arn="${identity_fields#*$'\t'}"
  if [[ -z "$account" || "$account" == "None" || -z "$arn" || "$arn" == "None" ]]; then
    echo "error: AWS identity response did not include Account and Arn" >&2
    exit 1
  fi
  echo "aws: account=${account} arn=${arn}"
  if [[ -n "${RELEASE_ACCOUNT_ID:-}" && "$account" != "$RELEASE_ACCOUNT_ID" ]]; then
    echo "error: expected AWS account ${RELEASE_ACCOUNT_ID}, got ${account}" >&2
    exit 1
  fi
  if ! aws s3api head-bucket --bucket "$RELEASE_BUCKET" >/dev/null 2>&1; then
    echo "error: cannot access the internal release bucket (check S3 permissions)" >&2
    exit 1
  fi
}

aws_preflight
if [[ "${PREFLIGHT_ONLY:-}" == "1" || "${PREFLIGHT_ONLY:-}" == "true" ]]; then
  echo "preflight: ok"
  exit 0
fi

aws_cp_flags=(--only-show-errors)
if [[ "${DRY_RUN:-}" == "1" || "${DRY_RUN:-}" == "true" ]]; then
  aws_cp_flags+=(--dryrun)
  echo "dry-run: aws s3 cp will not upload artifacts"
fi

upload() {
  aws s3 cp "$1" "s3://${RELEASE_BUCKET}/$2" "${aws_cp_flags[@]}" "${@:3}"
  echo "uploaded: ${BASE_URL}/$2"
}

# Verify goreleaser has run and produced output
if [[ ! -d "dist" ]]; then
  echo "error: dist/ not found. Run goreleaser first (via cut_release.sh)." >&2
  exit 1
fi

mkdir -p "$RELEASE_DIR"
python3 "$ROOT_DIR/scripts/release/build_installer.py" --output-dir "$RELEASE_DIR" >/dev/null
VERSION="$(git describe --tags --exact-match 2>/dev/null || true)"
if [[ -z "$VERSION" ]]; then
  VERSION="$(python3 - <<'PY'
from pathlib import Path
import re
text = Path("pkg/vault/version/version.go").read_text()
m = re.search(r'Version\s*=\s*"([^"]+)"', text)
print(m.group(1) if m else "")
PY
)"
fi
if [[ -z "$VERSION" ]]; then
  echo "error: cannot determine release version" >&2
  exit 1
fi
if [[ "$VERSION" != v* ]]; then
  VERSION="v$VERSION"
fi

built_version="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("version",""))' dist/metadata.json 2>/dev/null || true)"
if [[ "v${built_version}" != "$VERSION" ]]; then
  echo "error: dist/ holds build '${built_version:-unknown}', not the ${VERSION} release; run the keyed release build (make release) first" >&2
  exit 1
fi
STABLE=1
if [[ "$VERSION" == *-* ]]; then
  STABLE=0
fi

version_dir="${RELEASE_DIR}/${VERSION}"
mkdir -p "$version_dir"
checksums_path="${version_dir}/checksums.txt"
manifest_entries="${version_dir}/manifest-artifacts.tsv"
: > "$checksums_path"
: > "$manifest_entries"

for target in $RELEASE_TARGETS; do
  target_os="${target%/*}"
  target_arch="${target#*/}"
  exe_suffix=""
  if [[ "$target_os" == "windows" ]]; then
    exe_suffix=".exe"
  fi

  # Goreleaser build directories look like dist/rzm-darwin_darwin_arm64_v8.0.
  goreleaser_dir=$(find dist -maxdepth 1 -type d -name "rzm-*_${target_os}_${target_arch}*" | head -1)
  if [[ -z "$goreleaser_dir" || ! -d "$goreleaser_dir" ]]; then
    echo "error: goreleaser output not found for ${target_os}/${target_arch}" >&2
    echo "       expected dist/rzm-*_${target_os}_${target_arch}*" >&2
    exit 1
  fi

  bin_path="${goreleaser_dir}/rzm${exe_suffix}"
  if [[ ! -f "$bin_path" ]]; then
    echo "error: binary not found at $bin_path" >&2
    exit 1
  fi

  archive_name="${RELEASE_PREFIX}-${target_os}-${target_arch}.tbz"
  archive_path="${version_dir}/${archive_name}"

  # Create archive with just the binary named "rzm"
  tar -cjf "$archive_path" -C "$goreleaser_dir" "rzm${exe_suffix}"
  archive_sha="$(shasum -a 256 "$archive_path" | awk '{print $1}')"
  printf '%s  %s\n' "$archive_sha" "$archive_name" >> "$checksums_path"
  printf '%s\t%s\t%s\n' "${target_os}/${target_arch}" "${BASE_URL}/${VERSION}/${archive_name}" "$archive_sha" >> "$manifest_entries"

  upload "$archive_path" "${VERSION}/${archive_name}"
  if [[ "$STABLE" == 1 ]]; then
    upload "$archive_path" "${archive_name}"
    if [[ -n "$LEGACY_SUFFIX" ]]; then
      upload "$archive_path" "${RELEASE_PREFIX}-${target_arch}-${target_os}-${LEGACY_SUFFIX}.tbz"
    fi
  fi

  # Current clients install the goreleaser archive, as they would from GitHub.
  current_name="${RELEASE_PREFIX}-${target_os}-${target_arch}.tar.gz"
  if [[ ! -f "dist/${current_name}" ]]; then
    echo "error: goreleaser archive not found at dist/${current_name}" >&2
    exit 1
  fi
  cp "dist/${current_name}" "${version_dir}/${current_name}"
  printf '%s  %s\n' "$(shasum -a 256 "${version_dir}/${current_name}" | awk '{print $1}')" "$current_name" >> "$checksums_path"
  upload "${version_dir}/${current_name}" "${VERSION}/${current_name}"
done

installer_path="${RELEASE_DIR}/${INSTALLER_NAME}"
if [[ ! -f "$installer_path" ]]; then
  echo "error: installer not found at $installer_path" >&2
  exit 1
fi
if ! grep -qF "${BASE_URL}/releases" "$installer_path"; then
  echo "error: staged installer does not point at the internal mirror" >&2
  exit 1
fi
installer_sha="$(shasum -a 256 "$installer_path" | awk '{print $1}')"
if [[ "$(awk '{print $1}' "${installer_path}.sha256" 2>/dev/null)" != "$installer_sha" ]]; then
  echo "error: installer checksum sidecar does not match the staged installer" >&2
  exit 1
fi
printf '%s  %s\n' "$installer_sha" "$INSTALLER_NAME" >> "$checksums_path"

python3 - "$VERSION" "$manifest_entries" "$installer_sha" "$BASE_URL" "$version_dir" <<'PY'
import json
import sys

version, entries_path, installer_sha, base_url, version_dir = sys.argv[1:6]
artifacts = {}
with open(entries_path) as entries:
    for line in entries:
        platform, url, sha = line.rstrip("\n").split("\t")
        artifacts[platform] = {"url": url, "sha256": sha}
legacy = {
    "version": version,
    "installer": {"url": f"{base_url}/install-rzm.sh", "sha256": installer_sha},
    "artifacts": artifacts,
}
with open(f"{version_dir}/latest.json", "w") as f:
    json.dump(legacy, f, indent=2, sort_keys=True)
    f.write("\n")

names = [f"rhizome-{platform.replace('/', '-')}.tar.gz" for platform in sorted(artifacts)]
names += ["checksums.txt", "install-rzm.sh", "install-rzm.sh.sha256"]
release = {
    "tag_name": version,
    "draft": False,
    "prerelease": "-" in version,
    "assets": [{"name": name, "browser_download_url": f"{base_url}/{version}/{name}"} for name in names],
}
with open(f"{version_dir}/release.json", "w") as f:
    json.dump(release, f, indent=2, sort_keys=True)
    f.write("\n")
PY

json_type=(--content-type application/json)
upload "$installer_path" "${VERSION}/${INSTALLER_NAME}"
upload "${installer_path}.sha256" "${VERSION}/${INSTALLER_NAME}.sha256"
upload "$checksums_path" "${VERSION}/checksums.txt"
upload "${version_dir}/latest.json" "${VERSION}/latest.json" "${json_type[@]}"
upload "${version_dir}/release.json" "releases/tags/${VERSION}" "${json_type[@]}"
# Root aliases move last, and only for stable releases, so clients never see a
# partial or prerelease version as latest.
if [[ "$STABLE" == 1 ]]; then
  upload "$installer_path" "${INSTALLER_NAME}"
  upload "${version_dir}/latest.json" "latest.json" "${json_type[@]}"
  upload "${version_dir}/release.json" "releases/latest" "${json_type[@]}"
fi
