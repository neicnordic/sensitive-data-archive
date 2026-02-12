package locationbroker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

// LocationBroker is responsible for being able to serve the count of objects and the current accumulated size of all objects in a location
type LocationBroker interface {
	// GetObjectCount returns the current amount of objects in a location
	GetObjectCount(ctx context.Context, backendName, location string) (uint64, error)
	// GetSize returns the accumulated size(in bytes) of all objects in a location
	GetSize(ctx context.Context, backendName, location string) (uint64, error)
	// RegisterSizeAndCountFinderFunc registers a function that returns the size and object count for a backend and location which matches
	// the locationMatcher,
	// for which we can not be supported by the database, i.e any other backends than "inbox", "archive", and "backup"
	RegisterSizeAndCountFinderFunc(backend string, locationMatcher func(location string) bool, sizeAndCountFunc func(ctx context.Context, location string) (uint64, uint64, error))
}

type locationBroker struct {
	checkedLocations    map[string]*locationEntry
	config              *config
	db                  database.Database
	sizeAndCountFinders []*sizeAndCountFinder
	sync.Mutex
}

type sizeAndCountFinder struct {
	backend                string
	locationMatcher        func(location string) bool
	sizeAndCountFinderFunc func(ctx context.Context, location string) (uint64, uint64, error)
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

func (l *locationBroker) RegisterSizeAndCountFinderFunc(backend string, locationMatcher func(location string) bool, sizeAndCountFunc func(ctx context.Context, location string) (uint64, uint64, error)) {
	l.sizeAndCountFinders = append(l.sizeAndCountFinders, &sizeAndCountFinder{
		backend:                backend,
		locationMatcher:        locationMatcher,
		sizeAndCountFinderFunc: sizeAndCountFunc,
	})
}
func (l *locationBroker) GetObjectCount(ctx context.Context, backendName, location string) (uint64, error) {
	l.Lock()
	defer l.Unlock()

	loc, ok := l.checkedLocations[location]
	if ok && loc.lastChecked.Add(l.config.cacheTTL).After(time.Now()) {
		return loc.objectCount, nil
	}

	var size, objectCount uint64
	var err error
	switch backendName {
	case "inbox", "archive", "backup":
		size, objectCount, err = l.db.GetSizeAndObjectCountOfLocation(ctx, location)
	default:
		var sizeAndCountFinderFunc func(ctx context.Context, location string) (uint64, uint64, error)
		for _, scf := range l.sizeAndCountFinders {
			if scf.backend != backendName || !scf.locationMatcher(location) {
				continue
			}
			if sizeAndCountFinderFunc != nil {
				return 0, fmt.Errorf("multiple size and count finder func matching location: %s, backend: %s", location, backendName)
			}
			sizeAndCountFinderFunc = scf.sizeAndCountFinderFunc
		}
		if sizeAndCountFinderFunc == nil {
			return 0, fmt.Errorf("no size and count finder function defined for location: %s, backed %s", location, backendName)
		}
		size, objectCount, err = sizeAndCountFinderFunc(ctx, location)
	}
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

func (l *locationBroker) GetSize(ctx context.Context, backendName, location string) (uint64, error) {
	l.Lock()
	defer l.Unlock()

	loc, ok := l.checkedLocations[location]
	if ok && loc.lastChecked.Add(l.config.cacheTTL).After(time.Now()) {
		return loc.size, nil
	}

	var size, objectCount uint64
	var err error
	switch backendName {
	case "inbox", "archive", "backup":
		size, objectCount, err = l.db.GetSizeAndObjectCountOfLocation(ctx, location)
	default:
		var sizeAndCountFinderFunc func(ctx context.Context, location string) (uint64, uint64, error)
		for _, scf := range l.sizeAndCountFinders {
			if scf.backend != backendName || !scf.locationMatcher(location) {
				continue
			}
			if sizeAndCountFinderFunc != nil {
				return 0, fmt.Errorf("multiple size and count finder func matching location: %s, backend: %s", location, backendName)
			}
			sizeAndCountFinderFunc = scf.sizeAndCountFinderFunc
		}
		if sizeAndCountFinderFunc == nil {
			return 0, fmt.Errorf("no size and count finder function defined for location: %s, backed %s", location, backendName)
		}
		size, objectCount, err = sizeAndCountFinderFunc(ctx, location)
	}
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
