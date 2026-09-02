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

	var writerEndpoints []*endpointConfig

	for _, e := range endpointConf {
		// A writer_disabled entry is the config for a reader. Writing to it is
		// not supported, and deleting counts as a write, so it is dropped here
		// and the writer never sees it.
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

		writerEndpoints = append(writerEndpoints, e)
	}

	return writerEndpoints, nil
}

func (endpointConf *endpointConfig) isUsable(ctx context.Context, backendName string, locationBroker locationbroker.LocationBroker) (bool, error) {
	count, err := locationBroker.GetObjectCount(ctx, backendName, endpointConf.Path)
	if err != nil {
		return false, err
	}
	if count >= endpointConf.MaxObjects && endpointConf.MaxObjects > 0 {
		return false, nil
	}

	size, err := locationBroker.GetSize(ctx, backendName, endpointConf.Path)
	if err != nil {
		return false, err
	}
	if size >= endpointConf.maxSizeBytes && endpointConf.maxSizeBytes > 0 {
		return false, nil
	}

	return true, nil
}
