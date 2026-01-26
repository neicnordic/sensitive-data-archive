package reader

import (
	"errors"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

type endpointConfig struct {
	Path string `mapstructure:"path"`
}

func loadConfig(backendName string) ([]*endpointConfig, error) {
	var endpointConf []*endpointConfig

	if err := viper.UnmarshalKey(
		"storage."+backendName+".posix",
		&endpointConf,
		func(config *mapstructure.DecoderConfig) {
			config.WeaklyTypedInput = true
			config.ZeroFields = true
		},
	); err != nil {
		return nil, err
	}

	for _, e := range endpointConf {
		if !strings.HasPrefix(e.Path, "/") {
			return nil, errors.New("posix paths must be absolute")
		}
	}

	return endpointConf, nil
}
