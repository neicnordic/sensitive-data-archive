package database

import "context"

type Database interface {
	GetSizeAndObjectCountOfLocation(ctx context.Context, location string) (uint64, uint64, error)
}
