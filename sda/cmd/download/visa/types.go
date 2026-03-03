// Package visa provides GA4GH visa validation for the download service.
// It implements ControlledAccessGrants visa processing per the GA4GH Passport spec.
package visa

import "time"

// Identity represents an authenticated user's identity as an {issuer, subject} pair.
type Identity struct {
	Issuer  string
	Subject string
}

// TrustedIssuer represents an allowed (issuer, JKU) pair for visa verification.
type TrustedIssuer struct {
	ISS string `json:"iss"`
	JKU string `json:"jku"`
}

// VisaClaim represents the ga4gh_visa_v1 claim within a visa JWT.
type VisaClaim struct {
	Type       string `json:"type"`
	Asserted   int64  `json:"asserted"`
	Value      string `json:"value"`
	Source     string `json:"source"`
	By         string `json:"by"`
	Conditions any    `json:"conditions,omitempty"`
}

// UserinfoResponse represents the relevant fields from an OIDC userinfo response.
type UserinfoResponse struct {
	Sub      string   `json:"sub"`
	Passport []string `json:"ga4gh_passport_v1"`
}

// ValidatorConfig holds configuration for the visa validator.
type ValidatorConfig struct {
	Source           string // "userinfo" or "token"
	UserinfoURL      string // Userinfo endpoint URL
	DatasetIDMode    string // "raw" or "suffix"
	IdentityMode     string // "broker-bound", "strict-sub", or "strict-iss-sub"
	ValidateAsserted bool
	ClockSkew        time.Duration

	// Limits
	MaxVisas      int
	MaxJWKSPerReq int
	MaxVisaSize   int

	// Cache TTLs
	JWKCacheTTL        time.Duration
	ValidationCacheTTL time.Duration
	UserinfoCacheTTL   time.Duration
}

// DefaultConfig returns a ValidatorConfig with sensible defaults.
func DefaultConfig() ValidatorConfig {
	return ValidatorConfig{
		Source:             "userinfo",
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          30 * time.Second,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        16384,
		JWKCacheTTL:        5 * time.Minute,
		ValidationCacheTTL: 2 * time.Minute,
		UserinfoCacheTTL:   1 * time.Minute,
	}
}
