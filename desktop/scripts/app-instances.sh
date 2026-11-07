# Finds, stops, and checks the running copies of an installed desktop app on
# macOS or Linux. Sourced by install-dev.sh; app-instances_test.sh runs it
# against a throwaway app.
#
# Each function takes the app's identifier and its install location: the .app
# bundle on macOS, or on Linux the directory holding the app's executables.
# Messages name the app APP_LABEL, which defaults to the bundle's name.
#
# A Tauri app with the single-instance plugin hands a new launch to whichever
# process holds its single-instance socket (macOS) or D-Bus name (Linux), then
# exits. Several copies can share one identifier (the installed app, a
# checkout's build output, a copy in the Trash), so replacing an app means
# stopping every copy, not only the one at the install path.

APP_QUIT_SECONDS="${APP_QUIT_SECONDS:-10}"
APP_OS="${APP_OS:-$(uname -s)}"

app_label() {
  echo "${APP_LABEL:-$(basename "$1" .app)}"
}

# The directory the app's own executables, sidecars included, run from.
app_exec_dir() {
  if [[ "$APP_OS" == Darwin ]]; then echo "$1/Contents/MacOS"; else echo "$1"; fi
}

# The tauri-plugin-single-instance socket for an identifier (macOS).
single_instance_socket() {
  echo "/tmp/${1//[.-]/_}_si.sock"
}

# The PID holding the single-instance socket (macOS) or the plugin's D-Bus
# name, <identifier>.SingleInstance, on the session bus (Linux).
instance_holders() {
  if [[ "$APP_OS" == Darwin ]]; then
    lsof -t "$(single_instance_socket "$1")" 2>/dev/null || true
  else
    # busctl fails while no process holds the name, as when the app is starting.
    busctl --user call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus \
      GetConnectionUnixProcessID s "$1.SingleInstance" 2>/dev/null | awk '$1 == "u" { print $2 }' || true
  fi
}

# PIDs of the copies the platform knows as launched apps: LaunchServices by
# bundle identifier on macOS, the single-instance holder on Linux.
launched_pids() {
  if [[ "$APP_OS" != Darwin ]]; then
    instance_holders "$1"
    return
  fi
  osascript -l JavaScript -e 'function run(argv) {
    ObjC.import("AppKit");
    return ObjC.unwrap($.NSRunningApplication.runningApplicationsWithBundleIdentifier(argv[0]))
      .map((app) => app.processIdentifier).join("\n");
  }' "$1" | awk '/^[0-9]+$/'
}

# The executable a PID runs, followed on macOS by its arguments. A Linux
# executable replaced on disk reads as "<path> (deleted)".
pid_executable() {
  if [[ "$APP_OS" == Darwin ]]; then
    ps -o command= -p "$1" 2>/dev/null || true
  else
    readlink "/proc/$1/exe" 2>/dev/null || true
  fi
}

# PIDs of processes running an executable from directory $1.
pids_running_from() {
  local dir="$1" exe pid
  if [[ "$APP_OS" == Darwin ]]; then
    pgrep -f "^$dir/" || true
    return
  fi
  for exe in /proc/[0-9]*/exe; do
    pid="${exe#/proc/}"
    pid="${pid%/exe}"
    [[ "$(readlink "$exe" 2>/dev/null)" == "$dir/"* ]] && echo "$pid"
  done
  return 0
}

# PIDs of every running copy: the launched copies, whatever holds the
# single-instance socket or name, and anything still running from the app's
# executable directory, such as rzm sidecars.
app_pids() {
  local bundle_id="$1" bundle="$2"
  {
    launched_pids "$bundle_id" || true
    instance_holders "$bundle_id"
    pids_running_from "$(app_exec_dir "$bundle")"
  } | awk '/^[0-9]+$/' | sort -un
}

# Waits up to $1 seconds for the remaining PIDs to exit.
wait_exited() {
  local seconds="$1" pid
  shift
  for _ in $(seq 0 $((seconds * 5))); do
    for pid in "$@"; do
      if kill -0 "$pid" 2>/dev/null; then
        sleep 0.2
        continue 2
      fi
    done
    return 0
  done
  return 1
}

# Asks the launched copies to quit the way the platform does: a normal quit,
# which lets each copy save its window session, on macOS; SIGTERM on Linux,
# where the window session is already saved as windows change.
quit_launched() {
  local bundle_id="$1"
  if [[ "$APP_OS" == Darwin ]]; then
    osascript -l JavaScript -e 'function run(argv) {
      ObjC.import("AppKit");
      ObjC.unwrap($.NSRunningApplication.runningApplicationsWithBundleIdentifier(argv[0]))
        .forEach((app) => app.terminate);
    }' "$bundle_id" >/dev/null || true
  else
    # shellcheck disable=SC2046 # one PID per word
    kill -TERM $(launched_pids "$bundle_id") 2>/dev/null || true
  fi
}

# Quits every running copy and returns once they have all exited. Copies that
# do not quit within APP_QUIT_SECONDS get SIGTERM, then SIGKILL.
stop_app() {
  local bundle_id="$1" bundle="$2" name pids
  name="$(app_label "$bundle")"
  pids="$(app_pids "$bundle_id" "$bundle")"
  [[ -n "$pids" ]] || return 0
  echo "Quitting $name…"
  quit_launched "$bundle_id"
  # shellcheck disable=SC2046 # one PID per word
  wait_exited "$APP_QUIT_SECONDS" $(launched_pids "$bundle_id") || true

  pids="$(app_pids "$bundle_id" "$bundle")"
  [[ -n "$pids" ]] || return 0
  # shellcheck disable=SC2086
  echo "Stopping $name processes that are still running:" $pids
  # shellcheck disable=SC2086
  kill -TERM $pids 2>/dev/null || true
  # shellcheck disable=SC2086
  wait_exited 5 $pids && return 0

  # shellcheck disable=SC2086
  echo "$name did not stop; killing" $pids >&2
  # shellcheck disable=SC2086
  kill -KILL $pids 2>/dev/null || true
  # shellcheck disable=SC2086
  if ! wait_exited 5 $pids; then
    echo "$name is still running after SIGKILL (PIDs: $pids)." >&2
    return 1
  fi
}

# Waits until exactly one copy is running and it runs from $bundle, and fails
# when another copy is running or holds the single-instance socket or name.
verify_app() {
  local bundle_id="$1" bundle="$2" name dir launched holders
  name="$(app_label "$bundle")"
  dir="$(app_exec_dir "$bundle")"
  for _ in $(seq 1 50); do
    sleep 0.2
    launched="$(launched_pids "$bundle_id")"
    holders="$(instance_holders "$bundle_id")"
    [[ -n "$launched" && "$launched" != *$'\n'* ]] || continue
    if [[ "$(pid_executable "$launched")" == "$dir/"* && ( -z "$holders" || "$holders" == "$launched" ) ]]; then
      echo "$name is running from $bundle (PID $launched)."
      return 0
    fi
  done
  echo "$name did not start from $bundle. Running copies:" >&2
  for launched in $(launched_pids "$bundle_id") $holders; do
    echo "$launched $(pid_executable "$launched")" >&2
  done
  return 1
}
