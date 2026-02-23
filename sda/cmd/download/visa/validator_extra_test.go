//go:build visas
// +build visas

package visa

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVisa_UntrustedIssuerRejected(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD100")
	validator, identity, rawToken := setupValidatorWithUntrustedIssuer(t, visaClaim)

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestVisa_StrictSub_RejectsMismatchedSubject(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD101")
	validator, identity, rawToken := setupValidatorWithIdentityMode(t, visaClaim, "strict-sub", "other-subject", "https://broker.example")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestVisa_StrictIssSub_RejectsMismatchedIssuer(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD102")
	validator, identity, rawToken := setupValidatorWithIdentityMode(t, visaClaim, "strict-iss-sub", "user-123", "https://different-issuer.example")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestVisa_UnknownTypeIgnored(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD103")
	visaClaim["type"] = "AffiliationAndRole"

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestVisa_ValueExceedsMaxLengthRejected(t *testing.T) {
	longValue := strings.Repeat("a", 256)
	visaClaim := baseVisaClaim(longValue)

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestVisa_SourceExceedsMaxLengthRejected(t *testing.T) {
	longSource := strings.Repeat("b", 256)
	visaClaim := baseVisaClaim("EGAD104")
	visaClaim["source"] = longSource

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestVisa_MultiIdentityWarning(t *testing.T) {
	priv, pub, kid := newRSAKeyPair(t)
	jwksServer := newJWKSServer(t, pub)
	t.Cleanup(jwksServer.Close)

	issuerA := "https://issuer-a.example"
	issuerB := "https://issuer-b.example"
	jku := jwksServer.URL

	visaA := signVisaJWT(t, priv, jku, kid, issuerA, "user-123", baseVisaClaim("DS1"), time.Now().Add(time.Hour))
	visaB := signVisaJWT(t, priv, jku, kid, issuerB, "user-456", baseVisaClaim("DS2"), time.Now().Add(time.Hour))

	validator := setupValidatorWithPassports(t, []string{visaA, visaB}, []TrustedIssuer{
		{ISS: issuerA, JKU: jku},
		{ISS: issuerB, JKU: jku},
	}, map[string]bool{"DS1": true, "DS2": true}, ValidatorConfig{
		Source:             "userinfo",
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   time.Minute,
	})

	identity := Identity{Issuer: "https://broker.example", Subject: "user-123"}
	result, err := validator.GetVisaDatasets(context.Background(), identity, "opaque-token", "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.ElementsMatch(t, []string{"DS1", "DS2"}, result.Datasets)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, containsWarning(result.Warnings, "distinct {iss, sub} pairs"))
}

func TestVisa_MaxJWKSPerRequestEnforced(t *testing.T) {
	privA, pubA, kidA := newRSAKeyPair(t)
	privB, pubB, kidB := newRSAKeyPair(t)

	jwksServerA := newJWKSServer(t, pubA)
	t.Cleanup(jwksServerA.Close)
	jwksServerB := newJWKSServer(t, pubB)
	t.Cleanup(jwksServerB.Close)

	issuerA := "https://issuer-a.example"
	issuerB := "https://issuer-b.example"

	visaA := signVisaJWT(t, privA, jwksServerA.URL, kidA, issuerA, "user-123", baseVisaClaim("DSA"), time.Now().Add(time.Hour))
	visaB := signVisaJWT(t, privB, jwksServerB.URL, kidB, issuerB, "user-123", baseVisaClaim("DSB"), time.Now().Add(time.Hour))

	validator := setupValidatorWithPassports(t, []string{visaA, visaB}, []TrustedIssuer{
		{ISS: issuerA, JKU: jwksServerA.URL},
		{ISS: issuerB, JKU: jwksServerB.URL},
	}, map[string]bool{"DSA": true, "DSB": true}, ValidatorConfig{
		Source:             "userinfo",
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      1,
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   time.Minute,
	})

	identity := Identity{Issuer: "https://broker.example", Subject: "user-123"}
	result, err := validator.GetVisaDatasets(context.Background(), identity, "opaque-token", "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, len(result.Datasets))
	assert.Equal(t, "DSA", result.Datasets[0])
}

func TestVisa_MaxVisasLimit(t *testing.T) {
	priv, pub, kid := newRSAKeyPair(t)
	jwksServer := newJWKSServer(t, pub)
	t.Cleanup(jwksServer.Close)

	issuer := "https://issuer-maxvisas.example"
	jku := jwksServer.URL

	passports := make([]string, 0, 201)
	existing := make(map[string]bool, 201)
	for i := 0; i < 201; i++ {
		dataset := fmt.Sprintf("DS%03d", i)
		visaJWT := signVisaJWT(t, priv, jku, kid, issuer, "user-123", baseVisaClaim(dataset), time.Now().Add(time.Hour))
		passports = append(passports, visaJWT)
		existing[dataset] = true
	}

	validator := setupValidatorWithPassports(t, passports, []TrustedIssuer{{ISS: issuer, JKU: jku}}, existing, ValidatorConfig{
		Source:             "userinfo",
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   time.Minute,
	})

	identity := Identity{Issuer: "https://broker.example", Subject: "user-123"}
	result, err := validator.GetVisaDatasets(context.Background(), identity, "opaque-token", "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Datasets, 200)
}

func TestVisa_FailedJKUFetchCountsAgainstLimit(t *testing.T) {
	// First JKU server returns 500 (simulates transient failure)
	failingServer := newFailingJWKSServer(t)
	t.Cleanup(failingServer.Close)

	// Second JKU server works correctly
	privB, pubB, kidB := newRSAKeyPair(t)
	workingServer := newJWKSServer(t, pubB)
	t.Cleanup(workingServer.Close)

	issuerA := "https://issuer-failing.example"
	issuerB := "https://issuer-working.example"

	// Sign visa A with a throwaway key (JKU will fail before verification)
	privA, _, _ := newRSAKeyPair(t)
	visaA := signVisaJWT(t, privA, failingServer.URL, "kid-a", issuerA, "user-123", baseVisaClaim("DSA"), time.Now().Add(time.Hour))
	visaB := signVisaJWT(t, privB, workingServer.URL, kidB, issuerB, "user-123", baseVisaClaim("DSB"), time.Now().Add(time.Hour))

	validator := setupValidatorWithPassports(t, []string{visaA, visaB}, []TrustedIssuer{
		{ISS: issuerA, JKU: failingServer.URL},
		{ISS: issuerB, JKU: workingServer.URL},
	}, map[string]bool{"DSA": true, "DSB": true}, ValidatorConfig{
		Source:             "userinfo",
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      1, // Only 1 distinct JKU allowed
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   time.Minute,
	})

	identity := Identity{Issuer: "https://broker.example", Subject: "user-123"}
	result, err := validator.GetVisaDatasets(context.Background(), identity, "opaque-token", "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)

	// With the fix: failed JKU fetch counts against limit, blocking second visa's different JKU.
	// Without the fix: failed fetch wouldn't count, allowing the second visa's JKU fetch to succeed.
	assert.Empty(t, result.Datasets, "failed JKU fetch should count against per-request limit, preventing second JKU fetch")
}

func TestVisa_MaxVisaSizeLimit(t *testing.T) {
	priv, pub, kid := newRSAKeyPair(t)
	jwksServer := newJWKSServer(t, pub)
	t.Cleanup(jwksServer.Close)

	issuer := "https://issuer-maxsize.example"
	jku := jwksServer.URL

	largeValue := strings.Repeat("x", 2000)
	visaClaim := baseVisaClaim(largeValue)
	visaJWT := signVisaJWT(t, priv, jku, kid, issuer, "user-123", visaClaim, time.Now().Add(time.Hour))

	validator := setupValidatorWithPassports(t, []string{visaJWT}, []TrustedIssuer{{ISS: issuer, JKU: jku}}, map[string]bool{}, ValidatorConfig{
		Source:             "userinfo",
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        512,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   time.Minute,
	})

	identity := Identity{Issuer: "https://broker.example", Subject: "user-123"}
	result, err := validator.GetVisaDatasets(context.Background(), identity, "opaque-token", "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func setupValidatorWithPassports(t *testing.T, passports []string, trusted []TrustedIssuer, existing map[string]bool, cfg ValidatorConfig) *Validator {
	t.Helper()

	userinfoServer := newUserinfoServer(t, passports)
	t.Cleanup(userinfoServer.Close)

	cfg.UserinfoURL = userinfoServer.URL

	checker := &fakeDatasetChecker{existing: existing}

	validator, err := NewValidator(cfg, trusted, checker)
	require.NoError(t, err)

	return validator
}

func setupValidatorWithUntrustedIssuer(t *testing.T, visaClaim map[string]interface{}) (*Validator, Identity, string) {
	t.Helper()

	priv, pub, kid := newRSAKeyPair(t)
	jwksServer := newJWKSServer(t, pub)
	t.Cleanup(jwksServer.Close)

	issuer := "https://trusted-issuer.example"
	visaJWT := signVisaJWT(t, priv, jwksServer.URL, kid, issuer, "user-123", visaClaim, time.Now().Add(time.Hour))

	cfg := ValidatorConfig{
		Source:             "userinfo",
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   time.Minute,
	}

	validator := setupValidatorWithPassports(t, []string{visaJWT}, []TrustedIssuer{
		{ISS: "https://different-issuer.example", JKU: jwksServer.URL},
	}, map[string]bool{ExtractDatasetID(visaClaim["value"].(string), "raw"): true}, cfg)

	identity := Identity{Issuer: "https://broker.example", Subject: "user-123"}

	return validator, identity, "opaque-token"
}

func setupValidatorWithIdentityMode(t *testing.T, visaClaim map[string]interface{}, mode, subject, issuer string) (*Validator, Identity, string) {
	t.Helper()

	priv, pub, kid := newRSAKeyPair(t)
	jwksServer := newJWKSServer(t, pub)
	t.Cleanup(jwksServer.Close)

	visaIssuer := "https://visa-issuer.example"
	visaJWT := signVisaJWT(t, priv, jwksServer.URL, kid, visaIssuer, "user-123", visaClaim, time.Now().Add(time.Hour))

	cfg := ValidatorConfig{
		Source:             "userinfo",
		DatasetIDMode:      "raw",
		IdentityMode:       mode,
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   time.Minute,
	}

	validator := setupValidatorWithPassports(t, []string{visaJWT}, []TrustedIssuer{
		{ISS: visaIssuer, JKU: jwksServer.URL},
	}, map[string]bool{ExtractDatasetID(visaClaim["value"].(string), "raw"): true}, cfg)

	identity := Identity{Issuer: issuer, Subject: subject}

	return validator, identity, "opaque-token"
}

func containsWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}

	return false
}
