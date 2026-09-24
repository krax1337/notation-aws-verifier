{{/* vim: set filetype=mustache: */}}

{{- define "notation-aws-verifier.test.labels" -}}
{{- template "notation-aws-verifier.labels.merge" (list
  (include "notation-aws-verifier.labels.common" .)
  (include "notation-aws-verifier.labels.component" "test")
) -}}
{{- end -}}

{{- define "notation-aws-verifier.test.annotations" -}}
helm.sh/hook: test
helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded
{{- end -}}

{{- define "notation-aws-verifier.test.image" -}}
{{- template "notation-aws-verifier.image" (dict "image" .Values.test.image "defaultTag" "latest") -}}
{{- end -}}

{{- define "notation-aws-verifier.test.imagePullPolicy" -}}
{{- default .Values.image.pullPolicy .Values.test.image.pullPolicy -}}
{{- end -}}
