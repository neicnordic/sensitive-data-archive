package writer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/c2h5oh/datasize"
	"github.com/go-viper/mapstructure/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type endpointConfig struct {
	AccessKey      string `mapstructure:"access_key"`
	BucketPrefix   string `mapstructure:"bucket_prefix"`
	CACert         string `mapstructure:"ca_cert"`
	ChunkSize      string `mapstructure:"chunk_size"`
	chunkSizeBytes uint64
	MaxBuckets     uint64 `mapstructure:"max_buckets"`
	MaxObjects     uint64 `mapstructure:"max_objects"`
	MaxSize        string `mapstructure:"max_size"`
	maxSizeBytes   uint64
	Region         string `mapstructure:"region"`
	SecretKey      string `mapstructure:"secret_key"`
	Endpoint       string `mapstructure:"endpoint"`
	DisableHTTPS   bool   `mapstructure:"disable_https"`
	WriterDisabled bool   `mapstructure:"writer_disabled"`

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
		if e.WriterDisabled {
			continue
		}
		switch {
		case e.Endpoint == "":
			return nil, errors.New("missing required parameter: endpoint")
		case e.AccessKey == "":
			return nil, errors.New("missing required parameter: access_key")
		case e.SecretKey == "":
			return nil, errors.New("missing required parameter: secret_key")
		case e.BucketPrefix == "":
			return nil, errors.New("missing required parameter: bucket_prefix")
		default:
			switch {
			case strings.HasPrefix(e.Endpoint, "http") && !e.DisableHTTPS:
				return nil, errors.New("http scheme in endpoint when using HTTPS")
			case strings.HasPrefix(e.Endpoint, "https") && e.DisableHTTPS:
				return nil, errors.New("https scheme in endpoint when HTTPS is disabled")
			default:
			}
			if e.ChunkSize != "" {
				byteSize, err := datasize.ParseString(e.ChunkSize)
				if err != nil {
					return nil, errors.New("could not parse chunk_size as a valid data size")
				}
				if byteSize < 5*datasize.MB {
					return nil, errors.New("chunk_size can not be smaller than 5mb")
				}
				if byteSize > 1*datasize.GB {
					return nil, errors.New("chunk_size can not be bigger than 1gb")
				}
				e.chunkSizeBytes = byteSize.Bytes()
			}
			if e.MaxSize != "" {
				byteSize, err := datasize.ParseString(e.MaxSize)
				if err != nil {
					return nil, errors.New("could not parse max_size as a valid data size")
				}
				e.maxSizeBytes = byteSize.Bytes()
			}
			if e.MaxBuckets == 0 {
				e.MaxBuckets = 1
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
		log.Errorf("failed to read system CAs: %v, using an empty pool as base", err)
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

func (endpointConf *endpointConfig) findActiveBucket(ctx context.Context, locationBroker locationbroker.LocationBroker) (string, error) {
	client, err := endpointConf.getS3Client(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create S3 client to endpoint: %s, due to %v", endpointConf.Endpoint, err)
	}

	bucketsRsp, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return "", fmt.Errorf("failed to call S3 client at endpoint: %s, due to %v", endpointConf.Endpoint, err)
	}

	var bucketsWithPrefix []string
	for _, bucket := range bucketsRsp.Buckets {
		if strings.HasPrefix(aws.ToString(bucket.Name), endpointConf.BucketPrefix) {
			bucketsWithPrefix = append(bucketsWithPrefix, aws.ToString(bucket.Name))
		}
	}

	if len(bucketsWithPrefix) == 0 {
		activeBucket := endpointConf.BucketPrefix + "1"
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &activeBucket})
		if err != nil {
			return "", fmt.Errorf("failed to create S3 bucket: %s at endpoint: %s, due to %v", activeBucket, endpointConf.Endpoint, err)
		}

		return activeBucket, nil
	}

	slices.SortFunc(bucketsWithPrefix, strings.Compare)

	// find first bucket with available object count and size
	for _, bucket := range bucketsWithPrefix {
		loc := endpointConf.Endpoint + "/" + bucket
		count, err := locationBroker.GetObjectCount(ctx, loc)
		if err != nil {
			return "", fmt.Errorf("failed to get object count of location %s, due to %v", loc, err)
		}
		if count >= endpointConf.MaxObjects && endpointConf.MaxObjects > 0 {
			continue
		}

		size, err := locationBroker.GetSize(ctx, loc)
		if err != nil {
			return "", fmt.Errorf("failed to get size of location %s, due to %v", loc, err)
		}
		if size >= endpointConf.maxSizeBytes && endpointConf.maxSizeBytes > 0 {
			continue
		}

		return bucket, nil
	}

	// All created buckets are full, check if we should create new one after latest increment
	if uint64(len(bucketsWithPrefix)) >= endpointConf.MaxBuckets && endpointConf.MaxBuckets > 0 {
		return "", storageerrors.ErrorNoFreeBucket
	}

	currentInc, err := strconv.Atoi(strings.TrimPrefix(bucketsWithPrefix[len(bucketsWithPrefix)-1], endpointConf.BucketPrefix))
	if err != nil {
		return "", fmt.Errorf("failed to generate next bucket increment after bucket %s, due to %v", bucketsWithPrefix[len(bucketsWithPrefix)-1], err)
	}
	activeBucket := fmt.Sprintf("%s%d", endpointConf.BucketPrefix, currentInc+1)
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &activeBucket})
	if err != nil {
		return "", fmt.Errorf("failed to create S3 bucket: %s at endpoint: %s, due to %v", activeBucket, endpointConf.Endpoint, err)
	}

	return activeBucket, nil
}
