package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_WithoutDatabase_ReturnsError(t *testing.T) {
	h, err := New()

	assert.Nil(t, h)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database is required")
}

func TestNew_WithDatabase_Success(t *testing.T) {
	mockDB := &mockDatabase{}

	h, err := New(WithDatabase(mockDB))

	require.NoError(t, err)
	assert.NotNil(t, h)
	assert.Equal(t, mockDB, h.db)
}

func TestNew_WithAllOptions(t *testing.T) {
	mockDB := &mockDatabase{}

	h, err := New(
		WithDatabase(mockDB),
		WithGRPCReencryptHost("localhost"),
		WithGRPCReencryptPort(50051),
	)

	require.NoError(t, err)
	assert.NotNil(t, h)
	assert.Equal(t, mockDB, h.db)
	assert.Equal(t, "localhost", h.grpcHost)
	assert.Equal(t, 50051, h.grpcPort)
}
