# notation-aws-verifier

![Version: 1.0.0](https://img.shields.io/badge/Version-1.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v1.0.0](https://img.shields.io/badge/AppVersion-v1.0.0-informational?style=flat-square)

Kyverno extension service that verifies Notation signatures made with AWS Signer and resolves image tags to digests

**Homepage:** <https://github.com/krax1337/notation-aws-verifier>

## Installation

```sh
helm install notation-aws-verifier oci://ghcr.io/krax1337/charts/notation-aws-verifier \
  --version 1.0.0 \
  -n notation-aws-verifier --create-namespace
```

The chart installs the TrustPolicy and TrustStore CRDs (API group `notation.nirmata.io`) unless `crds.install=false`.

## AWS credentials

The service needs an AWS identity with `signer:GetRevocationStatus` and ECR read access
(`ecr:GetAuthorizationToken`, `ecr:BatchGetImage`, `ecr:GetDownloadUrlForLayer`).

- **IRSA**: set `serviceAccount.annotations."eks\.amazonaws\.com/role-arn"`.
- **EKS Pod Identity**: no annotation needed; create a Pod Identity association for the
  `notation-aws-verifier` ServiceAccount in the release namespace.

Set `region` to the region of your registry and signing profiles.

## Calling the service from Kyverno

The service generates a self-signed certificate stored in the Secret
`<service>.<namespace>.svc.tls-pair`. With the default release name and namespace:

- service URL: `https://notation-aws-verifier-svc.notation-aws-verifier.svc/checkimages`
- CA bundle: `/api/v1/namespaces/notation-aws-verifier/secrets/notation-aws-verifier-svc.notation-aws-verifier.svc.tls-pair`, key `tls.crt`

Only requests carrying a token of a user in `tokenReview.allowedUsers` are accepted while `tokenReview.enabled` is true.
If Kyverno mints request tokens for a specific audience, list it in `tokenReview.audiences`.

## High availability

Set `replicaCount` to 2 or more. Every replica serves requests; certificate rotation runs on an elected leader.
A PodDisruptionBudget is created automatically when `replicaCount` is greater than 1.
Spread replicas with `topologySpreadConstraints` or `affinity` (see the commented example in [values.yaml](values.yaml)).

## Requirements

Kubernetes: `>=1.25.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules. Ref: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity |
| cache.enabled | bool | `true` | Cache successful verification results (`--cacheEnabled`) |
| cache.maxSize | int | `2000` | Maximum number of cached entries (`--cacheMaxSize`) |
| cache.ttlSeconds | int | `7200` | Time-to-live of a cache entry in seconds (`--cacheTTLDurationSeconds`) |
| configMap.data | object | `{}` | Plugin config entries. Supported keys: `aws-region`, `aws-profile`, `aws-signer-endpoint-url` |
| configMap.name | string | `"notation-plugin-config"` | Name of the ConfigMap whose data is passed to the AWS Signer plugin as plugin config (`--pluginConfigMap`) |
| crds.install | bool | `true` | Whether to have Helm install the TrustPolicy and TrustStore CRDs (API group `notation.nirmata.io`) |
| customLabels | object | `{}` | User supplied labels applied to all resources (not to selectors) |
| defaultTrustPolicy | string | `"aws-signer-trust-policy"` | Name of the TrustPolicy used when a request does not specify one (`DEFAULT_TRUST_POLICY`) |
| deployment.allowInsecureRegistry | bool | `false` | Allow insecure connections to registries (`--allowInsecureRegistry`). Not recommended. |
| deployment.imagePullSecrets | object | `{}` | Registry credentials used while verifying images, for registries IAM does not cover. A `kubernetes.io/dockerconfigjson` Secret is created for each entry and the names are passed as `--imagePullSecrets`. ECR registries are accessed with the pod's AWS identity and need no entry here. |
| deployment.maxSignatureAttempts | int | `30` | Maximum number of signature envelopes processed per image (`--maxSignatureAttempts`) |
| deployment.updateStrategy | object | See [values.yaml](values.yaml) | Deployment update strategy. Ref: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy |
| extraArgs | list | `[]` | Additional command-line arguments for the container |
| extraEnv | list | `[]` | Additional environment variables for the container |
| fullnameOverride | string | `nil` | Override the expanded name of the chart (used for the Deployment, ServiceAccount, RBAC and Service names) |
| image.digest | string | `nil` | Image digest (e.g. `sha256:...`). Takes precedence over `image.tag` when set |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.pullSecrets | list | `[]` | Names of existing Secrets used to pull the verifier image itself (pod `imagePullSecrets`). Not to be confused with `deployment.imagePullSecrets`, which are the registry credentials used during verification. |
| image.registry | string | `"ghcr.io"` | Image registry |
| image.repository | string | `"krax1337/notation-aws-verifier"` | Image repository |
| image.tag | string | `nil` | Image tag. Defaults to `appVersion` in Chart.yaml if omitted |
| livenessProbe | object | See [values.yaml](values.yaml) | Liveness probe, forwarded as-is to the container. Ref: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/ |
| logFormat | string | `"text"` | Log format passed as `--logFormat`. One of `text`, `json` |
| logLevel | string | `"info"` | Log level passed as `--logLevel`. One of `trace`, `debug`, `info`, `warn`, `error` |
| nameOverride | string | `nil` | Override the name of the chart |
| namespaceOverride | string | `nil` | Override the namespace the chart deploys to |
| nodeSelector | object | `{}` | Node selector. Ref: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector |
| podAnnotations | object | `{}` | Annotations added to the pod |
| podDisruptionBudget.enabled | bool | `false` | Create a PodDisruptionBudget. One is always created when `replicaCount` is greater than 1 |
| podDisruptionBudget.maxUnavailable | int or string | `nil` | Maximum unavailable pods. Takes precedence over `minAvailable` |
| podDisruptionBudget.minAvailable | int | `1` | Minimum available pods. Ignored when `maxUnavailable` is set |
| podLabels | object | `{}` | Labels added to the pod |
| podSecurityContext | object | See [values.yaml](values.yaml) | Pod security context |
| priorityClassName | string | `""` | Pod priority class name |
| readinessProbe | object | See [values.yaml](values.yaml) | Readiness probe, forwarded as-is to the container. Ref: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/ |
| region | string | `"us-west-2"` | AWS region, exported to the container as `AWS_REGION`. Used for ECR credential lookups and, unless `configMap.data.aws-region` is set, by the AWS Signer plugin when checking signing profile revocation. Set it to the region of your registry and signing profiles. |
| replicaCount | int | `1` | Number of replicas. Every replica serves requests and keeps its own copy of the trust policies; certificate rotation runs on a single elected leader. Use 2 or more for high availability. |
| resources | object | See [values.yaml](values.yaml) | Container resource requests and limits |
| revisionHistoryLimit | int | `10` | Number of old ReplicaSets to retain for rollback |
| securityContext | object | See [values.yaml](values.yaml) | Container security context. The root filesystem is read-only; `/notation` and `/tmp` are writable emptyDir volumes. |
| service.annotations | object | `{}` | Service annotations |
| service.port | int | `443` | HTTPS port served by the Service (targets container port 9443) |
| service.type | string | `"ClusterIP"` | Service type |
| serviceAccount.annotations | object | `{}` | Annotations for the ServiceAccount. For IRSA set `eks.amazonaws.com/role-arn`. EKS Pod Identity needs no annotation: create a Pod Identity association for this ServiceAccount in AWS instead. The role needs `signer:GetRevocationStatus` and ECR read access (`ecr:GetAuthorizationToken`, `ecr:BatchGetImage`, `ecr:GetDownloadUrlForLayer`). |
| serviceAccount.enabled | bool | `true` | Create a ServiceAccount. When `false`, `serviceAccount.name` (or `default`) must already exist |
| serviceAccount.name | string | `nil` | The ServiceAccount name. Defaults to the chart fullname |
| startupProbe | object | See [values.yaml](values.yaml) | Startup probe, forwarded as-is to the container. Ref: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/ |
| terminationGracePeriodSeconds | int | `10` | Seconds the pod is given to shut down gracefully |
| test.image.pullPolicy | string | `nil` | Image pull policy. Defaults to `image.pullPolicy` if omitted |
| test.image.registry | string | `nil` | Image registry |
| test.image.repository | string | `"busybox"` | Image repository |
| test.image.tag | string | `"1.38.0"` | Image tag |
| test.resources | object | See [values.yaml](values.yaml) | Test pod resources |
| test.securityContext | object | See [values.yaml](values.yaml) | Test container security context |
| test.sleep | int | `20` | Seconds to wait before probing the service |
| tokenReview.allowedUsers | list | `["system:serviceaccount:kyverno:kyverno-admission-controller","system:serviceaccount:kyverno:kyverno-reports-controller"]` | Users and ServiceAccounts allowed to call the service (`--allowedUsers`) |
| tokenReview.audiences | list | `[]` | Audiences put in the TokenReview (`--tokenReviewAudiences`). Empty uses the API server default audience. Set e.g. `kyverno-svc.kyverno.io` if Kyverno mints request tokens for a specific audience. |
| tokenReview.enabled | bool | `true` | Validate the bearer token of each request with a TokenReview and only accept `allowedUsers` (`--reviewKyvernoToken`). When enabled the ServiceAccount is granted `create` on `tokenreviews`. |
| tolerations | list | `[]` | Tolerations. Ref: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/ |
| topologySpreadConstraints | list | `[]` | Topology spread constraints. `labelSelector` defaults to the pod selector labels when omitted. Ref: https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/ |
