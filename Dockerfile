# syntax=docker/dockerfile:1
# Base images are pinned by multi-arch index digest directly in FROM so that
# Dependabot can bump tag and digest together.
FROM --platform=$BUILDPLATFORM golang:1.26.8-alpine3.24@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS builder

ARG TARGETOS
ARG TARGETARCH

# Build the AWS Signer notation plugin from source so it links against the
# current Go toolchain; the prebuilt CDN binary ships with an old Go stdlib and
# its CVEs. The pseudo-version pins upstream main at 93a2aa12f47c (2025-12-16,
# newest commit, newer than the v1.0.2292 tag). It is immutable and its checksum
# is verified against sum.golang.org.
ARG SIGNER_PLUGIN_PKG="github.com/aws/aws-signer-notation-plugin/cmd"
ARG SIGNER_PLUGIN_VERSION="v1.0.2293-0.20251216222753-93a2aa12f47c"
WORKDIR /aws-signer-plugin
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod init plugin-pin && \
    go get ${SIGNER_PLUGIN_PKG}@${SIGNER_PLUGIN_VERSION} && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
      -o /out/notation-com.amazonaws.signer.notation.plugin ${SIGNER_PLUGIN_PKG}

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/notation-aws-verifier .

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

ARG VERSION=dev
LABEL org.opencontainers.image.title="notation-aws-verifier" \
      org.opencontainers.image.description="Kyverno extension service that verifies Notation signatures made with AWS Signer" \
      org.opencontainers.image.source="https://github.com/krax1337/notation-aws-verifier" \
      org.opencontainers.image.url="https://github.com/krax1337/notation-aws-verifier" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}"

WORKDIR /

# Notation plugins are copied from here into $NOTATION_DIR/plugins at startup.
ENV PLUGINS_DIR=/plugins

COPY --from=builder /out/notation-com.amazonaws.signer.notation.plugin /plugins/com.amazonaws.signer.notation.plugin/notation-com.amazonaws.signer.notation.plugin
COPY --from=builder /out/notation-aws-verifier /notation-aws-verifier

USER 65532:65532
ENTRYPOINT ["/notation-aws-verifier"]
