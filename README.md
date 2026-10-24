<!--
SPDX-FileCopyrightText: The OpenArtifact Authors

SPDX-License-Identifier: AGPL-3.0-or-later
-->

# drone-openartifact

[Woodpecker](https://woodpecker-ci.org) and [Drone](https://www.drone.io) plugin for [OpenArtifact](https://github.com/openartifact/openartifact): upload build artifacts, tag them with the pipeline that produced them, and promote release bundles between stages.

The plugin is a thin wrapper around the `oa` CLI (`oa upload`, `oa bundle`, `oa promote`). It maps plugin settings and the CI environment to `oa` arguments, prints a summary in the step log and can write a JSON result file. It never calls the REST API itself.

- Image: `ghcr.io/openartifact/drone-openartifact` (linux/amd64, linux/arm64, signed with cosign keyless, SBOM attached)
- Requires OpenArtifact / `oa` **0.10.0 or newer**; the plugin refuses to run against an older `oa`
- Full parameter reference: [DOCS.md](DOCS.md)

## Woodpecker

```yaml
# .woodpecker.yaml
steps:
  - name: build
    image: golang:1
    commands:
      - go build -o dist/app ./...

  - name: publish
    image: ghcr.io/openartifact/drone-openartifact
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
      repo: releases
      files:
        - dist/*
      target_path: app/${CI_COMMIT_TAG}
      properties:
        channel: stable

  - name: release
    image: ghcr.io/openartifact/drone-openartifact
    when:
      event: tag
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
      repo: releases
      files:
        - dist/*
      action: upload+promote   # upload, create+seal bundle, promote
      stage: prod
      result_file: openartifact-result.json
```

## Drone

```yaml
# .drone.yml
kind: pipeline
type: docker
name: default

steps:
  - name: publish
    image: ghcr.io/openartifact/drone-openartifact
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
      repo: releases
      files:
        - dist/*
      target_path: app/${DRONE_TAG}

  - name: release
    image: ghcr.io/openartifact/drone-openartifact
    when:
      event: tag
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
      repo: releases
      files:
        - dist/*
      action: upload+promote
      stage: prod
```

## Actions

| `action`         | What runs                                                                          |
| ---------------- | ---------------------------------------------------------------------------------- |
| `upload`         | `oa upload` of `files` with build properties                                       |
| `bundle`         | `oa bundle create`, `bundle add --from-build`, `bundle seal`                       |
| `promote`        | `oa promote` of an existing sealed bundle                                          |
| `upload+promote` | all of the above in order; stops at the first failure                              |

Build metadata (`build.name`, `build.number`, `build.url`, `vcs.revision`) and the bundle name/version default from the Woodpecker `CI_*` or Drone `DRONE_*` environment; see [DOCS.md](DOCS.md#build-metadata).

The token is read from the `token` setting (`PLUGIN_TOKEN`) or `OA_TOKEN`, passed to `oa` through its environment only, and masked in any output.

## Running without a CI server

```sh
docker run --rm -v "$PWD:/ws" -w /ws \
  -e PLUGIN_SERVER=https://artifacts.example.com -e PLUGIN_TOKEN \
  -e PLUGIN_REPO=releases -e PLUGIN_FILES='dist/*' \
  -e CI_REPO=acme/app -e CI_PIPELINE_NUMBER=1 -e CI_COMMIT_SHA=$(git rev-parse HEAD) \
  ghcr.io/openartifact/drone-openartifact
```

## Development

```sh
go test ./...                     # unit tests (settings -> oa arguments, failure handling)
golangci-lint run
docker build -t drone-openartifact:dev .
tests/smoke-fake.sh drone-openartifact:dev   # image + fake oa, no server needed
tests/integration.sh drone-openartifact:dev  # image + real oa against a real server (see script header)
```

To test with an `oa` that is not released yet, put the binary at `.oa-local/oa` and build with `--build-arg OA_SOURCE=local`.

The Dockerfile downloads `oa` from the official release and verifies it against `SHA256SUMS` (`OA_VERSION` build argument). Bump it together with `MinOAVersion` in `plugin.go`.

## Verify a release

```sh
cosign verify ghcr.io/openartifact/drone-openartifact:<version> \
  --certificate-identity-regexp 'https://github.com/openartifact/drone-openartifact/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## License

AGPL-3.0-or-later. The logo in `images/` is the OpenArtifact mark from [openartifact/brand](https://github.com/openartifact/brand), CC-BY-SA-4.0.
