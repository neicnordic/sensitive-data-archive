package inboxpath

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configure seeds viper the way a service's config.yaml does, then loads. It covers the whole chain
// the resolver depends on: nested YAML -> dotted flag key -> validated package state.
func configure(t *testing.T, yaml string) error {
	t.Helper()
	prevCode, prevDelimiter := projectCode, projectDelimiter
	t.Cleanup(func() {
		projectCode, projectDelimiter = prevCode, prevDelimiter
		viper.Reset()
	})

	viper.Reset()
	viper.SetConfigType("yaml")
	require.NoError(t, viper.ReadConfig(bytes.NewBufferString(yaml)))

	return Load()
}

func withProjectCode(t *testing.T, code, delimiter string) {
	t.Helper()
	require.NoError(t, configure(t, fmt.Sprintf(
		"inboxpath:\n  project_code: %q\n  project_delimiter: %q\n", code, delimiter)))
}

func TestLoad_readsNestedKeys(t *testing.T) {
	require.NoError(t, configure(t, "inboxpath:\n  project_code: p11\n  project_delimiter: \"-\"\n"))
	assert.Equal(t, "p11", projectCode)
	assert.Equal(t, "-", projectDelimiter)
}

func TestLoad_absentSectionIsStock(t *testing.T) {
	// With no inboxpath section the resolver keeps stock SDA behavior, so deployments that omit it
	// (e.g. the Swedish node) are unaffected.
	require.NoError(t, configure(t, ""))
	assert.Equal(t, "", projectCode)
	assert.Equal(t, "", projectDelimiter)
	assert.Equal(t, "test.user_demo.org/files/x.raw.enc",
		ResolveInboxPath("files/x.raw.enc", "test.user@demo.org"))
}

func TestLoad_halfConfigured_errors(t *testing.T) {
	// A project code without a delimiter would silently glue code and username together
	// ("p11username"); a delimiter without a code would be silently ignored. Both are
	// misconfigurations that must fail at startup, not produce garbage paths at runtime.
	err := configure(t, "inboxpath:\n  project_code: p11\n")
	assert.ErrorContains(t, err, "must be set together")

	err = configure(t, "inboxpath:\n  project_delimiter: \"-\"\n")
	assert.ErrorContains(t, err, "must be set together")
}

func TestLoad_projectCodeMustBeSingleSegment(t *testing.T) {
	// The code prefixes ONE per-user directory name. Path separators would silently split it into
	// multiple segments ("p11/" -> "p11/-user/..."), and whitespace or control characters produce
	// directory names no deployment intends. Free text otherwise: codes are deployment-specific.
	load := func(code string) error {
		return configure(t, fmt.Sprintf(
			"inboxpath:\n  project_code: %q\n  project_delimiter: \"-\"\n", code))
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
		return configure(t, fmt.Sprintf(
			"inboxpath:\n  project_code: p11\n  project_delimiter: %q\n", delimiter))
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
	// The registered defaults are what a deployment with no inboxpath section gets, so they must be
	// empty: any other default would change the inbox layout for every existing node.
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

func TestResolveInboxPath_stockDefault_prependsNormalizedUser(t *testing.T) {
	// With no project code the first "@" becomes "_" and the username directory is prepended
	// unless the path already starts with it. These assertions keep existing deployments pinned.
	require.NoError(t, configure(t, ""))
	user := "test.user@demo.org"
	assert.Equal(t, "test.user_demo.org/files/x.raw.enc",
		ResolveInboxPath("files/x.raw.enc", user))
	// Already-prefixed path is returned unchanged.
	assert.Equal(t, "test.user_demo.org/files/x.raw.enc",
		ResolveInboxPath("test.user_demo.org/files/x.raw.enc", user))
}

func TestResolveInboxPath_stockDefault_leadingSeparator(t *testing.T) {
	// A leading "/" is harmless: the userDir prefix is still applied, refuting the assumption that
	// filepath.Join treats a leading "/" as absolute and drops the prefix.
	require.NoError(t, configure(t, ""))
	assert.Equal(t, "test.user_demo.org/files/x.raw.enc",
		ResolveInboxPath("/files/x.raw.enc", "test.user@demo.org"))
}

func TestResolveInboxPath_stockDefault_matchesJoinOfNormalizedUser(t *testing.T) {
	require.NoError(t, configure(t, ""))
	filePath := "main_folder/sub_folder/file.name"
	userName := "test.user@demo.org"
	assert.Equal(t, filepath.Join(strings.Replace(userName, "@", "_", 1), filePath),
		ResolveInboxPath(filePath, userName))
}

func TestResolveInboxPath_projectCode_reconstructsRawUserDir(t *testing.T) {
	// FEGA-Norway: anonymized "/files/x" is rebuilt under the project-code-prefixed RAW username
	// (the "@" is not normalized to "_" when a project code is configured).
	withProjectCode(t, "p11", "-")
	assert.Equal(t, "p11-dummy@elixir-europe.org/files/x.raw.enc",
		ResolveInboxPath("/files/x.raw.enc", "dummy@elixir-europe.org"))
}

func TestResolveInboxPath_projectCode_alreadyPrefixed_unchanged(t *testing.T) {
	// An already-resolved path (e.g. on reprocessing) is returned as-is, not double-prefixed.
	withProjectCode(t, "p11", "-")
	fp := "p11-dummy@elixir-europe.org/files/x.raw.enc"
	assert.Equal(t, fp, ResolveInboxPath(fp, "dummy@elixir-europe.org"))
}

func TestResolveInboxPath_projectCode_leadingSeparator_notDoubled(t *testing.T) {
	// Older proxy formats can send an already-resolved path with a leading "/". The leading
	// separator must be normalized away, not cause the user directory to be prepended a second time
	// ("/p11-user/files/x" -> "p11-user/p11-user/files/x").
	withProjectCode(t, "p11", "-")
	assert.Equal(t, "p11-dummy@elixir-europe.org/files/x.raw.enc",
		ResolveInboxPath("/p11-dummy@elixir-europe.org/files/x.raw.enc", "dummy@elixir-europe.org"))
	// All leading separators are stripped, so repeated slashes cannot sneak past the
	// already-resolved check either.
	assert.Equal(t, "p11-dummy@elixir-europe.org/files/x.raw.enc",
		ResolveInboxPath("//p11-dummy@elixir-europe.org/files/x.raw.enc", "dummy@elixir-europe.org"))
}

func TestResolveInboxPath_projectCode_segmentBoundary(t *testing.T) {
	// The already-resolved check holds on a path-segment boundary: "p11-user2/..." belongs to a
	// different user and must not be treated as already under the "p11-user" directory.
	withProjectCode(t, "p11", "-")
	assert.Equal(t, "p11-user/p11-user2/files/x.raw.enc",
		ResolveInboxPath("p11-user2/files/x.raw.enc", "user"))
}
