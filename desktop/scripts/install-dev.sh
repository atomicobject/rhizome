#!/usr/bin/env bash
# Builds "Rhizome Dev" from the primary checkout on main, installs it into
# /Applications (override with RHIZOME_DEV_APP_DIR), and restarts it.
# Worktree and feature-branch builds would overwrite the installed app with
# unmerged work, so they are refused unless RHIZOME_DEV_INSTALL_FORCE=1.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
if [[ "${RHIZOME_DEV_INSTALL_FORCE:-}" != 1 ]]; then
  git_dir="$(cd "$(git rev-parse --git-dir)" && pwd -P)"
  common_dir="$(cd "$(git rev-parse --git-common-dir)" && pwd -P)"
  branch="$(git branch --show-current)"
  if [[ "$git_dir" != "$common_dir" || "$branch" != main ]]; then
    echo "Rhizome Dev installs only from the primary checkout on main (here: ${branch:-detached} in $root)." >&2
    echo "Merge to main and run this from the main checkout, or set RHIZOME_DEV_INSTALL_FORCE=1." >&2
    exit 1
  fi
fi

export PATH="$HOME/.cargo/bin:$PATH"
# shellcheck source=app-instances.sh
source "$root/desktop/scripts/app-instances.sh"
bundle_id="com.atomicobject.rhizome.dev"
apps="${RHIZOME_DEV_APP_DIR:-/Applications}"
mkdir -p "$apps"
apps="$(cd "$apps" && pwd -P)"
installed="$apps/Rhizome Dev.app"
built="$root/desktop/src-tauri/target/release/bundle/macos/Rhizome Dev.app"

make build
if [[ ! -d desktop/node_modules || desktop/package-lock.json -nt desktop/node_modules ]]; then
  (cd desktop && npm ci)
fi
(cd desktop && npm run tauri -- build --bundles app --config src-tauri/tauri.dev.conf.json)

# Every copy with this identifier must be gone before the relaunch: a launch
# while any copy holds the single-instance socket hands off to that copy.
stop_app "$bundle_id" "$installed"

# Replaced bundles go to the user's Trash, never deleted outright.
discard() {
  if command -v trash >/dev/null; then
    trash "$1"
  else
    mkdir -p "$HOME/.Trash"
    mv "$1" "$HOME/.Trash/$(basename "$1") $(date +%Y%m%d-%H%M%S)"
  fi
}
staged="$apps/.Rhizome Dev.app.installing"
if [[ -e "$staged" ]]; then discard "$staged"; fi
ditto "$built" "$staged"
if [[ -e "$installed" ]]; then discard "$installed"; fi
mv "$staged" "$installed"
echo "Installed $installed ($(git rev-parse --short HEAD))"
open "$installed"
verify_app "$bundle_id" "$installed"
