{{/*
Expand the name of the chart.
*/}}
{{- define "kbind-provider-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Truncate at 63 chars because some Kubernetes name fields are limited to this.
If release name contains the chart name it will be used as-is.
*/}}
{{- define "kbind-provider-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart label value (name + version).
*/}}
{{- define "kbind-provider-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "kbind-provider-operator.labels" -}}
helm.sh/chart: {{ include "kbind-provider-operator.chart" . }}
{{ include "kbind-provider-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "kbind-provider-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kbind-provider-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "kbind-provider-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "kbind-provider-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Kubeconfig mount path derived from the configured key.
*/}}
{{- define "kbind-provider-operator.kubeconfigPath" -}}
{{- printf "/etc/kbind/%s" .Values.kcpKubeconfigKey }}
{{- end }}
