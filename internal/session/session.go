package session

import (
	"github.com/dgraph-io/ristretto"
	"github.com/google/uuid"
	"github.com/neicnordic/sda-download/internal/config"
	log "github.com/sirupsen/logrus"
)

// SessionCache stores dataset permission lists
var SessionCache *ristretto.Cache

// DatasetCache stores the dataset permissions
// and information whether this information has
// already been checked or not. This information
// can then be used to skip the time-costly
// authentication middleware
// DatasetCache==nil, session doesn't exist
// DatasetCache.Datasets==nil, session exists, user has no permissions (this case is not used in middleware.go)
// DatasetCache.Datasets==[]string{}, session exists, user has permissions
type DatasetCache struct {
	Datasets []string
}

// InitialiseSessionCache creates a cache manager that stores keys and values in memory
func InitialiseSessionCache() (*ristretto.Cache, error) {
	log.Debug("creating session cache")
	sessionCache, err := ristretto.NewCache(
		&ristretto.Config{
			// Maximum number of items in cache
			// A recommended number is expected maximum times 10
			// so 100,000 * 10 = 1,000,000
			NumCounters: 1e6,
			// Maximum size of cache
			// 100,000 items at most, items have varying sizes, but are generally
			// very small, in the range of less than 1kB each.
			// Max memory usage with expected payloads are ~100MB
			MaxCost:     100000,
			BufferItems: 64,
		},
	)
	if err != nil {
		log.Errorf("failed to create session cache, reason=%v", err)

		return nil, err
	}
	log.Debug("session cache created")

	return sessionCache, nil
}

// Get returns a value from cache at key
var Get = func(key string) ([]string, bool) {
	log.Debug("get value from cache")
	header, exists := SessionCache.Get(key)
	var cachedDatasets []string
	if header != nil {
		cachedDatasets = header.(DatasetCache).Datasets
	} else {
		cachedDatasets = nil
	}
	log.Debugf("cache response, exists=%t, datasets=%s", exists, cachedDatasets)

	return cachedDatasets, exists
}

func Set(key string, datasets []string) {
	log.Debug("store to cache")
	datasetCache := DatasetCache{
		Datasets: datasets,
	}
	// Each item has a cost of 1, with max size of cache being 100,000 items
	SessionCache.SetWithTTL(key, datasetCache, 1, config.Config.Session.Expiration)
	log.Debug("stored to cache")
}

// NewSessionKey generates a session key used for storing
// dataset permissions, and checks that it doesn't already exist
var NewSessionKey = func() string {
	log.Debug("generating new session key")

	// Generate a new key until one is generated, which doesn't already exist
	var sessionKey string
	exists := true
	for exists {

		// Generate the key
		key := uuid.New().String()
		sessionKey = key

		// Check if the generated key already exists in the cache
		_, exists = Get(key)
	}

	log.Debug("new session key generated")

	return sessionKey
}
