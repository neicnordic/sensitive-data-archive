// Package inboxpath reconstructs the physical inbox-relative path of an anonymized submission
// path, following how a deployment names its per-user inbox directories.
//
// Importing the package registers inboxpath.project_code and inboxpath.project_delimiter with
// config/v2. Services call Load once at startup, after configv2.Load, and then resolve paths
// without carrying any configuration of their own. A deployment that sets neither key gets the
// stock layout, where the directory is the normalized username.
package inboxpath

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	codeKey      = "inboxpath.project_code"
	delimiterKey = "inboxpath.project_delimiter"
)

var (
	projectCode      string
	projectDelimiter string
)

var flags = []*config.Flag{
	{
		Name: codeKey,
		RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
			flagSet.String(flagName, "", `Project code prefixing every per-user inbox directory, e.g. "p11". Empty selects the stock layout, where the directory is the normalized username`)
		},
		Required: false,
		AssignFunc: func(flagName string) {
			projectCode = viper.GetString(flagName)
		},
	},
	{
		Name: delimiterKey,
		RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
			flagSet.String(flagName, "", `Character joining the project code to the username in an inbox directory name, one of "-", "_" or "."`)
		},
		Required: false,
		AssignFunc: func(flagName string) {
			projectDelimiter = viper.GetString(flagName)
		},
	},
}

func init() {
	config.RegisterFlags(flags...)
}

// Load reads the inbox layout keys and validates them, and is the single entry point for doing so:
// a service calls it once at startup and every later ResolveInboxPath call uses what it read.
//
// Setting neither key is the stock layout and is not an error. Code and delimiter join with the
// username into ONE inbox directory name, which is why the two must be set together and why
// neither may carry a character that would corrupt or split that name.
func Load() error {
	projectCode = viper.GetString(codeKey)
	projectDelimiter = viper.GetString(delimiterKey)

	switch {
	case projectCode == "" && projectDelimiter == "":
		return nil
	case projectCode == "" || projectDelimiter == "":
		return fmt.Errorf("%s and %s must be set together", codeKey, delimiterKey)
	}

	switch projectDelimiter {
	case "-", "_", ".":
	default:
		return fmt.Errorf("%s %q is not a delimiter character (use \"-\", \"_\" or \".\")", delimiterKey, projectDelimiter)
	}

	for _, r := range projectCode {
		if r == '/' || r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%s %q must not contain path separators or whitespace", codeKey, projectCode)
		}
	}

	return nil
}

// ResolveInboxPath reconstructs the physical inbox-relative path for an anonymized submission
// filePath.
//
// With no project code it is the stock round-trip: normalize the username, first "@" -> "_", and
// prepend "<user>/".
//
// With a project code it rebuilds "<code><delimiter><username>/<filePath>" using the RAW username.
// In this branch an already-resolved path (e.g. on reprocessing) is returned as-is, with any
// leading separator normalized away. Whether to normalize is derived from the project code rather
// than configured separately: a project code denotes a TSD-style inbox namespaced by project (e.g.
// FEGA-Norway's "p11-dummy@elixir-europe.org/files/..."), which stores the username verbatim. No
// current deployment needs a project code together with normalization; add an explicit toggle if
// one does.
func ResolveInboxPath(filePath, username string) string {
	if projectCode == "" {
		return unanonymize(filePath, username)
	}

	userDir := projectCode + projectDelimiter + username
	// Tolerate leading separators from older proxy formats (e.g. "/p11-user/files/..."); without
	// stripping them the prefix check below misses and userDir gets prepended a second time.
	relPath := strings.TrimLeft(filePath, "/")
	// Treat as already-resolved only on a path-segment boundary, so "p11-user2/..." is not mistaken
	// for the "p11-user" directory. A submission path always has a file component, so relPath is
	// never the bare userDir.
	if strings.HasPrefix(relPath, userDir+"/") {
		return relPath
	}

	return filepath.Join(userDir, relPath)
}

func unanonymize(filePath, username string) string {
	userDir := strings.Replace(username, "@", "_", 1)
	if strings.HasPrefix(filePath, userDir) {
		return filePath
	}

	return filepath.Join(userDir, filePath)
}
