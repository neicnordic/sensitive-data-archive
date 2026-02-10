package database

import (
	"context"
)

type Database interface {
	GetSizeAndObjectCountOfLocation(ctx context.Context, location string) (uint64, uint64, error)
	GetUploadedSubmissionFilePathAndLocation(ctx context.Context, submissionUser, fileID string) (string, string, error)
	GetArchiveLocation(fileID string) (string, error)
	GetArchivePathAndLocation(stableID string) (string, string, error)
	GetMappingData(accessionID string) (*MappingData, error)
	GetSubmissionLocation(ctx context.Context, fileID string) (string, error)
	CancelFile(ctx context.Context, fileID, message string) error
	IsFileInDataset(ctx context.Context, fileID string) (bool, error)
}
