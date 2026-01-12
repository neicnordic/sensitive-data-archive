package reader

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
)

func (reader *Reader) FindFile(ctx context.Context, filePath string) (string, error) {
	for _, endpointConf := range reader.endpoints {
		client, err := reader.createClient(ctx, endpointConf.Endpoint)
		if err != nil {
			return "", err
		}

		bucketsRsp, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			log.Errorf("failed to call S3 client: %v to endpoint: %s", err, endpointConf.Endpoint)

			return "", err
		}

		var bucketsWithPrefix []string
		for _, bucket := range bucketsRsp.Buckets {
			if strings.HasPrefix(aws.ToString(bucket.Name), endpointConf.BucketPrefix) {
				bucketsWithPrefix = append(bucketsWithPrefix, aws.ToString(bucket.Name))
			}
		}
		slices.SortFunc(bucketsWithPrefix, strings.Compare)

		for _, bucket := range bucketsWithPrefix {
			_, err := reader.getFileSize(ctx, client, bucket, filePath)

			if err != nil {
				if errors.Is(err, storageerrors.ErrorFileNotFoundInLocation) {
					continue
				}

				return "", err
			}

			return endpointConf.Endpoint + "/" + bucket, nil
		}
	}

	return "", storageerrors.ErrorFileNotFoundInLocation
}
