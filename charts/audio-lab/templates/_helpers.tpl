{{/*
Expand the name of the chart.
*/}}
{{- define "audio-lab.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "audio-lab.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "audio-lab.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "audio-lab.labels" -}}
helm.sh/chart: {{ include "audio-lab.chart" . }}
{{ include "audio-lab.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "audio-lab.selectorLabels" -}}
app.kubernetes.io/name: {{ include "audio-lab.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Audio Source selector labels
*/}}
{{- define "audio-lab.audioSourceSelectorLabels" -}}
{{ include "audio-lab.selectorLabels" . }}
app.kubernetes.io/component: audio-source
{{- end }}

{{/*
Audio Relay selector labels
*/}}
{{- define "audio-lab.audioRelaySelectorLabels" -}}
{{ include "audio-lab.selectorLabels" . }}
app.kubernetes.io/component: audio-relay
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "audio-lab.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "audio-lab.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}