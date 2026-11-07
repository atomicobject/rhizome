#!/usr/bin/env bash
# Builds "Rhizome Dev" from the primary checkout on main, installs it, and
# restarts it. On macOS it installs the app bundle into /Applications; on Linux
# it installs the executables into ~/.local/share/rhizome-dev/bin with an
# applications-menu entry and icon. RHIZOME_DEV_APP_DIR overrides the
# directory: the bundle's parent on macOS, the executables' directory on Linux.
# Worktree and feature-branch builds would overwrite the installed app with
# unmerged work, so they are refused unless RHIZOME_DEV_INSTALL_FORCE=1.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
os="$(uname -s)"
if [[ "$os" != Darwin && "$os" != Linux ]]; then
  echo "make desktop-install-dev supports macOS and Linux, not $os." >&2
  exit 1
fi
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
# shellcheck disable=SC2034 # read by app-instances.sh
APP_LABEL="Rhizome Dev"

# Replaced installs go to the user's Trash, never deleted outright.
discard() {
  local trash_dir
  if command -v trash >/dev/null; then
    trash "$1"
  elif [[ "$os" == Linux ]] && command -v gio >/dev/null; then
    gio trash "$1"
  else
    trash_dir="$HOME/.Trash"
    [[ "$os" == Darwin ]] || trash_dir="${XDG_DATA_HOME:-$HOME/.local/share}/Trash/files"
    mkdir -p "$trash_dir"
    mv "$1" "$trash_dir/$(basename "$1") $(date +%Y%m%d-%H%M%S)"
  fi
}

# Replaces $2 with a fresh copy of $1, staged beside it first so a failed
# copy leaves the installed app alone.
replace_install() {
  local built="$1" installed="$2" staged
  staged="$(dirname "$installed")/.$(basename "$installed").installing"
  if [[ -e "$staged" ]]; then discard "$staged"; fi
  if [[ "$os" == Darwin ]]; then
    ditto "$built" "$staged"
  else
    mkdir -p "$staged"
    cp "${@:3}" "$staged/"
  fi
  if [[ -e "$installed" ]]; then discard "$installed"; fi
  mv "$staged" "$installed"
}

install_macos() {
  local apps installed built
  apps="${RHIZOME_DEV_APP_DIR:-/Applications}"
  mkdir -p "$apps"
  apps="$(cd "$apps" && pwd -P)"
  installed="$apps/Rhizome Dev.app"
  built="$root/desktop/src-tauri/target/release/bundle/macos/Rhizome Dev.app"

  (cd desktop && npm run tauri -- build --bundles app --config src-tauri/tauri.dev.conf.json)
  # Every copy with this identifier must be gone before the relaunch: a launch
  # while any copy holds the single-instance socket hands off to that copy.
  stop_app "$bundle_id" "$installed"
  replace_install "$built" "$installed"
  echo "Installed $installed ($(git rev-parse --short HEAD))"
  open "$installed"
  verify_app "$bundle_id" "$installed"
}

# The applications-menu entry gtk-launch and desktop launchers start the app
# from. pkg/app/repoexec finds the Dev app's rzm at the default directory; for
# another one, set RZM_DEV_APP_RZM to its rzm.
desktop_entry() {
  cat <<ENTRY
[Desktop Entry]
Type=Application
Name=Rhizome Dev
Comment=Explore local repositories and Markdown vaults
Exec="$1/rhizome-desktop"
Icon=rhizome-dev
Terminal=false
Categories=Development;
StartupWMClass=rhizome-desktop
ENTRY
}

install_linux() {
  local installed release data icon entry
  installed="${RHIZOME_DEV_APP_DIR:-$HOME/.local/share/rhizome-dev/bin}"
  mkdir -p "$(dirname "$installed")"
  installed="$(cd "$(dirname "$installed")" && pwd -P)/$(basename "$installed")"
  release="$root/desktop/src-tauri/target/release"
  data="${XDG_DATA_HOME:-$HOME/.local/share}"

  (cd desktop && npm run tauri -- build --no-bundle --config src-tauri/tauri.dev.conf.json)
  # The desktop executable and its sidecars must sit together: the app runs
  # the rzm beside rhizome-desktop as its own bundled executable.
  stop_app "$bundle_id" "$installed"
  replace_install "" "$installed" "$release/rhizome-desktop" "$release/rzm" "$release/team-api"

  # <hicolor size>:<file in icons-dev>
  for icon in 32x32:32x32.png 128x128:128x128.png 256x256:128x128@2x.png 512x512:icon.png; do
    install -D -m 0644 "desktop/src-tauri/icons-dev/${icon#*:}" "$data/icons/hicolor/${icon%%:*}/apps/rhizome-dev.png"
  done
  entry="$data/applications/$bundle_id.desktop"
  mkdir -p "$(dirname "$entry")"
  desktop_entry "$installed" >"$entry"
  echo "Installed $installed ($(git rev-parse --short HEAD)) with the launcher $entry"

  # Start it the way the applications menu does: from its launcher, without the
  # variables make adds for its own child processes, and in its own systemd
  # scope, so closing the terminal that ran make does not stop the app.
  local launch=(env -u MAKEFLAGS -u MAKELEVEL -u MFLAGS -u MAKEOVERRIDES)
  if command -v systemd-run >/dev/null && systemctl --user show-environment >/dev/null 2>&1; then
    launch+=(systemd-run --user --scope --quiet --collect --slice=app.slice --description="Rhizome Dev" --)
  fi
  if command -v gtk-launch >/dev/null; then
    "${launch[@]}" gtk-launch "$bundle_id"
  else
    "${launch[@]}" setsid -f "$installed/rhizome-desktop" </dev/null >/dev/null 2>&1
  fi
  verify_app "$bundle_id" "$installed"
}

make build
if [[ ! -d desktop/node_modules || desktop/package-lock.json -nt desktop/node_modules ]]; then
  (cd desktop && npm ci)
fi
if [[ "$os" == Darwin ]]; then install_macos; else install_linux; fi
