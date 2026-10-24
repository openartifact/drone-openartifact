# SPDX-FileCopyrightText: The OpenArtifact Authors
#
# SPDX-License-Identifier: AGPL-3.0-or-later

# Where the oa CLI comes from: "release" downloads and checksum-verifies the
# official GitHub release; "local" copies ./.oa-local/oa (tests, unreleased oa).
ARG OA_SOURCE=release

# ---- plugin build -----------------------------------------------------------
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27.3-alpine@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /out/drone-openartifact .

# ---- oa from the official release, verified against SHA256SUMS --------------
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27.3-alpine@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS oa-release
ARG TARGETARCH
# Must be >= the plugin's minimum (MinOAVersion in plugin.go).
ARG OA_VERSION=0.9.0
WORKDIR /dl
RUN set -eu; \
    f="oa_${OA_VERSION}_linux_${TARGETARCH}.tar.gz"; \
    base="https://github.com/openartifact/openartifact/releases/download/v${OA_VERSION}"; \
    wget -q "$base/$f"; \
    wget -q "$base/SHA256SUMS"; \
    grep " $f\$" SHA256SUMS | sha256sum -c -; \
    tar -xzf "$f" oa; \
    install -D -m 0755 oa /out/oa

# ---- oa from the build context ----------------------------------------------
FROM scratch AS oa-local
COPY .oa-local/oa /out/oa

FROM oa-${OA_SOURCE} AS oa

# ---- runtime ----------------------------------------------------------------
FROM gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88
LABEL org.opencontainers.image.title="drone-openartifact" \
      org.opencontainers.image.description="Drone and Woodpecker plugin: upload artifacts and promote releases in OpenArtifact" \
      org.opencontainers.image.source="https://github.com/openartifact/drone-openartifact" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later"
COPY --from=oa /out/oa /usr/local/bin/oa
COPY --from=builder /out/drone-openartifact /usr/local/bin/drone-openartifact
ENTRYPOINT ["/usr/local/bin/drone-openartifact"]
