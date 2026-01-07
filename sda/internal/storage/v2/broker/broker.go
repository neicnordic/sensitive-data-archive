package broker

import "context"

// LocationBroker is responsible for being able to serve the count of objects and the current accumulated size of all objects in a location
type LocationBroker interface {
	// GetObjectCount returns the current amount of objects in a location
	GetObjectCount(ctx context.Context, location string) (uint64, error)
	// GetSize returns the accumulated size(in bytes) of all objects in a location
	GetSize(ctx context.Context, location string) (uint64, error)
}
