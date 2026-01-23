package locationbroker

import (
	"context"
	"errors"
	"sync"
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
	sync.Mutex
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
	l.Lock()
	defer l.Unlock()

	loc, ok := l.checkedLocations[location]
	if ok && loc.lastChecked.Add(l.config.cacheTTL).After(time.Now()) {
		return loc.objectCount, nil
	}

	size, objectCount, err := l.db.GetSizeAndObjectCountOfLocation(ctx, location)
	if err != nil {
		return 0, err
	}

	l.checkedLocations[location] = &locationEntry{
		lastChecked: time.Now(),
		size:        size,
		objectCount: objectCount,
	}

	return objectCount, nil
}

func (l *locationBroker) GetSize(ctx context.Context, location string) (uint64, error) {
	l.Lock()
	defer l.Unlock()

	loc, ok := l.checkedLocations[location]
	if ok && loc.lastChecked.Add(l.config.cacheTTL).After(time.Now()) {
		return loc.size, nil
	}

	size, objectCount, err := l.db.GetSizeAndObjectCountOfLocation(ctx, location)
	if err != nil {
		return 0, err
	}

	l.checkedLocations[location] = &locationEntry{
		lastChecked: time.Now(),
		size:        size,
		objectCount: objectCount,
	}

	return size, nil
}
