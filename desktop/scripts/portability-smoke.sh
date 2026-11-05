#!/usr/bin/env bash
# Exercises the real native accelerator under Xvfb; installed/Wayland checks are separate.
set -euo pipefail

if [[ $# != 2 ]]; then
  echo "usage: portability-smoke.sh <app executable> <evidence directory>" >&2
  exit 2
fi
app="$(realpath "$1")"
mkdir -p "$2"
output="$(realpath "$2")"
if [[ ! -x "$app" ]]; then
  echo "desktop executable is not runnable: $app" >&2
  exit 2
fi
for tool in xdotool import pgrep; do
  command -v "$tool" >/dev/null || { echo "missing smoke dependency: $tool" >&2; exit 2; }
done
if pgrep -x "$(basename "$app")" >/dev/null; then
  echo "another desktop app is running; refusing to control it" >&2
  exit 2
fi
state="$(mktemp -d "$output/state.XXXXXX")"
pid=
cleanup() {
  local status=$?
  trap - EXIT
  if [[ $status != 0 ]]; then
    import -window root "$output/failure.png" 2>>"$output/screenshot.log" || true
  fi
  if [[ -n "$pid" ]]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  exit "$status"
}
trap cleanup EXIT

RHIZOME_DESKTOP_DATA_DIR="$state" "$app" >"$output/stdout.log" 2>"$output/stderr.log" &
pid=$!
windows=()
wait_windows() {
  local expected=$1 deadline=$((SECONDS + 25))
  while (( SECONDS < deadline )); do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "desktop exited before $expected windows appeared" >&2
      return 1
    fi
    mapfile -t windows < <(xdotool search --onlyvisible --maxdepth 1 --pid "$pid" 2>/dev/null || true)
    if (( ${#windows[@]} >= expected )); then return 0; fi
    sleep 0.1
  done
  echo "expected $expected top-level windows, found ${#windows[@]}" >&2
  return 1
}

wait_windows 1
initial=${#windows[@]}
if (( initial != 1 )); then
  echo "fresh desktop state opened $initial windows instead of one" >&2
  exit 1
fi
xdotool windowfocus --sync "${windows[0]}"
xdotool key --clearmodifiers ctrl+n
wait_windows 2
printf '{"platform":"linux","display":"X11","initialWindows":%d,"newWindowCount":%d,"pid":%d}\n' \
  "$initial" "${#windows[@]}" "$pid" >"$output/smoke.json"
echo "native desktop startup and New Window passed (X11/Xvfb)"
