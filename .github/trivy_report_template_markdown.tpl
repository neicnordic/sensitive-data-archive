{{- if . }}
{{- $found := false }}
{{- range . }}
{{- if .Vulnerabilities }}
{{- $found = true }}
## Target `{{ escapeXML .Target }}`
### Vulnerabilities ({{ len .Vulnerabilities }})
| Package | ID | Severity | Installed Version | Fixed Version | Title |
| -------- | ---- | -------- | ---------------- | ------------ | ---- |
    {{- range .Vulnerabilities }}
| `{{ escapeXML .PkgName }}` | [{{ escapeXML .VulnerabilityID }}]({{ escapeXML .PrimaryURL }}) | {{ escapeXML .Severity }} | {{ escapeXML .InstalledVersion }} | {{ escapeXML .FixedVersion }} | {{ escapeXML .Title }} |
    {{- end }}

{{- end }}
{{- end }}
{{- if not $found }}
### No Vulnerabilities found
{{- end }}
{{- else }}
## Trivy Returned Empty Report
{{- end }}