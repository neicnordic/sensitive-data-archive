package reader

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/c2h5oh/datasize"
	"github.com/go-viper/mapstructure/v2"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type endpointConfig struct {
	AccessKey      string `mapstructure:"access_key"`
	CACert         string `mapstructure:"ca_cert"`
	ChunkSize      string `mapstructure:"chunk_size"`
	chunkSizeBytes uint64
	Region         string `mapstructure:"region"`
	SecretKey      string `mapstructure:"secret_key"`
	Endpoint       string `mapstructure:"endpoint"`
	BucketPrefix   string `mapstructure:"bucket_prefix"`
	DisableHTTPS   bool   `mapstructure:"disable_https"`

	s3Client *s3.Client // cached s3 client for this endpoint, created by getS3Client
}

func loadConfig(backendName string) ([]*endpointConfig, error) {
	var endpointConf []*endpointConfig

	if err := viper.UnmarshalKey(
		"storage."+backendName+".s3",
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
			return nil, errors.New("missing required parameter: access_key")
		case e.SecretKey == "":
			return nil, errors.New("missing required parameter: secret_key")
		default:
			switch {
			case strings.HasPrefix(e.Endpoint, "http") && !e.DisableHTTPS:
				return nil, errors.New("http scheme in endpoint when using HTTPS")
			case strings.HasPrefix(e.Endpoint, "https") && e.DisableHTTPS:
				return nil, errors.New("https scheme in endpoint when HTTPS is disabled")
			default:
			}

			e.chunkSizeBytes = 50 * 1024 * 1024
			if e.ChunkSize != "" {
				s, err := datasize.ParseString(e.ChunkSize)
				if err != nil {
					return nil, errors.New("could not parse chunk_size as a valid data size")
				}
				if s < 5*datasize.MB {
					return nil, errors.New("chunk_size can not be smaller than 5mb")
				}
				if s > 1*datasize.GB {
					return nil, errors.New("chunk_size can not be bigger than 1gb")
				}
			}
			if e.Region == "" {
				e.Region = "us-east-1"
			}
		}
	}

	return endpointConf, nil
}

func (endpointConf *endpointConfig) getS3Client(ctx context.Context) (*s3.Client, error) {
	if endpointConf.s3Client != nil {
		return endpointConf.s3Client, nil
	}

	transport, err := endpointConf.transportConfigS3()
	if err != nil {
		return nil, fmt.Errorf("failed to config s3 transport, due to: %v", err)
	}
	s3cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(endpointConf.AccessKey, endpointConf.SecretKey, "")),
		config.WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load default S3 config, due to: %v", err)
	}

	endpoint := endpointConf.Endpoint
	switch {
	case !strings.HasPrefix(endpoint, "http") && endpointConf.DisableHTTPS:
		endpoint = "http://" + endpoint
	case !strings.HasPrefix(endpoint, "https") && !endpointConf.DisableHTTPS:
		endpoint = "https://" + endpoint
	default:
	}

	endpointConf.s3Client = s3.NewFromConfig(
		s3cfg,
		func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.EndpointOptions.DisableHTTPS = endpointConf.DisableHTTPS
			o.Region = endpointConf.Region
			o.UsePathStyle = true
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		},
	)

	return endpointConf.s3Client, nil
}

// transportConfigS3 is a helper method to setup TLS for the S3 client.
func (endpointConf *endpointConfig) transportConfigS3() (http.RoundTripper, error) {
	cfg := new(tls.Config)

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Warnf("failed to read system CAs: %v, using an empty pool as base", err)
		systemCAs = x509.NewCertPool()
	}
	cfg.RootCAs = systemCAs

	if endpointConf.CACert != "" {
		caCert, err := os.ReadFile(endpointConf.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs, due to: %v", caCert, err)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(caCert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	return &http.Transport{
		TLSClientConfig:   cfg,
		ForceAttemptHTTP2: true,
	}, nil
}
