// Package inboxproject configures how a deployment names its per-user inbox directories.
//
// Deployments that namespace each inbox directory by project code (e.g. FEGA-Norway's
// "p11-dummy@elixir-europe.org/files/...") set storage.inbox.projectCode and
// storage.inbox.projectCodeDelimiter. Importing this package registers both flags with config/v2,
// so configv2.Load fills them; Load then validates them into a Config for helper.ResolveInboxPath.
// An absent section is stock SDA behavior.
package inboxproject

import (
	"errors"
	"fmt"
	"unicode"

	"github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	codeKey      = "storage.inbox.projectCode"
	delimiterKey = "storage.inbox.projectCodeDelimiter"
)

// assigned records that config/v2 has registered and loaded these keys. The values themselves are
// read in Load rather than captured here: mapper populates viper in two phases, configv2.Load
// followed by the legacy config.NewConfig, and only the latter honours the CONFIGFILE env var. A
// value captured at assign time would miss a config file that the legacy loader reads afterwards.
var assigned bool

var flags = []*config.Flag{
	{
		Name: codeKey,
		RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
			flagSet.String(flagName, "", `Project code prefixing every per-user inbox directory, e.g. "p11". Empty selects the stock layout, where the directory is the normalized username`)
		},
		Required: false,
		AssignFunc: func(_ string) {
			assigned = true
		},
	},
	{
		Name: delimiterKey,
		RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
			flagSet.String(flagName, "", `Character joining the project code to the username in an inbox directory name, one of "-", "_" or "."`)
		},
		Required: false,
		AssignFunc: func(_ string) {
			assigned = true
		},
	},
}

func init() {
	config.RegisterFlags(flags...)
}

// Config describes how a deployment names its per-user inbox directories, so an anonymized
// submission path (the user prefix stripped) can be resolved back to the physical inbox-relative
// path. An empty Code selects stock SDA behavior.
type Config struct {
	// Code optionally prefixes the per-user inbox directory (e.g. "p11"). Empty keeps the
	// stock layout, where the directory is the normalized username.
	Code string
	// Delimiter separates Code from the username (e.g. "-"). Unused when Code is empty.
	Delimiter string
}

// Load reads the inbox layout keys from viper and returns them validated. It must be called after
// configv2.Load and after any later config load, since it reads whatever viper holds at the moment
// it is called.
//
// The error when configv2.Load has not run is the one missing-configuration case this can detect.
// It proves the flags are registered and bound; it does not prove every config source has been
// read. A caller that loads more config afterwards still gets the zero Config here, and the zero
// Config is a valid stock layout, so it comes back with no error.
//
// An absent section yields the zero Config, which is stock SDA behavior, so deployments that omit
// it are unaffected. Code and delimiter join with the username into ONE inbox directory name, which
// is why the two must be set together and why neither may carry a character that would corrupt or
// split that name.
func Load() (Config, error) {
	if !assigned {
		return Config{}, errors.New("inboxproject.Load called before config/v2 assigned the inbox flags")
	}

	cfg := Config{Code: viper.GetString(codeKey), Delimiter: viper.GetString(delimiterKey)}

	switch {
	case cfg.Code == "" && cfg.Delimiter == "":
		return cfg, nil
	case cfg.Code == "" || cfg.Delimiter == "":
		return Config{}, fmt.Errorf("%s and %s must be set together", codeKey, delimiterKey)
	}

	switch cfg.Delimiter {
	case "-", "_", ".":
	default:
		return Config{}, fmt.Errorf("%s %q is not a delimiter character (use \"-\", \"_\" or \".\")", delimiterKey, cfg.Delimiter)
	}

	for _, r := range cfg.Code {
		if r == '/' || r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return Config{}, fmt.Errorf("%s %q must not contain path separators or whitespace", codeKey, cfg.Code)
		}
	}

	return cfg, nil
}
