package locationbroker

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
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
	config           *config
	db               database.Database
}

type locationEntry struct {
	lastChecked time.Time
	objectCount uint64
	size        uint64
}

func NewLocationBroker(db database.Database, options ...func(*config)) (LocationBroker, error) {
	if db == nil {
		return nil, errors.New("database option required")
	}

	conf := loadConfig()

	for _, option := range options {
		option(conf)
	}

	return &locationBroker{
		checkedLocations: make(map[string]*locationEntry),
		config:           conf,
		db:               db,
	}, nil
}

func (l *locationBroker) GetObjectCount(ctx context.Context, location string) (uint64, error) {
	loc, ok := l.checkedLocations[location]
	if ok && loc.lastChecked.Add(l.config.cacheTTL).After(time.Now()) {
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
	size := uint64(0)

	if err := filepath.Walk(path, func(_ string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		fileSize := info.Size()
		if fileSize < 0 {
			return fmt.Errorf("file: %s has negative size", info.Name())
		}
		//nolint:gosec // disable G115
		size += uint64(fileSize)
		count++

		return nil
	}); err != nil {
		return 0, 0, err
	}

	return size, count, nil
}

func (l *locationBroker) GetSize(ctx context.Context, location string) (uint64, error) {
	loc, ok := l.checkedLocations[location]
	if ok && loc.lastChecked.Add(l.config.cacheTTL).After(time.Now()) {
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

	l.checkedLocations[location] = loc

	return loc.size, nil
}
