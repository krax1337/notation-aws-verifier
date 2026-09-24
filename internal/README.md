# internal

The packages below `internal/` started as a copy of
[nirmata/kyverno-notation-verifier](https://github.com/nirmata/kyverno-notation-verifier)
v1.1.0, which is no longer maintained. It is licensed under the Apache License 2.0
(see `LICENSE` at the repository root) and was inlined here so this project can fix
and update it. Files changed since then carry a `Modified by krax1337, 2026` note.

| Package | Origin |
| --- | --- |
| `kubenotation` | `kubenotation` (controller-runtime manager) |
| `kubenotation/api/v1alpha1` | `kubenotation/api/v1alpha1` (TrustPolicy/TrustStore types, API group `notation.nirmata.io` kept for compatibility) |
| `kubenotation/controller` | `kubenotation/internal/controller` |
| `kubenotation/utils` | `kubenotation/utils` |
| `cache` | `pkg/cache` |
| `notationfactory` | `pkg/notationfactory` |
| `types` | `pkg/types` |
| `setup` | `setup` and `setup/internal` |
| `verifier` | `verifier` and `verifier/internal` |

The envtest based controller suite (`kubenotation/internal/controller/suite_test.go`)
was not carried over: it only bootstrapped a kube-apiserver and contained no test cases.
