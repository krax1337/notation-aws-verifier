{{/* vim: set filetype=mustache: */}}

{{- define "notation-aws-verifier.labels.merge" -}}
{{- $labels := dict -}}
{{- range . -}}
  {{- $labels = merge $labels (fromYaml .) -}}
{{- end -}}
{{- with $labels -}}
  {{- toYaml $labels -}}
{{- end -}}
{{- end -}}

{{/* Labels applied to every resource. */}}
{{- define "notation-aws-verifier.labels" -}}
{{- template "notation-aws-verifier.labels.merge" (list
  (include "notation-aws-verifier.labels.common" .)
  (include "notation-aws-verifier.matchLabels" .)
) -}}
{{- end -}}

{{- define "notation-aws-verifier.labels.common" -}}
{{- template "notation-aws-verifier.labels.merge" (list
  (printf "helm.sh/chart: %s" (include "notation-aws-verifier.chart" .))
  (printf "app.kubernetes.io/managed-by: %s" .Release.Service)
  (printf "app.kubernetes.io/part-of: %s" (include "notation-aws-verifier.name" .))
  (printf "app.kubernetes.io/version: %s" (.Chart.AppVersion | replace "+" "_" | quote))
  (toYaml .Values.customLabels)
) -}}
{{- end -}}

{{/* Immutable selector labels. */}}
{{- define "notation-aws-verifier.matchLabels" -}}
app.kubernetes.io/name: {{ include "notation-aws-verifier.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "notation-aws-verifier.labels.component" -}}
app.kubernetes.io/component: {{ . }}
{{- end -}}
