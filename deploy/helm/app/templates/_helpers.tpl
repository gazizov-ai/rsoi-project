{{- define "rsoi-service.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "rsoi-service.fullname" -}}
{{- include "rsoi-service.name" . -}}
{{- end -}}
