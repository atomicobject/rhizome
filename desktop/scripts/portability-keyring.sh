#!/usr/bin/env bash
# Runs synthetic native-store checks without replacing an existing user service.
set -euo pipefail
umask 077

if [[ $# != 1 || "${GITHUB_ACTIONS:-}" != true || "$(uname -s)" != Linux ]]; then
  echo "usage (disposable Linux CI only): portability-keyring.sh <evidence directory>" >&2
  exit 2
fi
for tool in dbus-daemon dbus-send gnome-keyring-daemon go secret-tool timeout; do
  command -v "$tool" >/dev/null || { echo "missing native keyring dependency: $tool" >&2; exit 2; }
done
root="$(cd "$(dirname "$0")/../.." && pwd)"
mkdir -p "$1"
output="$(realpath "$1")"
fixture="$(mktemp -d "${RUNNER_TEMP:?RUNNER_TEMP is required}/rhizome-native-keyring.XXXXXX")"
runtime_uid="$(id -u)"
runtime_gid="$(id -g)"
runtime_dir="/run/user/$runtime_uid"
bus_pid=
keyring_pid=
native_passed=false

stop_owned() {
  local owned_pid=$1
  [[ -n "$owned_pid" ]] || return 0
  if kill -0 "$owned_pid" 2>/dev/null; then
    kill -TERM "$owned_pid" 2>/dev/null || true
    for _ in {1..20}; do
      kill -0 "$owned_pid" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$owned_pid" 2>/dev/null; then kill -KILL "$owned_pid" 2>/dev/null || true; fi
  fi
  wait "$owned_pid" 2>/dev/null || true
}
cleanup() {
  local status=$?
  trap - EXIT
  set +e
  stop_owned "$keyring_pid"
  stop_owned "$bus_pid"
  printf '{"uid":%s,"ownedUserBus":%s,"nativeStorePassed":%s,"exitCode":%d}\n' \
    "$runtime_uid" "$([[ -n "$bus_pid" ]] && echo true || echo false)" "$native_passed" "$status" >"$output/keyring.json"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ ! -e "$runtime_dir" && ! -L "$runtime_dir" ]]; then
  sudo -n install -d -m 0700 -o "$runtime_uid" -g "$runtime_gid" "$runtime_dir"
fi
if [[ ! -d "$runtime_dir" || -L "$runtime_dir" || "$(stat -c %u "$runtime_dir")" != "$runtime_uid" ]]; then
  echo "the real user's runtime directory is unavailable or not owned by that user" >&2
  exit 2
fi
export XDG_RUNTIME_DIR="$runtime_dir"
export DBUS_SESSION_BUS_ADDRESS="unix:path=$runtime_dir/bus"
export XDG_DATA_HOME="$fixture/data"
export XDG_CONFIG_HOME="$fixture/config"
export XDG_CACHE_HOME="$fixture/cache"
export GNOME_KEYRING_CONTROL="$fixture/control"
mkdir -p "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$GNOME_KEYRING_CONTROL"

bus_call() {
  timeout 5s dbus-send --bus="$DBUS_SESSION_BUS_ADDRESS" --print-reply --reply-timeout=2000 \
    --dest=org.freedesktop.DBus /org/freedesktop/DBus "$@" 2>>"$output/bus.log"
}
service_owner() {
  local reply
  reply="$(bus_call org.freedesktop.DBus.NameHasOwner "string:$1")" || return 1
  case "$reply" in
    *'boolean true'*) echo true ;;
    *'boolean false'*) echo false ;;
    *) echo "invalid service-owner response from the user bus" >&2; return 1 ;;
  esac
}

if [[ ! -S "$runtime_dir/bus" ]]; then
  if [[ -e "$runtime_dir/bus" || -L "$runtime_dir/bus" ]]; then
    echo "refusing to replace an existing user-bus path" >&2
    exit 2
  fi
  dbus-daemon --session --nofork --nopidfile --nosyslog --address="$DBUS_SESSION_BUS_ADDRESS" \
    >"$output/bus-daemon.log" 2>&1 &
  bus_pid=$!
  deadline=$((SECONDS + 10))
  until bus_call org.freedesktop.DBus.GetId >/dev/null; do
    if ! kill -0 "$bus_pid" 2>/dev/null || (( SECONDS >= deadline )); then
      echo "fixture user bus did not become ready" >&2
      exit 1
    fi
    sleep 0.1
  done
else
  bus_call org.freedesktop.DBus.GetId >/dev/null
fi

for service in org.freedesktop.secrets org.gnome.keyring; do
  owner="$(service_owner "$service")"
  if [[ "$owner" == true ]]; then
    echo "refusing to replace or unlock an existing keyring service" >&2
    exit 2
  fi
done

gnome-keyring-daemon --foreground --components=secrets --control-directory="$GNOME_KEYRING_CONTROL" --unlock \
  < <(printf '%s' 'rhizome-ci-native-store-fixture') >"$output/keyring-daemon.log" 2>&1 &
keyring_pid=$!
deadline=$((SECONDS + 10))
while true; do
  if ! kill -0 "$keyring_pid" 2>/dev/null || (( SECONDS >= deadline )); then
    echo "isolated Secret Service did not become ready" >&2
    exit 1
  fi
  owner="$(service_owner org.freedesktop.secrets)"
  [[ "$owner" == true ]] && break
  sleep 0.1
done
service_pid="$(bus_call org.freedesktop.DBus.GetConnectionUnixProcessID string:org.freedesktop.secrets | awk '$1 == "uint32" {print $2}')"
if [[ "$service_pid" != "$keyring_pid" ]]; then
  echo "Secret Service is not owned by the isolated fixture daemon" >&2
  exit 1
fi

cd "$root"
TEAMAPI_NATIVE_STORE_TEST=1 timeout 90s go test -mod=vendor -tags fts5 -count=1 -timeout 60s -v \
  ./pkg/teamapi/session -run '^TestNativeStore' 2>&1 | tee "$output/native-store.log"
native_passed=true
