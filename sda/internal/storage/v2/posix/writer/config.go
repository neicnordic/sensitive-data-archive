package writer

import (
	"context"
	"errors"
	"strings"

	"github.com/c2h5oh/datasize"
	"github.com/go-viper/mapstructure/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/spf13/viper"
)

type endpointConfig struct {
	Path         string `mapstructure:"path"`
	MaxObjects   uint64 `mapstructure:"max_objects"`
	MaxSize      string `mapstructure:"max_size"`
	MaxSizeBytes uint64 `mapstructure:"-"`
}

func loadConfig(backendName string) ([]*endpointConfig, error) {
	var endpointConf []*endpointConfig

	// TODO ideally register these as flags so it could be included in --help, etc for easier usability
	if err := viper.UnmarshalKey(
		"storage.posix."+backendName,
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
		if e.MaxSize != "" {
			byteSize, err := datasize.ParseString(e.MaxSize)
			if err != nil {
				return nil, errors.New("could not parse maxsize as a valid data size")
			}
			e.MaxSizeBytes = byteSize.Bytes()
		}
	}

	return endpointConf, nil
}

func (endpointConf *endpointConfig) isUsable(ctx context.Context, locationBroker locationbroker.LocationBroker) (bool, error) {
	count, err := locationBroker.GetObjectCount(ctx, endpointConf.Path)
	if err != nil {
		return false, err
	}
	if count >= endpointConf.MaxObjects && endpointConf.MaxObjects > 0 {
		return false, nil
	}

	size, err := locationBroker.GetSize(ctx, endpointConf.Path)
	if err != nil {
		return false, err
	}
	if size >= endpointConf.MaxSizeBytes && endpointConf.MaxSizeBytes > 0 {
		return false, err
	}

	return true, nil
}
