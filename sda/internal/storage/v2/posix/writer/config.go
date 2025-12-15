package writer

import (
	"errors"
	"strings"

	"github.com/spf13/viper"
)

func loadConfig(backendName string) ([]string, error) {
	// TODO ideally register these as flags so it could be included in --help, etc for easier usability
	endpoints := viper.GetStringSlice("storage.posix." + backendName)

	for _, e := range endpoints {
		if !strings.HasPrefix(e, "/") {
			return nil, errors.New("posix paths must be absolute")
		}
	}

	return endpoints, nil
}
