{{/* vim: set filetype=mustache: */}}

{{/*
Renders <registry>/<repository>:<tag>, or <registry>/<repository>@<digest> when a digest is set.
Expects a dict with keys "image" (registry, repository, tag, digest) and "defaultTag".
*/}}
{{- define "notation-aws-verifier.image" -}}
{{- $repository := required "An image repository is required" .image.repository -}}
{{- if .image.registry -}}
  {{- $repository = printf "%s/%s" .image.registry $repository -}}
{{- end -}}
{{- if .image.digest -}}
  {{- printf "%s@%s" $repository .image.digest -}}
{{- else -}}
  {{- $tag := .image.tag | default .defaultTag -}}
  {{- if not (typeIs "string" $tag) -}}
    {{- fail "Image tags must be strings." -}}
  {{- end -}}
  {{- printf "%s:%s" $repository $tag -}}
{{- end -}}
{{- end -}}
