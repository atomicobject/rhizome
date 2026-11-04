# Finds, stops, and checks the running copies of a macOS app bundle.
# Sourced by install-dev.sh; app-instances_test.sh runs it against a throwaway app.
#
# A Tauri app with the single-instance plugin hands a new launch to whichever
# process holds its socket, then exits. Several bundles can share one
# identifier (the installed app, a checkout's build output, a copy in the
# Trash), so replacing an app means stopping every copy, not only the one at
# the install path.

APP_QUIT_SECONDS="${APP_QUIT_SECONDS:-10}"

# The tauri-plugin-single-instance socket for an identifier (macOS).
single_instance_socket() {
  echo "/tmp/${1//[.-]/_}_si.sock"
}

# PIDs of the copies LaunchServices knows by bundle identifier.
launched_pids() {
  osascript -l JavaScript -e 'function run(argv) {
    ObjC.import("AppKit");
    return ObjC.unwrap($.NSRunningApplication.runningApplicationsWithBundleIdentifier(argv[0]))
      .map((app) => app.processIdentifier).join("\n");
  }' "$1" | awk '/^[0-9]+$/'
}

# PIDs of every running copy: what LaunchServices knows by identifier, whatever
# holds the single-instance socket, and anything still running from the
# bundle's Contents/MacOS, such as rzm sidecars.
app_pids() {
  local bundle_id="$1" bundle="$2"
  {
    launched_pids "$bundle_id" || true
    lsof -t "$(single_instance_socket "$bundle_id")" 2>/dev/null || true
    pgrep -f "^$bundle/Contents/MacOS/" || true
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

# Quits every running copy and returns once they have all exited. Copies that
# do not quit within APP_QUIT_SECONDS get SIGTERM, then SIGKILL.
stop_app() {
  local bundle_id="$1" bundle="$2" name pids
  name="$(basename "$bundle" .app)"
  pids="$(app_pids "$bundle_id" "$bundle")"
  [[ -n "$pids" ]] || return 0
  echo "Quitting $name…"
  # A normal quit lets each copy save its window session.
  osascript -l JavaScript -e 'function run(argv) {
    ObjC.import("AppKit");
    ObjC.unwrap($.NSRunningApplication.runningApplicationsWithBundleIdentifier(argv[0]))
      .forEach((app) => app.terminate);
  }' "$bundle_id" >/dev/null || true
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
# when another copy is running or holds the single-instance socket.
verify_app() {
  local bundle_id="$1" bundle="$2" name launched holders command
  name="$(basename "$bundle" .app)"
  for _ in $(seq 1 50); do
    sleep 0.2
    launched="$(launched_pids "$bundle_id")"
    holders="$(lsof -t "$(single_instance_socket "$bundle_id")" 2>/dev/null || true)"
    [[ -n "$launched" && "$launched" != *$'\n'* ]] || continue
    command="$(ps -o command= -p "$launched" || true)"
    if [[ "$command" == "$bundle/Contents/MacOS/"* && ( -z "$holders" || "$holders" == "$launched" ) ]]; then
      echo "$name is running from $bundle (PID $launched)."
      return 0
    fi
  done
  echo "$name did not start from $bundle. Running copies:" >&2
  for launched in $(launched_pids "$bundle_id") $holders; do
    ps -o pid=,command= -p "$launched" >&2 || true
  done
  return 1
}
