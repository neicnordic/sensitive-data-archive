{{- if . }}
{{- range . }}
## Target `{{ escapeXML .Target }}`
{{- if (eq (len .Vulnerabilities) 0) }}
### No Vulnerabilities found
{{- else }}
### Vulnerabilities ({{ len .Vulnerabilities }})
| Package | ID | Severity | Installed Version | Fixed Version | Title |
| -------- | ---- | -------- | ---------------- | ------------ | ---- |
    {{- range .Vulnerabilities }}
| `{{ escapeXML .PkgName }}` | [{{ escapeXML .VulnerabilityID }}]({{ escapeXML .PrimaryURL }}) | {{ escapeXML .Severity }} | {{ escapeXML .InstalledVersion }} | {{ escapeXML .FixedVersion }} | {{ escapeXML .Title }} |
    {{- end }}

{{- end }}
{{- end }}
{{- else }}
## Trivy Returned Empty Report
{{- end }}