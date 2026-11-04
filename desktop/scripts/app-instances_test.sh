#!/usr/bin/env bash
# Runs app-instances.sh against a throwaway background app with its own bundle
# identifier, so it never touches an installed Rhizome. macOS only.
set -euo pipefail
if [[ "$(uname)" != Darwin ]]; then
  echo "app-instances_test: skipped (macOS only)"
  exit 0
fi
here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=app-instances.sh
source "$here/app-instances.sh"
APP_QUIT_SECONDS=2

tmp="$(cd "$(mktemp -d)" && pwd -P)"
bundle_id="com.atomicobject.rhizome.app-instances-test.$$"
socket="$(single_instance_socket "$bundle_id")"
installed="$tmp/Applications/Fake.app"
stray="$tmp/build/Fake.app"
lsregister=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
cleanup() {
  # shellcheck disable=SC2046
  kill -KILL $(app_pids "$bundle_id" "$installed") $(app_pids "$bundle_id" "$stray") 2>/dev/null || true
  "$lsregister" -u "$installed" "$stray" 2>/dev/null || true
  rm -rf "${tmp:?}" "$socket"
}
trap cleanup EXIT
fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# A windowless app that writes <bundle>.quit when it quits normally;
# --stubborn refuses to quit and ignores SIGTERM.
cat >"$tmp/fake.swift" <<'SWIFT'
import AppKit
let stubborn = CommandLine.arguments.contains("--stubborn")
if stubborn { signal(SIGTERM, SIG_IGN) }
final class Delegate: NSObject, NSApplicationDelegate {
  func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
    if stubborn { return .terminateCancel }
    FileManager.default.createFile(atPath: Bundle.main.bundlePath + ".quit", contents: nil)
    return .terminateNow
  }
}
let delegate = Delegate()
NSApplication.shared.delegate = delegate
NSApplication.shared.setActivationPolicy(.accessory)
NSApplication.shared.run()
SWIFT
swiftc -o "$tmp/fake" "$tmp/fake.swift"
for app in "$installed" "$stray"; do
  mkdir -p "$app/Contents/MacOS"
  cp "$tmp/fake" "$app/Contents/MacOS/fake"
  cat >"$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>$bundle_id</string>
<key>CFBundleExecutable</key><string>fake</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSUIElement</key><true/>
</dict></plist>
PLIST
done

wait_launched() {
  for _ in $(seq 1 50); do
    [[ "$(launched_pids "$bundle_id" | wc -l | tr -d ' ')" == "$1" ]] && return 0
    sleep 0.2
  done
  fail "expected $1 running copies"
}
assert_stopped() {
  [[ -z "$(app_pids "$bundle_id" "$installed")" ]] || fail "copies still running: $(app_pids "$bundle_id" "$installed")"
}

echo "stop_app quits a copy outside the install path and the socket holder"
open -n "$installed"
open -n "$stray"
nc -lU "$socket" &
wait_launched 2
stop_app "$bundle_id" "$installed"
assert_stopped
[[ -e "$installed.quit" && -e "$stray.quit" ]] || fail "a copy was stopped without a normal quit"

echo "stop_app kills a copy that refuses to quit and ignores SIGTERM"
open -n "$stray" --args --stubborn
wait_launched 1
output="$(stop_app "$bundle_id" "$installed" 2>&1)" || fail "stop_app failed: $output"
[[ "$output" == *"did not stop; killing"* ]] || fail "no forced-kill message in: $output"
assert_stopped

echo "verify_app accepts the installed copy"
open "$installed"
verify_app "$bundle_id" "$installed" >/dev/null || fail "verify_app rejected the installed copy"
stop_app "$bundle_id" "$installed" >/dev/null

echo "verify_app rejects a copy running from elsewhere"
open -n "$stray"
if verify_app "$bundle_id" "$installed" 2>/dev/null; then fail "verify_app accepted the stray copy"; fi
stop_app "$bundle_id" "$installed" >/dev/null
assert_stopped
echo "app-instances_test: ok"
