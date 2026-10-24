---
date: 2026-10-24T00:00:00+00:00
author: openartifact
containerImage: ghcr.io/openartifact/drone-openartifact
containerImageUrl: https://github.com/openartifact/drone-openartifact/pkgs/container/drone-openartifact
description: Plugin to upload artifacts to OpenArtifact and promote releases
icon: https://raw.githubusercontent.com/openartifact/drone-openartifact/refs/heads/main/images/logo.svg
image: ghcr.io/openartifact/drone-openartifact
logo: logo.svg
name: OpenArtifact
repo: openartifact/drone-openartifact
tags: [ publish, artifacts, release ]
title: OpenArtifact
url: https://github.com/openartifact/drone-openartifact
---

The OpenArtifact plugin uploads build artifacts to an [OpenArtifact](https://github.com/openartifact/openartifact) server, records the pipeline (build number, URL, commit) on every file, and promotes release bundles between stages. It wraps the `oa` CLI, so it needs OpenArtifact 0.10.0 or newer. The below pipeline configuration demonstrates simple usage:

```yaml
steps:
  - name: upload
    image: ghcr.io/openartifact/drone-openartifact
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
      repo: releases
      files:
        - dist/*.tar.gz
```

Example configuration for a Drone pipeline (the same settings apply, secrets use `from_secret` as well):

```yaml
kind: pipeline
type: docker
name: default

steps:
  - name: upload
    image: ghcr.io/openartifact/drone-openartifact
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
      repo: releases
      files:
        - dist/*.tar.gz
```

Example configuration with a target path, flat uploads and custom properties:

```diff
  - name: upload
    image: ghcr.io/openartifact/drone-openartifact
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
      repo: releases
      files:
        - dist/*.tar.gz
+     target_path: myapp/${CI_COMMIT_TAG}
+     flat: true
+     properties:
+       team: core
+       channel: stable
```

Example configuration that uploads on a tag, seals a release bundle with everything this pipeline uploaded and promotes it to the `prod` stage:

```diff
  - name: release
    image: ghcr.io/openartifact/drone-openartifact
+   when:
+     event: tag
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
      repo: releases
      files:
        - dist/*.tar.gz
+     action: upload+promote
+     bundle_pipeline: release
+     stage: prod
+     notes: Released by CI
+     result_file: openartifact-result.json
```

Example configuration that promotes an already sealed bundle, for example from a manually triggered pipeline:

```diff
  - name: promote
    image: ghcr.io/openartifact/drone-openartifact
    settings:
      server: https://artifacts.example.com
      token:
        from_secret: openartifact_token
+     action: promote
+     bundle_name: myapp
+     bundle_version: 1.2.3
+     stage: prod
```

## Build metadata

The plugin tags every uploaded file with `build.name`, `build.number`, `build.url` and `vcs.revision`, and uses the same build to fill a release bundle. The values come from the pipeline environment unless you set them explicitly:

| Setting          | Woodpecker           | Drone                |
| ---------------- | -------------------- | -------------------- |
| `build_name`     | `CI_REPO`            | `DRONE_REPO`         |
| `build_number`   | `CI_PIPELINE_NUMBER` | `DRONE_BUILD_NUMBER` |
| `build_url`      | `CI_PIPELINE_URL`    | `DRONE_BUILD_LINK`   |
| `vcs_revision`   | `CI_COMMIT_SHA`      | `DRONE_COMMIT_SHA`   |
| `bundle_name`    | `CI_REPO_NAME`       | `DRONE_REPO_NAME`    |
| `bundle_version` | `CI_COMMIT_TAG`      | `DRONE_TAG`          |

## Parameter Reference

server
: OpenArtifact server URL, for example `https://artifacts.example.com`

token
: API token with write access to the repository. Always pass it with `from_secret`; the plugin hands it to `oa` through the environment and never prints it

action
: `upload` (default), `bundle`, `promote` or `upload+promote`. `upload` uploads `files`. `bundle` creates the bundle `bundle_name`/`bundle_version`, adds the artifacts of this build (and `bundle_paths`) and seals it. `promote` promotes an existing sealed bundle to `stage`. `upload+promote` runs upload, bundle and promote in order and stops at the first failure

repo
: target repository. Uploads work on path based repositories (raw, maven, ivy, sbt, deb, rpm); for other formats use the native client

files
: list of files or glob patterns to upload, relative to the workspace

target_path
: path prefix inside the repository

flat
: upload files without their local directories

properties
: map of extra artifact properties, for example `team: core`

build_name
: build name property, defaults to the repository full name

build_number
: build number property, defaults to the pipeline number

build_url
: build URL property, defaults to the pipeline URL

vcs_revision
: commit SHA property, defaults to the pipeline commit

bundle_name
: release bundle name, defaults to the repository name

bundle_version
: release bundle version, defaults to the tag of a tag pipeline

bundle_description
: release bundle description

bundle_pipeline
: name of the promotion pipeline the bundle follows. Promotion needs a pipeline that defines `stage` (and, unless the stage fixes a repository, `target_repo`)

bundle_paths
: additional paths inside `repo` to add to the bundle

gpg_key
: GPG key id used to seal the bundle

stage
: promotion stage, required for `promote` and `upload+promote`

target_repo
: repository the promoted items are copied into

notes
: promotion notes

result_file
: write a JSON result (success, uploaded files, bundle state, per step output) to this path. It is also written when the step fails

oa_path
: path of the `oa` binary, default `oa` (the image ships a verified release of it)

## Result

The step log lists every uploaded file with size and checksum, the bundle state and the promotion. The exit code is non-zero if any `oa` call fails or if the installed `oa` is older than 0.10.0.
