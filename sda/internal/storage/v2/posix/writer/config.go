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
	Path           string `mapstructure:"path"`
	MaxObjects     uint64 `mapstructure:"max_objects"`
	MaxSize        string `mapstructure:"max_size"`
	maxSizeBytes   uint64
	WriterDisabled bool `mapstructure:"writer_disabled"`
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
		if e.WriterDisabled {
			continue
		}
		if !strings.HasPrefix(e.Path, "/") {
			return nil, errors.New("posix paths must be absolute")
		}
		if e.MaxSize != "" {
			byteSize, err := datasize.ParseString(e.MaxSize)
			if err != nil {
				return nil, errors.New("could not parse maxsize as a valid data size")
			}
			e.maxSizeBytes = byteSize.Bytes()
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
	if size >= endpointConf.maxSizeBytes && endpointConf.maxSizeBytes > 0 {
		return false, nil
	}

	return true, nil
}
