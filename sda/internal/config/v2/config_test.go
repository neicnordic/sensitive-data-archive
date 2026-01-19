package config

import (
	"os"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestRegisterFlags(t *testing.T) {
	// Reset state
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

func TestLoad_MissingRequiredFlag(t *testing.T) {
	// Reset state
	registeredFlags = nil
	viper.Reset()

	RegisterFlags(
		&Flag{
			Name: "required.flag",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Required flag")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				// noop
			},
		},
	)

	// Set os.Args to avoid parsing actual command line
	oldArgs := os.Args
	os.Args = []string{"test"}
	defer func() { os.Args = oldArgs }()

	err := Load()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required flag: required.flag")
}

func TestLoad_WithOptionalFlag(t *testing.T) {
	// Reset state
	registeredFlags = nil
	viper.Reset()

	var testValue string

	RegisterFlags(
		&Flag{
			Name: "optional.test",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "default-value", "Optional test flag")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				testValue = viper.GetString(flagName)
			},
		},
	)

	// Set os.Args to avoid parsing actual command line
	oldArgs := os.Args
	os.Args = []string{"test"}
	defer func() { os.Args = oldArgs }()

	err := Load()
	assert.NoError(t, err)
	// Should use default value since no env var or flag is set
	assert.Equal(t, "default-value", testValue)
}
