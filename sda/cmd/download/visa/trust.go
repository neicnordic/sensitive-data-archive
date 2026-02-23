package visa

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
)

// LoadTrustedIssuers loads trusted issuer+JKU pairs from a JSON file.
// The file must contain a JSON array of objects with "iss" and "jku" fields.
// Both URLs are normalized before storage.
// If allowInsecureJKU is true, HTTP JKU URLs are permitted (for testing only).
func LoadTrustedIssuers(filePath string, allowInsecureJKU bool) ([]TrustedIssuer, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read trusted issuers file: %w", err)
	}

	var issuers []TrustedIssuer
	if err := json.Unmarshal(data, &issuers); err != nil {
		return nil, fmt.Errorf("failed to parse trusted issuers JSON: %w", err)
	}

	// Validate and normalize
	for i := range issuers {
		if issuers[i].ISS == "" {
			return nil, fmt.Errorf("trusted issuer at index %d has empty 'iss'", i)
		}
		if issuers[i].JKU == "" {
			return nil, fmt.Errorf("trusted issuer at index %d has empty 'jku'", i)
		}

		// Require HTTPS for JKU unless explicitly allowed for testing
		if !allowInsecureJKU && !strings.HasPrefix(issuers[i].JKU, "https://") {
			return nil, fmt.Errorf("trusted issuer at index %d: JKU must use https:// (got %s); set visa.allow-insecure-jku for testing", i, issuers[i].JKU)
		}

		issuers[i].ISS = NormalizeURL(issuers[i].ISS)
		issuers[i].JKU = NormalizeURL(issuers[i].JKU)
	}

	return issuers, nil
}

// IsTrusted checks if the given issuer+JKU combination is in the trusted list.
func IsTrusted(trusted []TrustedIssuer, issuer, jku string) bool {
	normIss := NormalizeURL(issuer)
	normJKU := NormalizeURL(jku)

	for _, t := range trusted {
		if t.ISS == normIss && t.JKU == normJKU {
			return true
		}
	}

	return false
}

// NormalizeURL normalizes a URL by cleaning its path component.
// This ensures consistent comparison of URLs with trailing slashes,
// double slashes, or dot segments.
func NormalizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Path = path.Clean(u.Path)
	if u.Path == "." {
		u.Path = ""
	}

	return u.String()
}
