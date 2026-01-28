package database

import "context"

type Database interface {
	GetSizeAndObjectCountOfLocation(ctx context.Context, location string) (uint64, uint64, error)

	GetSubmissionLocation(ctx context.Context, fileID string) (string, error)
	CancelFile(ctx context.Context, fileID string) error
	IsFileInDataset(ctx context.Context, fileID string) (bool, error)
}
