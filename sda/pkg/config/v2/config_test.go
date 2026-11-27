package config

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestRegisterFlags(t *testing.T) {
	// Save and restore state
	oldFlags := registeredFlags
	defer func() { registeredFlags = oldFlags }()
	registeredFlags = nil

	RegisterFlags(
		&Flag{
			Name: "test.flag",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "default", "Test flag")
			},
			Required: false,
			AssignFunc: func(_ string) {
				// noop for this test
			},
		},
	)

	assert.Len(t, registeredFlags, 1)
	assert.Equal(t, "test.flag", registeredFlags[0].Name)
}

// Note: Tests for Load() are skipped because cobra.Command.Execute() modifies
// global state that cannot be easily reset between tests. The Load() function
// is tested via integration tests instead.
//
// The following scenarios are covered by integration tests:
// - Missing required flags
// - Optional flags with defaults
// - Environment variable binding
// - Config file loading
