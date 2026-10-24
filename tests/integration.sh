#!/bin/sh
# SPDX-FileCopyrightText: The OpenArtifact Authors
#
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# End-to-end test: the plugin image (with a REAL oa) against a REAL OpenArtifact
# server backed by a Postgres container. Runs upload+promote with Woodpecker
# style CI_* env, then checks the artifact and the promoted bundle with oa.
#
# Required env:
#   OA_BIN       path of an oa >= 0.10.0 (linux/amd64 or matching host arch)
#   SERVER_BIN   path of an openartifact server binary matching that oa
# Optional:
#   STAGE (default prod)   PG_PORT (55432)   SERVER_PORT (18080)
#   DOCKER (docker)        KEEP=1 keeps the temp dir/containers on exit
#
# Usage: tests/integration.sh
set -eu

: "${OA_BIN:?set OA_BIN}" "${SERVER_BIN:?set SERVER_BIN}"
DOCKER=${DOCKER:-docker}
STAGE=${STAGE:-prod}
PG_PORT=${PG_PORT:-55432}
SERVER_PORT=${SERVER_PORT:-18080}
suffix=$$
PG=oa-plugin-it-pg-$suffix
IMAGE=drone-openartifact:it-$suffix
work=$(mktemp -d "${TMPDIR:-/tmp}/plugin-it.XXXXXX")
srv_pid=

cleanup() {
  [ -n "$srv_pid" ] && kill "$srv_pid" 2>/dev/null || true
  if [ -z "${KEEP:-}" ]; then
    "$DOCKER" rm -f "$PG" >/dev/null 2>&1 || true
    "$DOCKER" rmi -f "$IMAGE" >/dev/null 2>&1 || true
    rm -rf "$work"
  else
    echo "kept: $work $PG $IMAGE"
  fi
}
trap cleanup EXIT

here=$(cd "$(dirname "$0")/.." && pwd)
SERVER=http://127.0.0.1:$SERVER_PORT
OA="$OA_BIN --server $SERVER"

echo "== build image with the real oa"
rm -rf "$here/.oa-local" && mkdir "$here/.oa-local" && cp "$OA_BIN" "$here/.oa-local/oa"
"$DOCKER" build -q --build-arg OA_SOURCE=local -t "$IMAGE" "$here" >/dev/null
rm -rf "$here/.oa-local"

echo "== postgres + server"
"$DOCKER" run -d --rm --name "$PG" -p "127.0.0.1:$PG_PORT:5432" \
  -e POSTGRES_PASSWORD=it -e POSTGRES_DB=oa docker.io/library/postgres:18-alpine >/dev/null
i=0; until "$DOCKER" exec "$PG" pg_isready -U postgres -d oa >/dev/null 2>&1; do
  i=$((i+1)); [ $i -lt 60 ] || { echo "postgres not ready"; exit 1; }; sleep 1; done
sleep 2
mkdir -p "$work/store"
SERVER_HOST=127.0.0.1 SERVER_PORT=$SERVER_PORT DB_DRIVER=postgres DB_HOST=127.0.0.1 \
  DB_PORT=$PG_PORT DB_USER=postgres DB_PASSWORD=it DB_NAME=oa DB_SSLMODE=disable \
  STORAGE_BACKEND=filesystem STORAGE_PATH="$work/store" LOG_LEVEL=warn \
  "$SERVER_BIN" >"$work/server.out" 2>"$work/server.err" &
srv_pid=$!
i=0; until curl -fs "$SERVER/health" >/dev/null 2>&1; do
  i=$((i+1)); [ $i -lt 60 ] || { echo "server not ready"; cat "$work/server.err"; exit 1; }; sleep 1; done
pw=$(sed -n 's/.*admin password: *//p' "$work/server.err" "$work/server.out" | head -n1)
[ -n "$pw" ] || { echo "no admin password in server output"; exit 1; }

echo "== token + repo"
export HOME=$work/home; mkdir -p "$HOME"
token=$(printf '%s\n' "$pw" | $OA --format json auth token create --name plugin-it \
  --username admin --password-stdin | sed -n 's/.*"token": *"\([^"]*\)".*/\1/p')
[ -n "$token" ] || { echo "token creation failed"; exit 1; }
OA_TOKEN=$token $OA repo create it-releases --format raw >/dev/null

echo "== plugin: upload+promote (Woodpecker env)"
mkdir -p "$work/ws/dist"
echo "artifact one" > "$work/ws/dist/app-linux.txt"
echo "artifact two" > "$work/ws/dist/app-darwin.txt"
chmod -R a+rwX "$work/ws"
"$DOCKER" run --rm --network host -v "$work/ws:/ws:z" -w /ws \
  -e PLUGIN_SERVER="$SERVER" -e PLUGIN_TOKEN="$token" \
  -e PLUGIN_ACTION=upload+promote -e PLUGIN_REPO=it-releases -e 'PLUGIN_FILES=dist/*.txt' \
  -e PLUGIN_TARGET_PATH=myapp/1.0.0 -e PLUGIN_FLAT=true -e 'PLUGIN_PROPERTIES={"team":"core"}' \
  -e PLUGIN_STAGE="$STAGE" -e PLUGIN_NOTES="promoted by plugin it" -e PLUGIN_RESULT_FILE=/ws/result.json \
  -e CI_REPO=acme/myapp -e CI_REPO_NAME=myapp -e CI_PIPELINE_NUMBER=42 \
  -e CI_PIPELINE_URL=https://ci.example.com/repos/1/pipeline/42 -e CI_COMMIT_SHA=0123456789abcdef \
  -e CI_COMMIT_TAG=1.0.0 \
  "$IMAGE" | tee "$work/log.txt"

echo "== assertions"
grep -q 'OpenArtifact: upload+promote succeeded' "$work/log.txt"
! grep -q "$token" "$work/log.txt" "$work/ws/result.json"
grep -q '"success": true' "$work/ws/result.json"
curl -fs -H "Authorization: Bearer $token" "$SERVER/it-releases/myapp/1.0.0/app-linux.txt" | grep -q 'artifact one'
OA_TOKEN=$token $OA --format json bundle show myapp 1.0.0 | grep -q '"state"'
echo "== failure path: bad token must exit non-zero"
if "$DOCKER" run --rm --network host -v "$work/ws:/ws:z" -w /ws -e PLUGIN_SERVER="$SERVER" \
  -e PLUGIN_TOKEN=wrong -e PLUGIN_REPO=it-releases -e 'PLUGIN_FILES=dist/*.txt' "$IMAGE" >/dev/null 2>&1; then
  echo "expected failure with a bad token"; exit 1
fi
echo "integration: OK"
