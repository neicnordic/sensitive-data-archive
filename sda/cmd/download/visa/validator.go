package visa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	log "github.com/sirupsen/logrus"
)

const maxURLClaimLength = 255

// DatasetChecker checks whether a dataset exists in the local database.
type DatasetChecker interface {
	CheckDatasetExists(ctx context.Context, datasetID string) (bool, error)
}

// Validator performs GA4GH visa validation.
type Validator struct {
	config         ValidatorConfig
	trustedIssuers []TrustedIssuer
	jwksCache      *JWKSCache
	userinfoClient *UserinfoClient
	validCache     *ristretto.Cache
	datasetChecker DatasetChecker
}

// NewValidator creates a new visa validator with the given configuration.
func NewValidator(cfg ValidatorConfig, trustedIssuers []TrustedIssuer, dc DatasetChecker) (*Validator, error) {
	jwksCache, err := NewJWKSCache(trustedIssuers, cfg.JWKCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS cache: %w", err)
	}

	userinfoClient, err := NewUserinfoClient(cfg.UserinfoURL, cfg.UserinfoCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo client: %w", err)
	}

	validCache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     10000,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create validation cache: %w", err)
	}

	return &Validator{
		config:         cfg,
		trustedIssuers: trustedIssuers,
		jwksCache:      jwksCache,
		userinfoClient: userinfoClient,
		validCache:     validCache,
		datasetChecker: dc,
	}, nil
}

// UserinfoClient returns the validator's userinfo client for reuse in opaque token authentication.
func (v *Validator) UserinfoClient() *UserinfoClient {
	return v.userinfoClient
}

// VisaResult holds the result of visa processing for a single request.
type VisaResult struct {
	Datasets  []string  // Datasets granted by valid visas
	MinExpiry time.Time // Earliest visa expiry (for cache TTL bounding)
	Warnings  []string  // Non-fatal issues (logged, not returned to client)
}

// GetVisaDatasets retrieves and validates visas, returning granted dataset IDs.
// authSource indicates how the user authenticated ("jwt" or "userinfo").
// For "jwt" auth with source="userinfo", we call userinfo to get visas.
// For opaque tokens (authSource="userinfo"), we already have the userinfo response.
func (v *Validator) GetVisaDatasets(ctx context.Context, identity Identity, rawToken string, authSource string) (*VisaResult, error) {
	// Get passport (visa JWTs)
	passport, err := v.getPassport(rawToken, authSource)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve passport: %w", err)
	}

	if len(passport) == 0 {
		return &VisaResult{}, nil
	}

	return v.processVisas(ctx, identity, passport)
}

// getPassport retrieves visa JWTs from the configured source.
func (v *Validator) getPassport(rawToken string, authSource string) ([]string, error) {
	switch v.config.Source {
	case "userinfo":
		// Always call userinfo for visa retrieval (GA4GH compliant)
		userinfo, err := v.userinfoClient.FetchUserinfo(rawToken)
		if err != nil {
			return nil, err
		}

		return userinfo.Passport, nil

	case "token":
		// For opaque tokens, must use userinfo regardless
		if authSource == "userinfo" {
			userinfo, err := v.userinfoClient.FetchUserinfo(rawToken)
			if err != nil {
				return nil, err
			}

			return userinfo.Passport, nil
		}

		// For JWT tokens in "token" mode, parse visas from token claims.
		// Userinfo overrides if also called (per GA4GH spec precedence).
		token, err := jwt.Parse([]byte(rawToken), jwt.WithVerify(false))
		if err != nil {
			return nil, fmt.Errorf("failed to parse token for visa claims: %w", err)
		}

		claims := token.PrivateClaims()
		passportClaim, ok := claims["ga4gh_passport_v1"]
		if !ok {
			return nil, nil
		}

		passportSlice, ok := passportClaim.([]any)
		if !ok {
			return nil, errors.New("ga4gh_passport_v1 claim is not an array")
		}

		passport := make([]string, 0, len(passportSlice))
		for _, v := range passportSlice {
			s, ok := v.(string)
			if !ok {
				continue
			}
			passport = append(passport, s)
		}

		return passport, nil

	default:
		return nil, fmt.Errorf("unknown visa source: %s", v.config.Source)
	}
}

// processVisas validates individual visa JWTs and extracts dataset grants.
func (v *Validator) processVisas(ctx context.Context, identity Identity, passport []string) (*VisaResult, error) {
	result := &VisaResult{}
	seen := make(map[string]bool)
	jkuTracker := make(map[string]bool)
	identities := make(map[string]bool) // Track {iss, sub} pairs for multi-identity detection

	for i, visaJWT := range passport {
		// Enforce max visas limit
		if i >= v.config.MaxVisas {
			result.Warnings = append(result.Warnings, fmt.Sprintf("stopped processing after %d visas (max-visas limit)", v.config.MaxVisas))
			log.Warnf("stopped processing after %d visas (max-visas limit)", v.config.MaxVisas)

			break
		}

		// Enforce max visa size
		if len(visaJWT) > v.config.MaxVisaSize {
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipped visa %d: size %d exceeds max %d", i, len(visaJWT), v.config.MaxVisaSize))
			log.Warnf("skipped visa %d: size %d exceeds max %d", i, len(visaJWT), v.config.MaxVisaSize)

			continue
		}

		// Check validation cache
		visaHash := hashToken(visaJWT)
		if cached, found := v.validCache.Get(visaHash); found {
			if datasets, ok := cached.([]string); ok {
				for _, ds := range datasets {
					if !seen[ds] {
						seen[ds] = true
						result.Datasets = append(result.Datasets, ds)
					}
				}

				continue
			}
		}

		datasets, expiry, err := v.validateSingleVisa(ctx, identity, visaJWT, jkuTracker, identities)
		if err != nil {
			log.Debugf("visa %d rejected: %v", i, err)

			continue
		}

		// Track earliest expiry for cache TTL bounding
		if !expiry.IsZero() && (result.MinExpiry.IsZero() || expiry.Before(result.MinExpiry)) {
			result.MinExpiry = expiry
		}

		// Cache validated visa result
		cacheTTL := v.config.ValidationCacheTTL
		if !expiry.IsZero() {
			remaining := time.Until(expiry)
			if remaining < cacheTTL {
				cacheTTL = remaining
			}
		}
		if cacheTTL > 0 {
			v.validCache.SetWithTTL(visaHash, datasets, 1, cacheTTL)
		}

		for _, ds := range datasets {
			if !seen[ds] {
				seen[ds] = true
				result.Datasets = append(result.Datasets, ds)
			}
		}
	}

	// Multi-identity detection
	if len(identities) > 1 {
		msg := fmt.Sprintf("passport contains visas from %d distinct {iss, sub} pairs", len(identities))
		result.Warnings = append(result.Warnings, msg)
		log.Warn(msg)
	}

	return result, nil
}

// validateSingleVisa validates one visa JWT and returns granted datasets and expiry.
func (v *Validator) validateSingleVisa(ctx context.Context, identity Identity, visaJWT string, jkuTracker map[string]bool, identities map[string]bool) ([]string, time.Time, error) {
	var zeroTime time.Time

	// 1. Parse JWS header to get JKU
	msg, err := jws.Parse([]byte(visaJWT))
	if err != nil {
		return nil, zeroTime, fmt.Errorf("failed to parse visa JWS: %w", err)
	}

	sigs := msg.Signatures()
	if len(sigs) == 0 {
		return nil, zeroTime, errors.New("visa has no signatures")
	}

	jkuURL := sigs[0].ProtectedHeaders().JWKSetURL()
	if jkuURL == "" {
		return nil, zeroTime, errors.New("visa missing jku header")
	}

	// 2. Parse unverified token to get issuer (needed for trust check)
	unverified, err := jwt.Parse([]byte(visaJWT), jwt.WithVerify(false))
	if err != nil {
		return nil, zeroTime, fmt.Errorf("failed to parse visa claims: %w", err)
	}

	visaIssuer := unverified.Issuer()
	visaSubject := unverified.Subject()

	// Track identity for multi-identity detection
	identityKey := visaIssuer + "\x00" + visaSubject
	identities[identityKey] = true

	// 3. Get JWKS from trusted JKU (enforces allowlist)
	keySet, err := v.jwksCache.GetKeySet(visaIssuer, jkuURL, jkuTracker, v.config.MaxJWKSPerReq)
	if err != nil {
		return nil, zeroTime, fmt.Errorf("failed to get JWKS: %w", err)
	}

	// 4. Verify visa signature
	verified, err := jwt.Parse([]byte(visaJWT),
		jwt.WithKeySet(keySet, jws.WithInferAlgorithmFromKey(true), jws.WithRequireKid(false)),
		jwt.WithAcceptableSkew(v.config.ClockSkew),
		jwt.WithValidate(true),
	)
	if err != nil {
		return nil, zeroTime, fmt.Errorf("visa signature/claims validation failed: %w", err)
	}

	// 5. Identity binding check
	if err := v.checkIdentityBinding(identity, visaIssuer, visaSubject); err != nil {
		return nil, zeroTime, err
	}

	// 6. Extract and validate ga4gh_visa_v1 claim
	visaClaim, err := extractVisaClaim(verified)
	if err != nil {
		return nil, zeroTime, err
	}

	// 7. Only process ControlledAccessGrants
	if visaClaim.Type != "ControlledAccessGrants" {
		// Unknown visa types are silently ignored (GA4GH compliant)
		return nil, zeroTime, nil
	}

	// 8. Validate ControlledAccessGrants requirements
	if err := v.validateControlledAccessGrant(visaClaim); err != nil {
		return nil, zeroTime, err
	}

	// 9. Extract dataset ID
	datasetID := ExtractDatasetID(visaClaim.Value, v.config.DatasetIDMode)
	if datasetID == "" {
		return nil, zeroTime, errors.New("empty dataset ID extracted from visa value")
	}

	// 10. Verify dataset exists locally
	exists, err := v.datasetChecker.CheckDatasetExists(ctx, datasetID)
	if err != nil {
		log.Debugf("dataset existence check failed for %s: %v", datasetID, err)

		return nil, zeroTime, nil
	}
	if !exists {
		log.Debugf("visa dataset %s not found locally", datasetID)

		return nil, zeroTime, nil
	}

	return []string{datasetID}, verified.Expiration(), nil
}

// checkIdentityBinding enforces the configured identity binding mode.
func (v *Validator) checkIdentityBinding(identity Identity, visaIssuer, visaSubject string) error {
	switch v.config.IdentityMode {
	case "broker-bound":
		// Accept all visas from trusted issuers (no identity check)
		return nil
	case "strict-sub":
		if visaSubject != identity.Subject {
			return fmt.Errorf("visa subject %q does not match authenticated subject %q", visaSubject, identity.Subject)
		}

		return nil
	case "strict-iss-sub":
		if visaIssuer != identity.Issuer || visaSubject != identity.Subject {
			return fmt.Errorf("visa {iss, sub} {%q, %q} does not match authenticated {%q, %q}",
				visaIssuer, visaSubject, identity.Issuer, identity.Subject)
		}

		return nil
	default:
		return fmt.Errorf("unknown identity mode: %s", v.config.IdentityMode)
	}
}

// extractVisaClaim extracts and parses the ga4gh_visa_v1 claim from a validated token.
func extractVisaClaim(token jwt.Token) (*VisaClaim, error) {
	claims := token.PrivateClaims()
	raw, ok := claims["ga4gh_visa_v1"]
	if !ok {
		return nil, errors.New("visa missing ga4gh_visa_v1 claim")
	}

	// Marshal and unmarshal to get a typed struct
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal visa claim: %w", err)
	}

	var vc VisaClaim
	if err := json.Unmarshal(data, &vc); err != nil {
		return nil, fmt.Errorf("failed to parse visa claim: %w", err)
	}

	return &vc, nil
}

// validateControlledAccessGrant validates GA4GH ControlledAccessGrants requirements.
func (v *Validator) validateControlledAccessGrant(vc *VisaClaim) error {
	// by claim must exist and be non-empty
	if vc.By == "" {
		return errors.New("ControlledAccessGrants visa missing 'by' claim")
	}

	// value must be valid URL-claim (max 255 chars)
	if len(vc.Value) > maxURLClaimLength {
		return fmt.Errorf("visa value exceeds max length (%d > %d)", len(vc.Value), maxURLClaimLength)
	}
	if vc.Value == "" {
		return errors.New("ControlledAccessGrants visa missing 'value' claim")
	}

	// source must be valid URL-claim
	if vc.Source == "" {
		return errors.New("ControlledAccessGrants visa missing 'source' claim")
	}
	if len(vc.Source) > maxURLClaimLength {
		return fmt.Errorf("visa source exceeds max length (%d > %d)", len(vc.Source), maxURLClaimLength)
	}

	// asserted is REQUIRED for ControlledAccessGrants (GA4GH requirement)
	if vc.Asserted <= 0 {
		return errors.New("ControlledAccessGrants visa missing or invalid 'asserted' claim")
	}

	// asserted must be <= now (if validation enabled)
	if v.config.ValidateAsserted {
		assertedTime := time.Unix(vc.Asserted, 0)
		if assertedTime.After(time.Now().Add(v.config.ClockSkew)) {
			return fmt.Errorf("visa asserted timestamp is in the future: %v", assertedTime)
		}
	}

	// conditions: if present and non-empty, reject
	if vc.Conditions != nil {
		if err := rejectNonEmptyConditions(vc.Conditions); err != nil {
			return err
		}
	}

	return nil
}

// rejectNonEmptyConditions rejects visas with non-empty conditions.
func rejectNonEmptyConditions(conditions any) error {
	switch c := conditions.(type) {
	case []any:
		if len(c) > 0 {
			return errors.New("visa has non-empty conditions (not supported)")
		}
	case map[string]any:
		if len(c) > 0 {
			return errors.New("visa has non-empty conditions (not supported)")
		}
	default:
		// Non-nil, non-empty conditions of unknown type
		return errors.New("visa has conditions of unsupported type")
	}

	return nil
}

// ExtractDatasetID extracts a dataset ID from a visa value based on the configured mode.
func ExtractDatasetID(value string, mode string) string {
	if mode == "raw" {
		return value
	}

	// mode == "suffix": extract last segment from URL/URN
	value = strings.TrimSuffix(value, "/")

	// For URLs, use proper parsing
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		if u, err := url.Parse(value); err == nil {
			return path.Base(u.Path)
		}
	}

	// For URNs or paths, extract last segment
	if idx := strings.LastIndex(value, "/"); idx >= 0 {
		return value[idx+1:]
	}
	if idx := strings.LastIndex(value, ":"); idx >= 0 {
		return value[idx+1:]
	}

	return value
}
