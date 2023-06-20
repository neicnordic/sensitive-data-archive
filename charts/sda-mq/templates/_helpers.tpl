{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "sda.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Expand the name of the chart.
*/}}
{{- define "sda.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "sda.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "verifyPeer" -}}
  {{ if and .Values.global.tls.enabled .Values.global.tls.verifyPeer }}
    {{-  print "verify_peer" -}}
  {{ else }}
    {{-  print "verify_none" -}}
  {{ end }}
{{- end -}}

{{- define "mqCert" -}}
    {{- if .Values.externalPkiService.tlsPath }}
        {{- printf "%s" (regexReplaceAll "^/*|/+" (printf "%s/%s" .Values.externalPkiService.tlsPath .Values.global.tls.certName) "/")}}
    {{- else if or .Values.global.tls.clusterIssuer .Values.global.tls.issuer }}
        {{- print "/etc/rabbitmq/tls/tls.crt" -}}
    {{- else }}
        {{- printf "/etc/rabbitmq/tls/%s" (required "name of tls certificate is required" .Values.global.tls.certName) }}
    {{- end -}}
{{- end -}}

{{- define "mqKey" -}}
    {{- if .Values.externalPkiService.tlsPath }}
        {{- printf "%s" (regexReplaceAll "^/*|/+" (printf "%s/%s" .Values.externalPkiService.tlsPath .Values.global.tls.keyName) "/")}}
    {{- else if or .Values.global.tls.clusterIssuer .Values.global.tls.issuer }}
        {{- printf "/etc/rabbitmq/tls/tls.key" -}}
    {{- else }}
        {{- printf "/etc/rabbitmq/tls/%s" (required "name of tls key is required" .Values.global.tls.keyName) }}
    {{- end -}}
{{- end -}}

{{- define "caCert" -}}
    {{- if .Values.externalPkiService.tlsPath }}
        {{- printf "%s" (regexReplaceAll "^/*|/+" (printf "%s/%s" .Values.externalPkiService.tlsPath .Values.global.tls.caCert) "/")}}
    {{- else if or .Values.global.tls.clusterIssuer .Values.global.tls.issuer }}
        {{- printf "/etc/rabbitmq/tls/ca.crt" -}}
    {{- else }}
        {{- printf "/etc/rabbitmq/tls/%s" (required "name of ca file is required" .Values.global.tls.caCert) }}
    {{- end -}}
{{- end -}}

{{- define "TLSissuer" -}}
    {{- if and .Values.global.tls.clusterIssuer .Values.global.tls.issuer }}
        {{- fail "Only one of global.tls.issuer or global.tls.clusterIssuer should be set" }}
    {{- end -}}

    {{- if and .Values.global.tls.issuer }}
        {{- printf "%s" .Values.global.tls.issuer }}
    {{- else if and .Values.global.tls.clusterIssuer }}
        {{- printf "%s" .Values.global.tls.clusterIssuer }}
    {{- end -}}
{{- end -}}

{{- define "TLSsecret" -}}
    {{- if and .Values.global.tls.enabled (not .Values.externalPkiService.tlsPath) }}
        {{- if and (not .Values.global.tls.issuer) (not .Values.global.tls.clusterIssuer) }}
            {{ printf "%s" (required "TLS secret name is required when TLS in enabled without issuer or PKI service" .Values.global.tls.secretName) }}
        {{- else }}
            {{- printf "%s-certs" (include "sda.fullname" .) }}
        {{- end -}}
    {{- end -}}
{{- end -}}

{{- define "testTLSsecret" -}}
    {{- if and .Values.global.tls.enabled (not .Values.externalPkiService.tlsPath) }}
        {{- if and (not .Values.global.tls.issuer) (not .Values.global.tls.clusterIssuer) }}
            {{ printf "%s" (required "TLS secret name is required when TLS in enabled without issuer or PKI service" .Values.testimage.tls.secretName) }}
        {{- else }}
            {{- printf "%s-test-certs" (include "sda.fullname" .) }}
        {{- end -}}
    {{- end -}}
{{- end -}}

{{- define "adminPass" -}}
    {{- if .Values.global.adminPassword }}
        {{- printf "%s" (.Values.global.adminPassword ) | b64enc }}
    {{- else }}
        {{- randAlphaNum 32 | b64enc }}
    {{- end -}}
{{- end -}}