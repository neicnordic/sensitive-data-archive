package writer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/c2h5oh/datasize"
	"github.com/go-viper/mapstructure/v2"
	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type endpointConfig struct {
	AccessKey      string `mapstructure:"access_key"`
	BucketPrefix   string `mapstructure:"bucket_prefix"`
	CACert         string `mapstructure:"ca_cert"`
	ChunkSize      string `mapstructure:"chunk_size"`
	ChunkSizeBytes uint64 `mapstructure:"-"`
	MaxBuckets     int    `mapstructure:"max_buckets"`
	MaxObjects     int    `mapstructure:"max_objects"`
	MaxQuota       string `mapstructure:"max_quota"`
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

func (endpointConf *endpointConfig) createClient(ctx context.Context) (*s3.Client, error) {
	s3cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(endpointConf.AccessKey, endpointConf.SecretKey, "")),
		config.WithHTTPClient(&http.Client{Transport: endpointConf.transportConfigS3()}),
	)
	if err != nil {
		return nil, err
	}
	endpoint := endpointConf.Endpoint
	switch {
	case !strings.HasPrefix(endpoint, "http") && endpointConf.DisableHTTPS:
		endpoint = "http://" + endpoint
	case !strings.HasPrefix(endpoint, "https") && !endpointConf.DisableHTTPS:
		endpoint = "https://" + endpoint
	default:
	}

	return s3.NewFromConfig(
		s3cfg,
		func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.EndpointOptions.DisableHTTPS = endpointConf.DisableHTTPS
			o.Region = endpointConf.Region
		},
	), nil
}

// transportConfigS3 is a helper method to setup TLS for the S3 client.
func (endpointConf *endpointConfig) transportConfigS3() http.RoundTripper {
	cfg := new(tls.Config)

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v, using an empty pool as base", err)
		systemCAs = x509.NewCertPool()
	}
	cfg.RootCAs = systemCAs

	if endpointConf.CACert != "" {
		cacert, e := os.ReadFile(endpointConf.CACert) // #nosec this file comes from our config
		if e != nil {
			log.Fatalf("failed to append %q to RootCAs: %v", cacert, e) // nolint # FIXME Fatal should only be called from main
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	var trConfig http.RoundTripper = &http.Transport{
		TLSClientConfig:   cfg,
		ForceAttemptHTTP2: true}

	return trConfig
}

func (endpointConf *endpointConfig) findActiveBucket(ctx context.Context) (string, error) {

	client, err := endpointConf.createClient(ctx)
	if err != nil {
		log.Errorf("failed to create S3 client: %v to endpoint: %s", err, endpointConf.Endpoint)
		return "", err
	}

	bucketsRsp, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		log.Errorf("failed to call S3 client: %v to endpoint: %s", err, endpointConf.Endpoint)
		return "", err
	}

	var relevantBuckets []string
	for _, bucket := range bucketsRsp.Buckets {
		if strings.HasPrefix(aws.ToString(bucket.Name), endpointConf.BucketPrefix) {
			relevantBuckets = append(relevantBuckets, aws.ToString(bucket.Name))
		}
	}
	if len(relevantBuckets) > endpointConf.MaxBuckets {
		return "", storageerrors.ErrorMaxBucketReached
	}

	if len(relevantBuckets) == 0 {
		activeBucket := endpointConf.BucketPrefix + "1"
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &activeBucket})
		if err != nil {
			return "", err
		}
		return activeBucket, nil
	}

	slices.SortFunc(relevantBuckets, func(a, b string) int {
		return strings.Compare(a, b)
	})

	activeBucket := relevantBuckets[len(relevantBuckets)-1]

	// TODO check object count, etc

	return activeBucket, nil
}
