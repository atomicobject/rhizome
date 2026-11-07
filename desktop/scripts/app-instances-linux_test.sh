#!/usr/bin/env bash
# Runs app-instances.sh on Linux against a throwaway app that claims its own
# single-instance D-Bus name, so it never touches an installed Rhizome. The
# fake app is a copy of the Python interpreter, so its executable lives in the
# directory under test, as rhizome-desktop and its sidecars do.
set -euo pipefail
skip() {
  echo "app-instances-linux_test: skipped ($*)"
  exit 0
}
command -v busctl >/dev/null || skip "needs busctl"
command -v python3 >/dev/null || skip "needs python3"
busctl --user status >/dev/null 2>&1 || skip "needs a session bus"
python3 -c 'import gi; gi.require_version("Gio", "2.0"); from gi.repository import Gio' 2>/dev/null ||
  skip "needs PyGObject"

here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=app-instances.sh
source "$here/app-instances.sh"
APP_QUIT_SECONDS=2

tmp="$(cd "$(mktemp -d)" && pwd -P)"
bundle_id="com.atomicobject.rhizome.app_instances_test.p$$"
installed="$tmp/installed/bin"
stray="$tmp/build/release"
cleanup() {
  # shellcheck disable=SC2046
  kill -KILL $(app_pids "$bundle_id" "$installed") $(app_pids "$bundle_id" "$stray") 2>/dev/null || true
  rm -rf "${tmp:?}"
}
trap cleanup EXIT
fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# Holds <identifier>.SingleInstance and writes the marker file when SIGTERM
# asks it to quit; --stubborn ignores SIGTERM, and --slow claims the name late.
cat >"$tmp/fake.py" <<'PY'
import signal, sys, time
import gi
gi.require_version("Gio", "2.0")
from gi.repository import Gio, GLib
name, marker = sys.argv[1], sys.argv[2]
stubborn = "--stubborn" in sys.argv
if "--slow" in sys.argv:
    time.sleep(1)
loop = GLib.MainLoop()
def on_term():
    if stubborn:
        return True
    open(marker, "w").close()
    loop.quit()
    return False
GLib.unix_signal_add(GLib.PRIORITY_DEFAULT, signal.SIGTERM, on_term)
Gio.bus_own_name(Gio.BusType.SESSION, name, Gio.BusNameOwnerFlags.DO_NOT_QUEUE,
                 None, None, lambda *_: sys.exit(3))
loop.run()
PY
python="$(readlink -f "$(command -v python3)")"
for dir in "$installed" "$stray"; do
  mkdir -p "$dir"
  cp "$python" "$dir/fake"
done

# Starts a copy outside this shell's process tree, as a launcher would, so a
# stopped copy never lingers as this shell's zombie.
launch() {
  local dir="$1"
  shift
  setsid -f "$dir/fake" "$tmp/fake.py" "$bundle_id.SingleInstance" "$dir.quit" "$@" </dev/null >/dev/null 2>&1
  for _ in $(seq 1 50); do
    [[ "$(pid_executable "$(instance_holders "$bundle_id")")" == "$dir/"* ]] && return 0
    sleep 0.2
  done
  fail "the copy in $dir did not claim the single-instance name"
}
assert_stopped() {
  local dir
  for dir in "$installed" "$stray"; do
    [[ -z "$(app_pids "$bundle_id" "$dir")" ]] || fail "copies still running: $(app_pids "$bundle_id" "$dir")"
  done
}

echo "stop_app quits a name holder outside the install directory and a sidecar inside it"
launch "$stray"
setsid -f "$installed/fake" -c 'import time; time.sleep(600)' </dev/null >/dev/null 2>&1
for _ in $(seq 1 50); do
  [[ -n "$(pids_running_from "$installed")" ]] && break
  sleep 0.2
done
[[ -n "$(pids_running_from "$installed")" ]] || fail "the sidecar did not start"
stop_app "$bundle_id" "$installed" >/dev/null
assert_stopped
[[ -e "$stray.quit" ]] || fail "the name holder was stopped without a normal quit"

echo "stop_app kills a copy that ignores SIGTERM"
launch "$stray" --stubborn
output="$(stop_app "$bundle_id" "$installed" 2>&1)" || fail "stop_app failed: $output"
[[ "$output" == *"did not stop; killing"* ]] || fail "no forced-kill message in: $output"
assert_stopped

echo "verify_app accepts the installed copy"
launch "$installed"
verify_app "$bundle_id" "$installed" >/dev/null || fail "verify_app rejected the installed copy"
stop_app "$bundle_id" "$installed" >/dev/null

echo "verify_app waits for a copy that claims the name late"
setsid -f "$installed/fake" "$tmp/fake.py" "$bundle_id.SingleInstance" "$installed.quit" --slow </dev/null >/dev/null 2>&1
# Called as install-dev.sh calls it, outside any || list, so set -e applies
# inside verify_app; a failure exits the test.
verify_app "$bundle_id" "$installed" >/dev/null
stop_app "$bundle_id" "$installed" >/dev/null

echo "verify_app rejects a copy running from elsewhere"
launch "$stray"
if verify_app "$bundle_id" "$installed" 2>/dev/null; then fail "verify_app accepted the stray copy"; fi
stop_app "$bundle_id" "$installed" >/dev/null
assert_stopped
echo "app-instances-linux_test: ok"
