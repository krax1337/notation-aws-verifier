# Security Policy

## Supported versions

Security fixes are released for the latest minor version only. Upgrade to the latest release to receive fixes.

| Version | Supported |
| ------- | --------- |
| 1.0.x   | Yes       |
| < 1.0   | No        |

## Reporting a vulnerability

Please do not open public issues for security problems.

Report vulnerabilities through GitHub private vulnerability reporting: open the repository's
**Security** tab and choose **Report a vulnerability**
(<https://github.com/krax1337/notation-aws-verifier/security/advisories/new>).
If that is not possible, email [asanali@bulatov.dev](mailto:asanali@bulatov.dev).

Include the affected version, a description of the issue and its impact, and steps to reproduce.

This project is maintained by volunteers, so response times are best effort. The targets are:

- acknowledgement within 5 business days;
- an initial assessment within 10 business days;
- a fix or mitigation for confirmed issues as soon as practical, coordinated with the reporter before public disclosure.

## Verifying release artifacts

Container images and Helm charts are signed with [cosign](https://github.com/sigstore/cosign) keyless signing
from the release workflow, and carry GitHub build provenance attestations. Examples use `v1.0.0`; substitute the
version you deploy. The commands require cosign v3 or later and the GitHub CLI.

Verify the image signature:

```sh
cosign verify ghcr.io/krax1337/notation-aws-verifier:v1.0.0 \
  --certificate-identity-regexp '^https://github.com/krax1337/notation-aws-verifier/.github/workflows/release.yaml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Verify the image build provenance:

```sh
gh attestation verify oci://ghcr.io/krax1337/notation-aws-verifier:v1.0.0 \
  --repo krax1337/notation-aws-verifier
```

Verify the Helm chart signature:

```sh
cosign verify ghcr.io/krax1337/charts/notation-aws-verifier:1.0.0 \
  --certificate-identity-regexp '^https://github.com/krax1337/notation-aws-verifier/.github/workflows/release.yaml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Verify a downloaded release binary archive:

```sh
gh attestation verify notation-aws-verifier_1.0.0_linux_amd64.tar.gz \
  --repo krax1337/notation-aws-verifier
```

Each release attaches `notation-aws-verifier_<version>.intoto.jsonl` (the SLSA
provenance bundle, usable with `gh attestation verify --bundle`). Releases after
v1.0.0 also attach `checksums.txt.sigstore.json`, a keyless cosign signature of
`checksums.txt`, so archives can be verified offline from the release page:

```sh
cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/krax1337/notation-aws-verifier/.github/workflows/release.yaml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --ignore-missing -c checksums.txt
```
