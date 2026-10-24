#!/bin/sh
# SPDX-FileCopyrightText: The OpenArtifact Authors
#
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Runs the image as a real Woodpecker step with `woodpecker-cli exec`, to check
# how Woodpecker turns `settings:` into PLUGIN_* (lists, maps, secrets) and
# that the plugin maps them to oa arguments. oa is a static fake mounted over
# the real one, so no server is needed (tests/integration.sh covers the real
# oa + server).
#
# Usage: WOODPECKER_CLI=/path/woodpecker-cli tests/woodpecker-exec.sh IMAGE
# DOCKER_HOST selects the container engine socket (docker or podman).
set -eu
image=${1:?usage: woodpecker-exec.sh IMAGE}
cli=${WOODPECKER_CLI:-woodpecker-cli}
work=$(mktemp -d "${TMPDIR:-/tmp}/wp-exec.XXXXXX")
trap 'rm -rf "$work"' EXIT
chmod 777 "$work"
mkdir -p "$work/ws/dist"
chmod 777 "$work/ws"
CGO_ENABLED=0 go build -o "$work/oa" ./tests/fakeoa
echo one > "$work/ws/dist/a.txt"
echo two > "$work/ws/dist/b.zip"
cat > "$work/ws/.woodpecker.yaml" <<YAML
steps:
  - name: publish
    image: $image
    pull: false
    settings:
      server: https://oa.example.com
      token: s3cr3t-token
      repo: releases
      files:
        - dist/*.txt
        - dist/*.zip
      flat: true
      target_path: app/
      properties:
        team: core
        channel: stable
      action: upload+promote
      bundle_version: 9.9.9
      stage: prod
      result_file: /woodpecker/src/result.json
YAML

cd "$work/ws"
"$cli" exec --local --repo-path "$work/ws" --backend-engine docker \
  --pipeline-number 7 --pipeline-event tag --commit-sha abc123 \
  --volumes "$work/oa:/usr/local/bin/oa:ro" \
  --env FAKE_OA_LOG=/woodpecker/src/calls.log --env CI_REPO=acme/app --env CI_REPO_NAME=app \
  .woodpecker.yaml

grep -q 'upload releases dist/\*.txt dist/\*.zip --target-path app/ --flat --property channel=stable --property team=core --build-name acme/app --build-number 7' calls.log
grep -q 'from-build acme/app/7 --repo releases' calls.log
grep -q 'promote app 9.9.9 --stage prod' calls.log
grep -q 'token_env=true' calls.log
! grep -q 's3cr3t-token' calls.log result.json
grep -q '"success": true' result.json
echo "woodpecker-exec: OK"
