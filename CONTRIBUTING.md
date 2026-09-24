# Contributing

Thanks for your interest in improving `notation-aws-verifier`. Bug reports, fixes, documentation, and
feature proposals are all welcome.

By participating you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md). Report security issues
privately as described in [SECURITY.md](SECURITY.md), not in public issues.

## Getting started

Requirements:

- Go (the version in `go.mod`; with `GOTOOLCHAIN=auto` the right toolchain is downloaded automatically)
- Docker with Buildx, for container builds
- Helm 3.8+ or Helm 4, for chart work
- `kubectl`, for local end-to-end testing with kind

Linters and chart tools (golangci-lint, helm-docs, kubeconform, kind) run through `go run` at pinned
versions, so they do not need to be installed. Run `make help` to list every target.

| Command | Purpose |
| ------- | ------- |
| `make build` | Build the binary into `bin/` |
| `make test` | Run unit tests with the race detector and coverage |
| `make lint` | Run golangci-lint with `.golangci.yml` |
| `make fmt` / `make vet` | Format and vet the code |
| `make docker-build` | Build the container image for the local platform |
| `make helm-lint` | Lint the Helm chart |
| `make helm-template` | Render the chart with several value sets and validate them with kubeconform |
| `make helm-docs` | Regenerate the chart README from `values.yaml` |
| `make manifests` | Regenerate `configs/install.yaml` from the chart |
| `make kind-e2e` | Create a kind cluster, load a locally built image, install the chart, and run its tests |
| `make kind-delete` | Delete the kind cluster |

## Making changes

1. Fork the repository and create a branch from `main`.
2. Make your change, with tests for behavior changes.
3. If you changed the chart, run `make helm-docs manifests` and commit the regenerated files. CI fails when
   `charts/notation-aws-verifier/README.md` or `configs/install.yaml` is out of date.
4. Add a line to `CHANGELOG.md` under `## [Unreleased]` for user-facing changes.
5. Run `make lint test` and open a pull request.

### Pull request titles

Use [Conventional Commits](https://www.conventionalcommits.org/) for pull request titles, for example
`feat: add --foo flag`, `fix(chart): quote region`, or `docs: clarify IRSA setup`. Pull requests are
squash-merged, and release notes are grouped by these prefixes.

### Sign-off

Signing off your commits under the [Developer Certificate of Origin](https://developercertificate.org/)
is encouraged:

```sh
git commit -s -m "fix: handle empty image list"
```

## Release process

Releases are cut by the maintainer from `main`:

1. Open a pull request that:
   - sets `version` (for example `1.1.0`) and `appVersion` (for example `"v1.1.0"`) in
     `charts/notation-aws-verifier/Chart.yaml`;
   - runs `make helm-docs manifests` and commits the regenerated files;
   - moves the `## [Unreleased]` entries in `CHANGELOG.md` into a new version section.
2. Merge the pull request once CI passes.
3. Tag the merge commit and push the tag:

   ```sh
   git tag -s v1.1.0 -m "v1.1.0"
   git push origin v1.1.0
   ```

The `release` workflow does the rest. It fails early if the tag does not match the chart's `appVersion`
and `version`. It then:

- builds binaries with GoReleaser, publishes a GitHub release with checksums and SBOMs, and attests the binaries;
- builds and pushes the multi-arch image `ghcr.io/krax1337/notation-aws-verifier` (tags `vX.Y.Z`, `X.Y.Z`,
  `X.Y`, and `latest`), signs it with cosign, and attaches provenance and SBOM attestations;
- packages the chart, pushes it to `oci://ghcr.io/krax1337/charts/notation-aws-verifier`, and signs it;
- attaches the chart archive and `configs/install.yaml` to the GitHub release.
