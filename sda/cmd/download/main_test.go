package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePermissionModel_Valid(t *testing.T) {
	assert.NoError(t, validatePermissionModel("ownership", false))
	assert.NoError(t, validatePermissionModel("visa", true))
	assert.NoError(t, validatePermissionModel("combined", true))
	assert.NoError(t, validatePermissionModel("combined", false))
}

func TestValidatePermissionModel_Invalid(t *testing.T) {
	err := validatePermissionModel("bogus", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid permission.model")
}

func TestValidatePermissionModel_VisaWithoutEnabled(t *testing.T) {
	err := validatePermissionModel("visa", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "visa.enabled is false")
}

func TestValidateProductionConfig_AllValid(t *testing.T) {
	err := validateProductionConfig(productionConfig{
		AllowAllData:   false,
		HMACSecret:     "some-secret",
		GRPCClientCert: "/path/to/cert",
		GRPCClientKey:  "/path/to/key",
	})
	require.NoError(t, err)
}

func TestValidateProductionConfig_AllowAllDataBlocked(t *testing.T) {
	err := validateProductionConfig(productionConfig{
		AllowAllData:   true,
		HMACSecret:     "some-secret",
		GRPCClientCert: "/path/to/cert",
		GRPCClientKey:  "/path/to/key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allow-all-data")
}

func TestValidateProductionConfig_EmptyHMACSecret(t *testing.T) {
	err := validateProductionConfig(productionConfig{
		AllowAllData:   false,
		HMACSecret:     "",
		GRPCClientCert: "/path/to/cert",
		GRPCClientKey:  "/path/to/key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pagination.hmac-secret")
}

func TestValidateProductionConfig_MissingGRPCCert(t *testing.T) {
	err := validateProductionConfig(productionConfig{
		AllowAllData:   false,
		HMACSecret:     "some-secret",
		GRPCClientCert: "",
		GRPCClientKey:  "/path/to/key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grpc.client-cert")
}

func TestValidateProductionConfig_MissingGRPCKey(t *testing.T) {
	err := validateProductionConfig(productionConfig{
		AllowAllData:   false,
		HMACSecret:     "some-secret",
		GRPCClientCert: "/path/to/cert",
		GRPCClientKey:  "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grpc.client-cert")
}
