#!/bin/sh
# SPDX-FileCopyrightText: The OpenArtifact Authors
#
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Runs the image with a static fake oa mounted over the real one and checks
# the Woodpecker/Drone env -> oa invocation mapping, the step log and the
# result file. Usage: tests/smoke-fake.sh IMAGE
set -eu
image=${1:?usage: smoke-fake.sh IMAGE}
work=$(mktemp -d "${TMPDIR:-/tmp}/smoke.XXXXXX")
trap 'rm -rf "$work"' EXIT
chmod 777 "$work"
CGO_ENABLED=0 go build -o "$work/oa" ./tests/fakeoa
echo hello > "$work/file.txt"

${DOCKER:-docker} run --rm -v "$work:/ws:z" -w /ws \
  -v "$work/oa:/usr/local/bin/oa:ro,z" \
  -e FAKE_OA_LOG=/ws/calls.log \
  -e PLUGIN_SERVER=https://oa.example.com -e PLUGIN_TOKEN=s3cr3t-token \
  -e PLUGIN_ACTION=upload+promote -e PLUGIN_REPO=releases -e PLUGIN_FILES='*.txt' \
  -e PLUGIN_STAGE=prod -e 'PLUGIN_PROPERTIES={"team":"core"}' -e PLUGIN_RESULT_FILE=/ws/result.json \
  -e CI_REPO=acme/app -e CI_REPO_NAME=app -e CI_PIPELINE_NUMBER=7 -e CI_COMMIT_SHA=abc123 \
  -e CI_PIPELINE_URL=https://ci.example.com/7 -e CI_COMMIT_TAG=v1.0.0 \
  "$image" | tee "$work/log.txt"

grep -q 'OpenArtifact: upload+promote succeeded' "$work/log.txt"
! grep -q 's3cr3t-token' "$work/log.txt" "$work/calls.log" "$work/result.json"
grep -q 'token_env=true args=--server https://oa.example.com --format json upload releases \*.txt --property team=core --build-name acme/app --build-number 7 --build-url https://ci.example.com/7 --vcs-revision abc123' "$work/calls.log"
grep -q 'promote app v1.0.0 --stage prod' "$work/calls.log"
grep -q '"success": true' "$work/result.json"
echo "smoke-fake: OK"
