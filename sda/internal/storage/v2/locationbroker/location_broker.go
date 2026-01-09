package locationbroker

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

// LocationBroker is responsible for being able to serve the count of objects and the current accumulated size of all objects in a location
type LocationBroker interface {
	// GetObjectCount returns the current amount of objects in a location
	GetObjectCount(ctx context.Context, location string) (uint64, error)
	// GetSize returns the accumulated size(in bytes) of all objects in a location
	GetSize(ctx context.Context, location string) (uint64, error)
}

type locationBroker struct {
	checkedLocations map[string]*locationEntry
	cacheTTL         time.Duration
	db               database.Database
}

type locationEntry struct {
	lastChecked time.Time
	objectCount uint64
	size        uint64
}

func NewLocationBroker(db database.Database, cacheTTL time.Duration) LocationBroker {
	return &locationBroker{
		checkedLocations: make(map[string]*locationEntry),
		cacheTTL:         cacheTTL,
		db:               db,
	}
}

func (l *locationBroker) GetObjectCount(ctx context.Context, location string) (uint64, error) {
	loc, ok := l.checkedLocations[location]
	if ok && loc.lastChecked.Add(l.cacheTTL).After(time.Now()) {
		return loc.objectCount, nil
	}

	loc = &locationEntry{
		lastChecked: time.Now(),
	}

	var err error
	switch {
	case strings.HasPrefix(location, "/"):
		loc.size, loc.objectCount, err = getSizeAndCountInDir(location)
		if err != nil {
			return 0, err
		}
	default:
		loc.size, loc.objectCount, err = l.db.GetSizeAndObjectCountOfLocation(ctx, location)
		if err != nil {
			return 0, err
		}
	}

	l.checkedLocations[location] = loc

	return loc.objectCount, nil
}

// TODO is it more performant to just use the DB for posix as well?
func getSizeAndCountInDir(path string) (uint64, uint64, error) {
	count := uint64(0)
	size := int64(0)
	dir, err := os.ReadDir(path)
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range dir {
		if entry.IsDir() {
			subDirSize, subDirCount, err := getSizeAndCountInDir(entry.Name())
			if err != nil {
				return 0, 0, err
			}
			count += subDirCount
			subDirSize += subDirSize
			continue
		}
		count++
		fileInfo, err := entry.Info()
		if err != nil {
			return 0, 0, err
		}
		size += fileInfo.Size()
	}
	return uint64(size), count, nil
}

func (l *locationBroker) GetSize(ctx context.Context, location string) (uint64, error) {
	loc, ok := l.checkedLocations[location]
	if ok && loc.lastChecked.After(time.Now()) {
		return loc.size, nil
	}

	loc = &locationEntry{
		lastChecked: time.Now(),
	}
	var err error
	switch {
	case strings.HasPrefix(location, "/"):
		loc.size, loc.objectCount, err = getSizeAndCountInDir(location)
		if err != nil {
			return 0, err
		}
	default:
		loc.size, loc.objectCount, err = l.db.GetSizeAndObjectCountOfLocation(ctx, location)
		if err != nil {
			return 0, err
		}
	}

	return loc.size, nil
}
