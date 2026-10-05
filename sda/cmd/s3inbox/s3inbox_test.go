package main

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/awsdocs/aws-doc-sdk-examples/gov2/s3/stubs"
	"github.com/awsdocs/aws-doc-sdk-examples/gov2/testtools"
	"github.com/stretchr/testify/assert"
)

type BucketBasics struct {
	S3Client *s3.Client
}

func TestCheckS3Bucket_existingBucket(t *testing.T) {
	stubber := testtools.NewStubber()
	defer testtools.ExitTest(stubber, t)

	testClient := s3.NewFromConfig(*stubber.SdkConfig)

	stubber.Add(stubs.StubHeadBucket(
		"bucket",
		&testtools.StubError{
			Err:           &types.BucketAlreadyExists{Message: aws.String("bucket")},
			ContinueAfter: false,
		},
	))
	err := checkS3Bucket(context.Background(), testClient, "bucket", "location")
	assert.NoError(t, err)
}

func TestCheckS3Bucket_noBucket(t *testing.T) {
	stubber := testtools.NewStubber()
	defer testtools.ExitTest(stubber, t)

	testClient := s3.NewFromConfig(*stubber.SdkConfig)

	stubber.Add(stubs.StubHeadBucket(
		"bucket",
		&testtools.StubError{
			Err:           &types.NotFound{Message: aws.String("bucket")},
			ContinueAfter: true,
		},
	))
	stubber.Add(stubs.StubCreateBucket(
		"bucket",
		"location",
		&testtools.StubError{Err: nil, ContinueAfter: false},
	))
	err := checkS3Bucket(context.Background(), testClient, "bucket", "location")
	assert.NoError(t, err)
}

func TestCheckS3Bucket_accessDenied(t *testing.T) {
	stubber := testtools.NewStubber()
	defer testtools.ExitTest(stubber, t)

	testClient := s3.NewFromConfig(*stubber.SdkConfig)

	stubber.Add(stubs.StubHeadBucket(
		"bucket",
		&testtools.StubError{
			Err:           &types.AccessDenied{Message: aws.String("test")},
			ContinueAfter: true,
		},
	))
	err := checkS3Bucket(context.Background(), testClient, "bucket", "location")
	assert.Error(t, err)
}
