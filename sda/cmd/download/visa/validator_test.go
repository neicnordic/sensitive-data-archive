//go:build visas

package visa

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetVisaDatasets_UserinfoFlow(t *testing.T) {
	visaValue := "EGAD001"
	visaClaim := baseVisaClaim(visaValue)

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []string{visaValue}, result.Datasets)
}

func TestGetVisaDatasets_RejectsMissingBy(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD002")
	delete(visaClaim, "by")

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestGetVisaDatasets_RejectsConditions(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD003")
	visaClaim["conditions"] = []string{"consent"}

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestGetVisaDatasets_RejectsFutureAsserted(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD004")
	visaClaim["asserted"] = time.Now().Add(1 * time.Hour).Unix()

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets)
}

func TestGetVisaDatasets_RejectsMissingAsserted(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD004")
	delete(visaClaim, "asserted") // Remove asserted field entirely

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets, "visa without asserted claim should be rejected")
}

func TestGetVisaDatasets_RejectsZeroAsserted(t *testing.T) {
	visaClaim := baseVisaClaim("EGAD004")
	visaClaim["asserted"] = 0 // Explicit zero

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "raw")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Datasets, "visa with asserted=0 should be rejected")
}

func TestGetVisaDatasets_SuffixExtraction(t *testing.T) {
	visaValue := "https://example.org/datasets/EGAD005?foo=bar"
	visaClaim := baseVisaClaim(visaValue)

	validator, identity, rawToken := setupValidatorWithVisa(t, visaClaim, "suffix")

	result, err := validator.GetVisaDatasets(context.Background(), identity, rawToken, "userinfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []string{"EGAD005"}, result.Datasets)
}

func baseVisaClaim(value string) map[string]any {
	return map[string]any{
		"type":       "ControlledAccessGrants",
		"by":         "https://example.org/issuer",
		"value":      value,
		"source":     "https://example.org/source",
		"asserted":   time.Now().Add(-1 * time.Hour).Unix(),
		"conditions": []string{},
	}
}

func setupValidatorWithVisa(t *testing.T, visaClaim map[string]any, datasetIDMode string) (*Validator, Identity, string) {
	t.Helper()

	priv, pub, kid := newRSAKeyPair(t)
	jwksServer := newJWKSServer(t, pub)
	t.Cleanup(jwksServer.Close)

	issuer := "https://visa-issuer.example"
	jku := jwksServer.URL
	visaJWT := signVisaJWT(t, priv, jku, kid, issuer, "user-123", visaClaim, time.Now().Add(1*time.Hour))

	userinfoServer := newUserinfoServer(t, []string{visaJWT})
	t.Cleanup(userinfoServer.Close)

	cfg := ValidatorConfig{
		Source:             "userinfo",
		UserinfoURL:        userinfoServer.URL,
		DatasetIDMode:      datasetIDMode,
		ValidateAsserted:   true,
		IdentityMode:       "broker-bound",
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        5 * time.Minute,
		ValidationCacheTTL: 2 * time.Minute,
		UserinfoCacheTTL:   1 * time.Minute,
	}

	checker := &fakeDatasetChecker{existing: map[string]bool{
		ExtractDatasetID(visaClaim["value"].(string), datasetIDMode): true,
	}}

	trustedIssuers := []TrustedIssuer{
		{
			ISS: issuer,
			JKU: jku,
		},
	}

	validator, err := NewValidator(cfg, trustedIssuers, checker)
	require.NoError(t, err)

	identity := Identity{Issuer: "https://broker.example", Subject: "user-123"}

	return validator, identity, "opaque-token"
}
