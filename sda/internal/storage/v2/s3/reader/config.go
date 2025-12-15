package reader

import (
	"errors"
	"strings"

	"github.com/c2h5oh/datasize"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

type endpointConfig struct {
	AccessKey      string `mapstructure:"access_key"`
	CACert         string `mapstructure:"ca_cert"`
	ChunkSize      string `mapstructure:"chunk_size"`
	ChunkSizeBytes uint64 `mapstructure:"-"`
	Region         string `mapstructure:"region"`
	SecretKey      string `mapstructure:"secret_key"`
	Endpoint       string `mapstructure:"endpoint"`
	DisableHTTPS   bool   `mapstructure:"disable_https"`
}

func loadConfig(backendName string) ([]*endpointConfig, error) {
	var endpointConf []*endpointConfig

	// TODO ideally register these as flags so it could be included in --help, etc for easier usability
	if err := viper.UnmarshalKey(
		"storage.s3."+backendName,
		&endpointConf,
		func(config *mapstructure.DecoderConfig) {
			config.WeaklyTypedInput = true
			config.ZeroFields = true
		},
	); err != nil {
		return nil, err
	}

	for _, e := range endpointConf {
		switch {
		case e.Endpoint == "":
			return nil, errors.New("missing required parameter: endpoint")
		case e.AccessKey == "":
			return nil, errors.New("missing required parameter: accessKey")
		case e.SecretKey == "":
			return nil, errors.New("missing required parameter: secretKey")
		default:
			switch {
			case strings.HasPrefix(e.Endpoint, "http") && !e.DisableHTTPS:
				return nil, errors.New("http scheme in endpoint when using HTTPS")
			case strings.HasPrefix(e.Endpoint, "https") && e.DisableHTTPS:
				return nil, errors.New("https scheme in endpoint when HTTPS is disabled")
			default:
			}

			if e.ChunkSize != "" {
				s, err := datasize.ParseString(e.ChunkSize)
				if err != nil {
					return nil, errors.New("could not parse chunk size as a valid data size")
				}

				if s > 5*datasize.MB {
					e.ChunkSizeBytes = s.Bytes()
				}
			}
		}
	}

	return endpointConf, nil
}
