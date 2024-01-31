package session

import (
	"github.com/dgraph-io/ristretto"
	"github.com/google/uuid"
	"github.com/neicnordic/sda-download/internal/config"
	log "github.com/sirupsen/logrus"
)

// SessionCache is the in-memory storage holding session keys and interfaces containing cached data
var SessionCache *ristretto.Cache

// Cache stores the dataset permissions
// and information whether this information has
// already been checked or not. This information
// can then be used to skip the time-costly
// authentication middleware
// Cache==nil, session doesn't exist
// Cache.Datasets==nil, session exists, user has no permissions (this case is not used in middleware.go)
// Cache.Datasets==[]string{...}, session exists, user has permissions
type Cache struct {
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

// Get returns a cache item from the session storage at key
var Get = func(key string) (Cache, bool) {
	log.Debug("get value from cache")
	cachedItem, exists := SessionCache.Get(key)
	var cached Cache
	if exists {
		// the storage is unaware of cached types, so if an item is found
		// we must assert it is the expected interface type (Cache)
		cached = cachedItem.(Cache)
	}
	log.Debugf("cache response, exists=%t, cached=%v", exists, cached)

	return cached, exists
}

func Set(key string, toCache Cache) {
	log.Debugf("store %v to cache", toCache)
	// Each item has a cost of 1, with max size of cache being 100,000 items
	SessionCache.SetWithTTL(key, toCache, 1, config.Config.Session.Expiration)
	log.Debugf("stored %v to cache", toCache)
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
