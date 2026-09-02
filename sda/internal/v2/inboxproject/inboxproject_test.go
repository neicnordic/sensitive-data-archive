package inboxproject

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assign runs the registered AssignFuncs, which is what configv2.Load does. Calling configv2.Load
// itself is not an option here: it runs cobra Execute, which parses os.Args and so picks up the
// test binary's own flags.
func assign(t *testing.T) {
	t.Helper()
	prev := assigned
	t.Cleanup(func() { assigned = prev })

	for _, flag := range flags {
		flag.AssignFunc(flag.Name)
	}
}

// configure seeds viper from a storage.inbox section, the way a service's config.yaml does, then
// assigns and loads. It covers the whole chain the resolver depends on: nested YAML -> dotted flag
// key -> validated Config.
func configure(t *testing.T, storageInbox string) (Config, error) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigType("yaml")
	require.NoError(t, viper.ReadConfig(bytes.NewBufferString(storageInbox)))
	assign(t)

	return Load()
}

func TestLoad_readsNestedStorageInboxKeys(t *testing.T) {
	got, err := configure(t, "storage:\n  inbox:\n    projectCode: p11\n    projectCodeDelimiter: \"-\"\n")
	assert.NoError(t, err)
	assert.Equal(t, Config{Code: "p11", Delimiter: "-"}, got)
}

func TestLoad_absentSectionIsStock(t *testing.T) {
	// With no storage.inbox section Load yields the zero Config, which helper.ResolveInboxPath
	// treats as stock SDA behavior, so deployments that omit it (e.g. the Swedish node) are
	// unaffected.
	got, err := configure(t, "")
	assert.NoError(t, err)
	assert.Equal(t, Config{}, got)
}

func TestLoad_beforeAssignment_errors(t *testing.T) {
	// An unassigned Config is indistinguishable from a deliberate stock layout, so loading before
	// configv2.Load must fail rather than hand back a project-code deployment's inbox paths
	// silently reverted to the stock layout.
	prev := assigned
	t.Cleanup(func() { assigned = prev })
	assigned = false

	_, err := Load()
	assert.ErrorContains(t, err, "called before config/v2")
}

// mapper reads its config file through the legacy loader, which runs AFTER configv2.Load. Keys that
// only arrive with that second read must still reach Load, so the values cannot be captured when
// the flags are assigned. Regression guard: capturing at assign time silently drops FEGA-Norway's
// project code and reverts mapper to the stock inbox layout.
func TestLoad_readsValuesArrivingAfterAssignment(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	assign(t)

	viper.SetConfigType("yaml")
	require.NoError(t, viper.ReadConfig(bytes.NewBufferString(
		"storage:\n  inbox:\n    projectCode: p11\n    projectCodeDelimiter: \"-\"\n")))

	got, err := Load()
	assert.NoError(t, err)
	assert.Equal(t, Config{Code: "p11", Delimiter: "-"}, got)
}

func TestLoad_halfConfigured_errors(t *testing.T) {
	// A project code without a delimiter would silently glue code and username together
	// ("p11username"); a delimiter without a code would be silently ignored. Both are
	// misconfigurations that must fail at startup, not produce garbage paths at runtime.
	_, err := configure(t, "storage:\n  inbox:\n    projectCode: p11\n")
	assert.ErrorContains(t, err, "must be set together")

	_, err = configure(t, "storage:\n  inbox:\n    projectCodeDelimiter: \"-\"\n")
	assert.ErrorContains(t, err, "must be set together")
}

func TestLoad_projectCodeMustBeSingleSegment(t *testing.T) {
	// The code prefixes ONE per-user directory name. Path separators would silently split it into
	// multiple segments ("p11/" -> "p11/-user/..."), and whitespace or control characters produce
	// directory names no deployment intends. Free text otherwise: codes are deployment-specific.
	load := func(code string) error {
		_, err := configure(t, fmt.Sprintf("storage:\n  inbox:\n    projectCode: %q\n    projectCodeDelimiter: \"-\"\n", code))

		return err
	}

	for _, bad := range []string{"p11/", "/p11", "p\\11", "p 11", "p\t11", "p\n11", "p\x0011", "p\x1b11"} {
		assert.ErrorContains(t, load(bad), "must not contain",
			"project code %q should be rejected", bad)
	}
	for _, good := range []string{"p11", "fega-no", "P11.x"} {
		assert.NoError(t, load(good), "project code %q should be accepted", good)
	}
}

func TestLoad_delimiterMustBeSeparatorCharacter(t *testing.T) {
	// The delimiter joins code and username into ONE inbox directory name ("p11-user"), so only the
	// allowlisted separators "-", "_" and "." are accepted. Letters and digits would blur the
	// code/username boundary, "/" would split the directory across two path segments, and
	// whitespace or control characters would produce miserable directory names.
	load := func(delimiter string) error {
		_, err := configure(t, fmt.Sprintf("storage:\n  inbox:\n    projectCode: p11\n    projectCodeDelimiter: %q\n", delimiter))

		return err
	}

	for _, bad := range []string{"x", "Q", "1", "/", "\\", " ", "\t", "\n", "+", "★", "--", "p11"} {
		assert.ErrorContains(t, load(bad), "not a delimiter character",
			"delimiter %q should be rejected", bad)
	}
	for _, good := range []string{"-", "_", "."} {
		assert.NoError(t, load(good), "delimiter %q should be accepted", good)
	}
}

func TestRegisteredFlags_defaultToStock(t *testing.T) {
	// The registered defaults are what a deployment with no storage.inbox section gets, so they
	// must be empty: any other default would change the inbox layout for every existing node.
	flagSet := pflag.NewFlagSet("test", pflag.ContinueOnError)
	for _, flag := range flags {
		flag.RegisterFunc(flagSet, flag.Name)
	}

	for _, name := range []string{codeKey, delimiterKey} {
		registered := flagSet.Lookup(name)
		require.NotNil(t, registered, "flag %q should be registered", name)
		assert.Equal(t, "", registered.DefValue)
	}
}
