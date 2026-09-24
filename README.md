# notation-aws-verifier

[![CI](https://github.com/krax1337/notation-aws-verifier/actions/workflows/ci.yaml/badge.svg)](https://github.com/krax1337/notation-aws-verifier/actions/workflows/ci.yaml)
[![Release](https://img.shields.io/github/v/release/krax1337/notation-aws-verifier)](https://github.com/krax1337/notation-aws-verifier/releases)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/krax1337/notation-aws-verifier/badge)](https://scorecard.dev/viewer/?uri=github.com/krax1337/notation-aws-verifier)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/14823/badge)](https://www.bestpractices.dev/projects/14823)
[![Go Report Card](https://goreportcard.com/badge/github.com/krax1337/notation-aws-verifier)](https://goreportcard.com/report/github.com/krax1337/notation-aws-verifier)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

A [Kyverno](https://kyverno.io) extension service that verifies container image signatures
made with [AWS Signer](https://docs.aws.amazon.com/signer/latest/developerguide/Welcome.html)
and [Notation](https://notaryproject.dev/), and pins verified images to their digests.

> [!NOTE]
> **This is the maintained continuation of
> [nirmata/kyverno-notation-aws](https://github.com/nirmata/kyverno-notation-aws).**
> The original repository is no longer actively maintained and is being archived by its
> owners. Its last published image (`v1.1`) carries known CVEs and does not work with
> EKS Pod Identity. This project picks it up, fixes those problems, and keeps releasing.
> The fixes it started from have been running in production on EKS with Pod Identity
> since 2026.

## Why this fork

| | `nirmata/kyverno-notation-aws` `v1.1` | `notation-aws-verifier` `v1.0.0` |
| --- | --- | --- |
| EKS Pod Identity | fails (`only loopback hosts are allowed`) | works (signer plugin built from source against a current AWS SDK) |
| arm64 image | ships an amd64 signer plugin | native multi-arch (amd64 + arm64) |
| Bearer tokens in logs | logged at `info` on every request | never logged |
| Memory under load | grows until OOM ([#482](https://github.com/nirmata/kyverno-notation-aws/issues/482)) | bounded cache, leaks fixed |
| TokenReview audiences | not configurable ([#485](https://github.com/nirmata/kyverno-notation-aws/issues/485)) | `--tokenReviewAudiences` |
| JSON logs | no ([#471](https://github.com/nirmata/kyverno-notation-aws/issues/471)) | `--logFormat=json` |
| Helm HA settings | hardcoded replicas/resources ([#459](https://github.com/nirmata/kyverno-notation-aws/issues/459)) | `replicaCount`, PDB, topology spread, resources |
| Supply chain | unsigned | cosign-signed image and chart, SLSA provenance, SBOM |

See [CHANGELOG.md](CHANGELOG.md) for the full list.

## How it works

1. A Kyverno policy calls the service's `/checkimages` endpoint (via `apiCall`) with the
   images of the Pod being admitted.
2. The service checks that the caller is an allowed Kyverno service account (Kubernetes
   TokenReview).
3. For each image, it pulls the Notation signatures from ECR and verifies them with the
   AWS Signer plugin against the configured trust policy and trust store. Optionally it
   also verifies signed attestations (SBOMs, scan reports) against conditions.
4. It returns JSON patches that replace each image tag with its verified digest, and
   Kyverno mutates the Pod. Unsigned or untrusted images are rejected.

Trust policies and trust stores are Kubernetes custom resources (`TrustPolicy`,
`TrustStore` in API group `notation.nirmata.io`), so each team or namespace can have its own.

## Install

Requirements: Kubernetes 1.25+, Kyverno 1.11+, images in Amazon ECR signed with AWS Signer.

### Helm (OCI)

```sh
helm install notation-aws-verifier \
  oci://ghcr.io/krax1337/charts/notation-aws-verifier \
  --version 1.0.0 \
  --namespace notation-aws-verifier --create-namespace \
  --set region=<your-aws-region>
```

All chart values are documented in [charts/notation-aws-verifier/README.md](charts/notation-aws-verifier/README.md).

### Plain manifests

```sh
kubectl apply -f https://github.com/krax1337/notation-aws-verifier/releases/download/v1.0.0/install.yaml
```

The manifest is the chart rendered with default values into namespace `notation-aws-verifier`.
Set `AWS_REGION` on the Deployment to your region.

### Image

```text
ghcr.io/krax1337/notation-aws-verifier:v1.0.0   # linux/amd64, linux/arm64
```

## AWS permissions

The service needs to read images and signatures from ECR and check signature revocation:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow", "Action": ["signer:GetRevocationStatus"], "Resource": "*" },
    {
      "Effect": "Allow",
      "Action": [
        "ecr:GetAuthorizationToken",
        "ecr:BatchGetImage",
        "ecr:GetDownloadUrlForLayer",
        "ecr:DescribeImages",
        "ecr:ListImages"
      ],
      "Resource": "*"
    }
  ]
}
```

(`AmazonEC2ContainerRegistryReadOnly` plus the `signer:GetRevocationStatus` statement also works.)

Attach the role with either:

- **EKS Pod Identity** (recommended): create a Pod Identity association for namespace
  `notation-aws-verifier`, service account `notation-aws-verifier`. No annotation needed.
- **IRSA**: `--set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::<account>:role/<role>`

Without either, you can pass registry credentials as image pull secrets with
`deployment.imagePullSecrets` in the chart.

## Quick start

### 1. Sign an image with AWS Signer

```sh
export REGION=us-west-2 ACCOUNT=123456789012 PROFILE=myprofile
export IMAGE=$ACCOUNT.dkr.ecr.$REGION.amazonaws.com/demo:v1

aws signer put-signing-profile --profile-name $PROFILE \
  --platform-id Notation-OCI-SHA384-ECDSA \
  --signature-validity-period 'value=12,type=MONTHS'

aws ecr get-login-password --region $REGION \
  | notation login --username AWS --password-stdin $ACCOUNT.dkr.ecr.$REGION.amazonaws.com

notation key add --plugin com.amazonaws.signer.notation.plugin \
  --id arn:aws:signer:$REGION:$ACCOUNT:/signing-profiles/$PROFILE $PROFILE
notation sign $IMAGE --key $PROFILE
```

### 2. Create the trust store and trust policy

Edit the samples with your region, account and signing profile, then apply them:

```sh
kubectl apply -f configs/samples/truststore.yaml
kubectl apply -f configs/samples/trustpolicy.yaml
```

The trust policy lists the signing profiles you trust:

```yaml
trustedIdentities:
  - "arn:aws:signer:<region>:<account>:/signing-profiles/<profile>"
```

### 3. Apply the Kyverno policy

```sh
kubectl apply -f configs/samples/kyverno-policy.yaml
```

The sample policy applies to Pods in namespace `test-notation`. Its key parts:

```yaml
context:
  - name: tlscerts
    apiCall:
      urlPath: "/api/v1/namespaces/notation-aws-verifier/secrets/notation-aws-verifier-svc.notation-aws-verifier.svc.tls-pair"
      jmesPath: 'base64_decode( data."tls.crt" )'
  - name: response
    apiCall:
      method: POST
      data:
        - key: images
          value: "{{images}}"
      service:
        url: https://notation-aws-verifier-svc.notation-aws-verifier.svc/checkimages
        caBundle: "{{ tlscerts }}"
mutate:
  foreach:
    - list: "response.results"
      patchesJson6902: |-
        - path: '{{ element.path }}'
          op: '{{ element.op }}'
          value: '{{ element.value }}'
```

Kyverno needs permission to read that TLS secret. Grant it with a Role/RoleBinding in the
`notation-aws-verifier` namespace, or use Kyverno's aggregated RBAC.

### 4. Test

```sh
kubectl create namespace test-notation
kubectl -n test-notation run signed --image=$IMAGE --dry-run=server     # admitted, image pinned to digest
kubectl -n test-notation run unsigned --image=<unsigned-image>          # denied: no signature is associated with ...
```

## Features

### Digest mutation

Tags are mutable, so the service returns the verified digest for each image and the
policy rewrites the Pod spec:

```json
{
  "verified": true,
  "results": [
    {
      "op": "replace",
      "path": "/spec/containers/0/image",
      "value": "123456789012.dkr.ecr.us-west-2.amazonaws.com/demo@sha256:4a1c..."
    }
  ]
}
```

### Attestation verification

The policy can pass an `attestations` list to verify signed metadata attached to an image,
with conditions on its content:

```yaml
- key: attestations
  value:
    - imageReference: "*"
      type:
        - name: application/vnd.cyclonedx
          conditions:
            all:
              - key: \{{ element.components[].licenses[].expression }}
                operator: AllNotIn
                value: ["GPL-2.0", "GPL-3.0"]
```

Escape the condition keys with `\` so Kyverno doesn't substitute them before calling the service.

### Multi-tenancy

Several `TrustPolicy`/`TrustStore` resources can exist side by side. The policy chooses
one per request, for example per namespace:

```yaml
- key: trustPolicy
  value: "tp-{{request.namespace}}"
```

Without it, the default from `defaultTrustPolicy` (env `DEFAULT_TRUST_POLICY`) is used.

### Caching

Verification results are cached in memory, keyed by image and trust policy. The cache is
bounded by entry count and cleared whenever a trust policy or trust store changes.

| Flag | Chart value | Default |
| --- | --- | --- |
| `--cacheEnabled` | `cache.enabled` | `true` |
| `--cacheMaxSize` | `cache.maxSize` | `2000` (chart) / `1000` (binary) |
| `--cacheTTLDurationSeconds` | `cache.ttlSeconds` | `7200` (chart) / `3600` (binary) |

### High availability

All replicas serve verification requests. Leader election is used only for managing the
TLS certificate, which all replicas share through a Secret. For production:

```yaml
replicaCount: 3
podDisruptionBudget:
  enabled: true
  maxUnavailable: 1
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: ScheduleAnyway
```

### Other regions (GovCloud, China)

A trust policy can reference several trust stores, one per AWS partition. The
[sample trust store](configs/samples/truststore.yaml) includes the commercial and GovCloud
root certificates. See the
[AWS Signer image verification docs](https://docs.aws.amazon.com/signer/latest/developerguide/image-verification.html).

## Configuration

| Flag | Chart value | Default | Description |
| --- | --- | --- | --- |
| `--logLevel` | `logLevel` | `info` | `trace`, `debug`, `info`, `warn`, `error` |
| `--logFormat` | `logFormat` | `text` | `text` or `json` |
| `--reviewKyvernoToken` | `tokenReview.enabled` | `true` | Authenticate callers with TokenReview |
| `--allowedUsers` | `tokenReview.allowedUsers` | Kyverno admission and reports controllers | Service accounts allowed to call the service |
| `--tokenReviewAudiences` | `tokenReview.audiences` | empty (API server default) | Audiences for TokenReview, e.g. `kyverno-svc.kyverno.io` |
| `--imagePullSecrets` | `deployment.imagePullSecrets` | none | Registry credentials when not using IAM |
| `--maxSignatureAttempts` | `deployment.maxSignatureAttempts` | `30` | Max signature envelopes checked per image |
| `--allowInsecureRegistry` | `deployment.allowInsecureRegistry` | `false` | Not recommended |
| `--debug` | via `extraArgs` | `false` | Notation/plugin debug output; implies `--logLevel=debug` |
| `--version` | | | Print the version and exit |

If Kyverno runs in a namespace other than `kyverno`, or with custom service accounts,
update `tokenReview.allowedUsers`.

## Migrating from kyverno-notation-aws

The CRD API group is unchanged, so existing `TrustPolicy` and `TrustStore` resources keep
working. What changes: image, chart, and the default namespace/service names, so the
Kyverno policy URL and TLS secret path change.

1. Back up your trust resources:
   ```sh
   kubectl get trustpolicies.notation.nirmata.io,truststores.notation.nirmata.io -A -o yaml > notation-trust-backup.yaml
   ```
2. Stop Helm from deleting the CRDs (and your resources) when the old release is removed:
   ```sh
   kubectl annotate crd trustpolicies.notation.nirmata.io truststores.notation.nirmata.io helm.sh/resource-policy=keep --overwrite
   helm uninstall kyverno-notation-aws -n kyverno-notation-aws
   ```
3. Let the new release adopt the CRDs, then install it:
   ```sh
   kubectl annotate crd trustpolicies.notation.nirmata.io truststores.notation.nirmata.io \
     meta.helm.sh/release-name=notation-aws-verifier meta.helm.sh/release-namespace=notation-aws-verifier --overwrite
   kubectl label crd trustpolicies.notation.nirmata.io truststores.notation.nirmata.io app.kubernetes.io/managed-by=Helm --overwrite
   helm install notation-aws-verifier oci://ghcr.io/krax1337/charts/notation-aws-verifier --version 1.0.0 \
     -n notation-aws-verifier --create-namespace --set region=<region>
   ```
4. Update the Kyverno policy to the new service URL and TLS secret path (see Quick start).
5. Move the IAM role binding (Pod Identity association or IRSA annotation) to namespace
   `notation-aws-verifier`, service account `notation-aws-verifier`.

To keep the old names instead, install with `fullnameOverride=kyverno-notation-aws` into the
`kyverno-notation-aws` namespace; the service is then `kyverno-notation-aws-svc`.

Behaviour changes to be aware of:

- Rejected callers now get `401`/`403` instead of `406`.
- The `metadata` request field (the `kyverno-notation-aws.io/verify-images` annotation) no
  longer skips verification. The Pod's creator could set that annotation, so it was a bypass.
- Per-request logs moved from `info` to `debug`.

## Troubleshooting

```sh
kubectl -n notation-aws-verifier logs deploy/notation-aws-verifier -f
```

To call the service by hand, disable token review (`tokenReview.enabled=false`) on a test
cluster, then:

```sh
kubectl run curl --rm -it --image=curlimages/curl -- \
  curl -sk https://notation-aws-verifier-svc.notation-aws-verifier.svc/checkimages -X POST -d \
  '{"images":{"containers":{"app":{"registry":"123456789012.dkr.ecr.us-west-2.amazonaws.com","path":"demo","name":"demo","tag":"v1","jsonPointer":"/spec/containers/0/image"}}}}'
```

Common problems:

- `only loopback hosts are allowed`: you are running the old nirmata image. Use this one.
- `Token is not authenticated`: Kyverno's token audience does not match. Set
  `tokenReview.audiences`, for example to `kyverno-svc.kyverno.io`.
- `Token is not authorized`: the caller isn't in `tokenReview.allowedUsers`.

## Verifying releases

Images and charts are signed keylessly with cosign from the release workflow and carry
SLSA build provenance. See [SECURITY.md](SECURITY.md#verifying-release-artifacts) for the commands.

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for local
development and the release process. Report security issues privately as described in
[SECURITY.md](SECURITY.md).

## License and attribution

Apache License 2.0, see [LICENSE](LICENSE). Originally developed by Nirmata, Inc. as
[kyverno-notation-aws](https://github.com/nirmata/kyverno-notation-aws) and
[kyverno-notation-verifier](https://github.com/nirmata/kyverno-notation-verifier); see [NOTICE](NOTICE).
This project is not affiliated with or endorsed by Nirmata, the Kyverno project, the CNCF,
or AWS.
