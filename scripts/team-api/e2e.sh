#!/usr/bin/env bash
# End-to-end check of the team API gateway with the real binaries:
#   1. start `team-api dev` with fake providers on a loopback port;
#   2. sign in with `rzm auth login`, driving the development sign-in page;
#   3. with no provider keys, index and search a copy of the fixture vault and
#      run a Jev evaluation through the gateway;
#   4. check what reached the fake providers, the admin boundary, and the
#      server diagnostics; then sign out.
#
# TEAM_API_E2E_UPSTREAMS=live uses the real providers with the development keys
# from the login keychain (./scripts/with-secrets sync-keychain); the
# gateway alone reads them. Build first with `NO_WEB=1 make build`. The sign-in writes a synthetic
# session for the e2e origin to the login keychain; the script signs out and
# removes it, even on failure.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GOOS="$(go env GOOS)"
RZM="$ROOT/bin/$GOOS/rzm"
TEAM_API="$ROOT/bin/$GOOS/team-api"
PORT="${TEAM_API_E2E_PORT:-8789}"
UPSTREAMS="${TEAM_API_E2E_UPSTREAMS:-fake}"
ORIGIN="http://127.0.0.1:$PORT"
WORK="$(mktemp -d)"
SERVER_PID=""

say() { printf '\n==> %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

cleanup() {
	[ -d "$WORK/repo" ] && (cd "$WORK/repo" && RZM_SKIP_REPO_DELEGATE=1 "$RZM" stop >/dev/null 2>&1) || true
	"$RZM" auth logout --server "$ORIGIN" --json >/dev/null 2>&1 || true
	[ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null && wait "$SERVER_PID" 2>/dev/null || true
	if [ -n "${TEAM_API_E2E_KEEP:-}" ]; then echo "kept $WORK"; return; fi
	if command -v trash >/dev/null; then trash "$WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

[ -x "$RZM" ] && [ -x "$TEAM_API" ] || fail "build first: NO_WEB=1 make build"

say "Starting the local stack on $ORIGIN ($UPSTREAMS providers)"
live_flags=()
[ "$UPSTREAMS" = live ] && live_flags=(--keys-from-keychain)
env -u VOYAGE_API_KEY -u TYPESAFE_API_KEY "$TEAM_API" dev --listen "127.0.0.1:$PORT" --data-dir "$WORK/gateway" --upstreams "$UPSTREAMS" "${live_flags[@]}" >"$WORK/gateway.log" 2>&1 &
SERVER_PID=$!
for _ in $(seq 50); do curl -fs "$ORIGIN/healthz" >/dev/null 2>&1 && break; sleep 0.2; done
curl -fs "$ORIGIN/healthz" >/dev/null || { cat "$WORK/gateway.log"; fail "gateway did not start"; }

say "Signing in as the development member"
"$RZM" auth login --server "$ORIGIN" --no-browser --json --device-name e2e >"$WORK/login.jsonl" &
LOGIN_PID=$!
for _ in $(seq 50); do grep -q authorize_url "$WORK/login.jsonl" 2>/dev/null && break; sleep 0.2; done
python3 - "$WORK/login.jsonl" member <<'PY'
import json, re, sys, urllib.parse, urllib.request
url = next(json.loads(l)["url"] for l in open(sys.argv[1]) if "authorize_url" in l)
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k): return None
opener = urllib.request.build_opener(NoRedirect)
page = opener.open(url).read().decode()
csrf = re.search(r'name="csrf" value="([^"]+)"', page).group(1)
u = urllib.parse.urlsplit(url)
form = urllib.parse.urlencode({"csrf": csrf, "identity": sys.argv[2]}).encode()
try:
    opener.open(f"{u.scheme}://{u.netloc}{u.path}", form)
except urllib.error.HTTPError as e:
    urllib.request.urlopen(e.headers["Location"]).read()
PY
wait "$LOGIN_PID" || { cat "$WORK/login.jsonl"; fail "rzm auth login failed"; }
grep -q '"event":"signed_in"' "$WORK/login.jsonl" || fail "no signed_in event"
"$RZM" auth status --server "$ORIGIN" --json

say "Indexing a copy of the fixture vault with no provider keys"
cp -R "$ROOT/testdata/integration/python-app" "$WORK/repo"
cd "$WORK/repo"
# Index the fixture's Python under vault/ and embed notes through the
# gateway. Code embeddings use the same provider factory but stay off here:
# search right after indexing can still race their metadata write
# ("code embedding metadata is missing"), which predates the gateway.
sed -i.bak -e 's#^            - src$#            - vault/src#' -e 's#^            - packages$#            - vault/packages#' -e 's#^            - scripts$#            - vault/scripts#' .rhizome/config.yml
printf 'noteEmbeddings:\n    enabled: true\n    provider: voyage\n    model: voyage-4-lite\ncodeEmbeddings:\n    enabled: false\n' >>.rhizome/config.yml
run() {
	env -u VOYAGE_API_KEY -u RHIZOME_VOYAGE_API_KEY -u TYPESAFE_API_KEY -u ATOMIC_RHIZOME_KEY \
		RHIZOME_TEAM_API_ORIGIN="$ORIGIN" RZM_SKIP_REPO_DELEGATE=1 "$RZM" "$@"
}
run index
# Background index work can still be settling right after `rzm index` returns
# ("stale search continuation"), so search gets a few short retries.
for attempt in 1 2 3 4 5; do
	run search "invoice" --limit 3 && break
	[ "$attempt" = 5 ] && fail "search kept failing"
	sleep 2
done
# paid_calls ROUTE counts successful provider calls in the gateway's own
# diagnostics, the same evidence for fake and live providers.
paid_calls() {
	cat "$WORK"/gateway/.rhizome/diagnostics/events/*.jsonl | python3 -c '
import json, sys
n = 0
for line in sys.stdin:
    e = json.loads(line)
    a = e.get("attributes") or {}
    if e.get("event") == "teamapi.provider_request" and sys.argv[1] in json.dumps(a) and a.get("upstream_status") == 200:
        n += 1
print(n)' "$1"
}
voyage_calls="$(paid_calls /voyage/)"
[ "$voyage_calls" -gt 0 ] || fail "no embedding call succeeded through the gateway"
echo "gateway relayed $voyage_calls successful Voyage requests"

say "Running a Jev evaluation through the gateway"
echo '{"state":{"text":"The spec says the cache is per person."},"questions":{"accurate":{"type":"noul","instructions":"Is the statement plausible?"}}}' | run agent evaluate
[ "$(paid_calls /typesafe/)" -gt 0 ] || fail "no evaluation succeeded through the gateway"

say "Checking the admin boundary"
if "$RZM" auth admin usage --server "$ORIGIN" --json >"$WORK/admin.json" 2>&1; then fail "a member used the admin API"; fi
grep -q not_admin "$WORK/admin.json" || { cat "$WORK/admin.json"; fail "expected not_admin"; }

say "Checking server diagnostics"
events="$(find "$WORK/gateway/.rhizome/diagnostics/events" -name '*.jsonl' | wc -l | tr -d ' ')"
[ "$events" -gt 0 ] || fail "no server diagnostics events"
grep -h '"event"' "$WORK"/gateway/.rhizome/diagnostics/events/*.jsonl | python3 -c '
import collections, json, sys
names = collections.Counter(json.loads(l).get("event") for l in sys.stdin if l.strip())
print("server events:", dict(names))'
if grep -rqE 'rzt_[ar]_|dev-member@' "$WORK/gateway/.rhizome/diagnostics"; then fail "diagnostics contain a token or email"; fi

say "Signing out"
"$RZM" auth logout --server "$ORIGIN" --json
echo
echo "PASS: team API end-to-end"
