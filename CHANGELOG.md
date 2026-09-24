# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- Base images are pinned by digest directly in `FROM`, so Dependabot keeps tag and digest current.
- Releases attach a keyless cosign signature of `checksums.txt` and the SLSA provenance bundle.
- Added a fuzz test for `/checkimages` request parsing.
- Added `osv-scanner.toml` documenting advisories that do not affect this service.

## [1.0.0] - 2026-09-24

First release of `notation-aws-verifier`, forked from
[nirmata/kyverno-notation-aws](https://github.com/nirmata/kyverno-notation-aws) at commit `b0677e4`.

### Added

- `--logFormat` flag (`text` or `json`) (upstream #471).
- `--tokenReviewAudiences` flag for Kyverno tokens minted with a custom audience (upstream #485).
- `--version` flag; the version is also logged at startup.
- Chart: `replicaCount`, `resources`, `podDisruptionBudget`, `topologySpreadConstraints`,
  `priorityClassName`, `podAnnotations`, `podLabels`, `extraArgs`, `extraEnv`, `logLevel`,
  `logFormat`, `cache.*`, `tokenReview.*`, `defaultTrustPolicy`, `service.*`, `configMap.data`,
  `image.digest`, `image.pullSecrets` (upstream #459).
- Multi-arch (linux/amd64, linux/arm64) images with a native signer plugin per architecture.

### Changed

- Renamed the Go module to `github.com/krax1337/notation-aws-verifier`, the container image to
  `ghcr.io/krax1337/notation-aws-verifier`, and the Helm chart to `notation-aws-verifier`
  (published at `oci://ghcr.io/krax1337/charts/notation-aws-verifier`). The CRD API group
  `notation.nirmata.io` is unchanged for drop-in migration.
- Inlined the unmaintained `github.com/nirmata/kyverno-notation-verifier` v1.1.0 library under `internal/`.
- The AWS Signer plugin is built from source (pinned, checksum-verified) instead of downloaded
  as a prebuilt amd64-only binary. This fixes EKS Pod Identity (`only loopback hosts are allowed`).
- Updated Go to 1.26.8 and all dependencies (Kubernetes 0.37, controller-runtime 0.25,
  Kyverno 1.19, notation-go 1.3, ristretto v2).
- Per-request logs moved from `info` to `debug`.
- Unauthenticated callers get `401` and unauthorized callers `403` (previously `406`).
- Chart: all resources are named from the release fullname; the Deployment, ServiceAccount and
  Service no longer disagree for non-default release names.
- Chart: hardcoded `--debug` removed.
- Chart: RBAC trimmed to what the service uses; `system:auth-delegator` binding replaced by a
  `tokenreviews` create rule.
- Chart: `readOnlyRootFilesystem: true`, UID/GID 65532, CRDs annotated with
  `helm.sh/resource-policy: keep` so uninstalling does not delete trust resources.

### Fixed

- Memory growth under load (upstream #482): conditions were evaluated in one Kyverno context
  shared by all concurrent requests, attestation layer readers were never closed, the cache
  was effectively unbounded (`MaxCost` 1 GiB with zero item cost), and every request built new
  ECR/GCR/ACR credential helpers.
- Default `--cacheTTLDurationSeconds` was 3.6e12 seconds instead of 3600.
- Panic on an `Authorization` header without the `Bearer ` prefix.
- With several replicas, deleting a TrustPolicy or TrustStore left stale files on the replicas
  that did not handle the finalizer; reconciles now rebuild state from the full list.
- Trust policy/store file write errors were ignored; files are now written atomically.
- `Stop` could deadlock on shutdown; the plain HTTP server had no timeouts.
- Plugin setup failed after a container restart with a persisted `emptyDir`.
- Chart: `image.pullPolicy` is honored, `AWS_REGION` is quoted, probe blocks no longer render
  stray whitespace, the Helm test pod renders, `serviceAccount.enabled=false` works.

### Security

- Kyverno service account bearer tokens were logged at `info` on every request; they are no
  longer logged.
- The caller-supplied `metadata` field (`kyverno-notation-aws.io/verify-images` annotation) no
  longer skips verification; a Pod's creator could set it to bypass signature checks.
- Request bodies are size-limited.
- Plugin binaries are installed `0755` instead of `0777`.
- Release images and charts are signed with cosign (keyless) and ship SLSA build provenance
  and SBOM attestations.

[Unreleased]: https://github.com/krax1337/notation-aws-verifier/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/krax1337/notation-aws-verifier/releases/tag/v1.0.0
