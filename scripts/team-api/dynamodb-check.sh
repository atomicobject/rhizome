#!/usr/bin/env bash
# Run every gateway race test against DynamoDB Local, with synthetic credentials.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
CONTAINER=""
cleanup() {
 if [ -n "$CONTAINER" ]; then docker stop "$CONTAINER" >/dev/null; fi
}
trap cleanup EXIT
if [ -z "${TEAMAPI_DYNAMODB_ENDPOINT:-}" ]; then
 CONTAINER="$(docker run --detach --rm -p 127.0.0.1::8000 amazon/dynamodb-local:3.1.0 -jar DynamoDBLocal.jar -inMemory -sharedDb)"
 TEAMAPI_DYNAMODB_ENDPOINT="http://$(docker port "$CONTAINER" 8000/tcp)"
 export TEAMAPI_DYNAMODB_ENDPOINT
fi
if [[ ! "$TEAMAPI_DYNAMODB_ENDPOINT" =~ ^http://(127\.0\.0\.1|localhost|\[::1\])(:[0-9]+)?/?$ ]]; then
 printf '%s\n' 'TEAMAPI_DYNAMODB_ENDPOINT must be a loopback HTTP DynamoDB Local endpoint' >&2
 exit 2
fi
# Local can take a moment to bind. The Go helper rejects non-loopback URLs
# before creating a table and constructs its own synthetic credential provider.
for _ in {1..60}; do
 if curl --silent --connect-timeout 1 --max-time 2 --output /dev/null "$TEAMAPI_DYNAMODB_ENDPOINT"; then break; fi
 sleep 0.5
done
curl --silent --connect-timeout 1 --max-time 2 --output /dev/null "$TEAMAPI_DYNAMODB_ENDPOINT"
make team-api-check TEAM_API_TEST_FLAGS='-count=1 -v'
