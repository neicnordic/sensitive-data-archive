// Package database provides database operations for the download service.
package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

// CacheConfig holds configuration for the database cache.
type CacheConfig struct {
	// FileTTL is the time-to-live for file query results.
	FileTTL time.Duration
	// PermissionTTL is the time-to-live for permission check results.
	PermissionTTL time.Duration
	// DatasetTTL is the time-to-live for dataset query results.
	DatasetTTL time.Duration
}

// DefaultCacheConfig returns sensible defaults for cache configuration.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		FileTTL:       5 * time.Minute,
		PermissionTTL: 2 * time.Minute,
		DatasetTTL:    5 * time.Minute,
	}
}

// CachedDB wraps a Database implementation with ristretto caching.
// It implements the Database interface and caches query results
// to reduce database roundtrips, particularly beneficial for streaming
// use cases where the same file may be requested multiple times.
type CachedDB struct {
	db     Database
	cache  *ristretto.Cache
	config CacheConfig
}

// NewCachedDB creates a new CachedDB wrapping the provided database.
// The cache is configured with sensible defaults for a download service:
// - NumCounters: 1e6 (recommended: expected max items * 10)
// - MaxCost: 100000 (max items, each with cost 1)
// - BufferItems: 64
func NewCachedDB(db Database, cfg CacheConfig) (*CachedDB, error) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	slog.Info("Database cache initialized",
		slog.Duration("FileTTL", cfg.FileTTL),
		slog.Duration("PermissionTTL", cfg.PermissionTTL),
		slog.Duration("DatasetTTL", cfg.DatasetTTL),
	)

	return &CachedDB{
		db:     db,
		cache:  cache,
		config: cfg,
	}, nil
}

// Ping verifies the database connection is alive.
// This delegates directly to the underlying database without caching.
func (c *CachedDB) Ping(ctx context.Context) error {
	return c.db.Ping(ctx)
}

// Close closes the underlying database connection and the cache.
func (c *CachedDB) Close() error {
	c.cache.Close()

	return c.db.Close()
}

// GetAllDatasets returns all datasets (for allow-all-data mode).
// Results are cached with DatasetTTL.
func (c *CachedDB) GetAllDatasets(ctx context.Context) ([]Dataset, error) {
	ctx, span := observability.StartSpan(ctx, "cache.GetAllDatasets")
	defer span.End()

	const key = "datasets:all"

	if val, found := c.cache.Get(key); found {
		if rval, ok := val.([]Dataset); ok {
			span.Debug("cache hit: GetAllDatasets")

			return rval, nil
		}
	}

	span.Debug("cache miss: GetAllDatasets")
	datasets, err := c.db.GetAllDatasets(ctx)
	if err != nil {
		return nil, err
	}

	c.cache.SetWithTTL(key, datasets, 1, c.config.DatasetTTL)

	return datasets, nil
}

// GetDatasetIDsByUser returns dataset IDs where the user is the submission_user.
// Results are cached per user with DatasetTTL.
func (c *CachedDB) GetDatasetIDsByUser(ctx context.Context, user string) ([]string, error) {
	ctx, span := observability.StartSpan(ctx, "cache.GetDatasetIDsByUser", attribute.String("user", user))
	defer span.End()

	key := "datasets:user:" + user

	if val, found := c.cache.Get(key); found {
		if rval, ok := val.([]string); ok {
			span.Debug("cache hit: GetDatasetIDsByUser(user)")

			return rval, nil
		}
	}

	span.Debug("cache miss: GetDatasetIDsByUser(user)")
	datasetIDs, err := c.db.GetDatasetIDsByUser(ctx, user)
	if err != nil {
		return nil, err
	}

	c.cache.SetWithTTL(key, datasetIDs, 1, c.config.DatasetTTL)

	return datasetIDs, nil
}

// GetUserDatasets returns datasets the user has access to.
// Results are cached based on the hash of dataset IDs with DatasetTTL.
func (c *CachedDB) GetUserDatasets(ctx context.Context, datasetIDs []string) ([]Dataset, error) {
	ctx, span := observability.StartSpan(ctx, "cache.GetUserDatasets")
	defer span.End()

	key := "datasets:ids:" + hashStrings(datasetIDs)

	if val, found := c.cache.Get(key); found {
		if rval, ok := val.([]Dataset); ok {
			span.Debug("cache hit: GetUserDatasets")

			return rval, nil
		}
	}

	span.Debug("cache miss: GetUserDatasets")
	datasets, err := c.db.GetUserDatasets(ctx, datasetIDs)
	if err != nil {
		return nil, err
	}

	c.cache.SetWithTTL(key, datasets, 1, c.config.DatasetTTL)

	return datasets, nil
}

// GetDatasetInfo returns metadata for a specific dataset.
// Results are cached with DatasetTTL.
func (c *CachedDB) GetDatasetInfo(ctx context.Context, datasetID string) (*DatasetInfo, error) {
	ctx, span := observability.StartSpan(ctx, "cache.GetDatasetInfo", attribute.String("dataset-id", datasetID))
	defer span.End()

	key := "dataset:info:" + datasetID

	if val, found := c.cache.Get(key); found {
		if rval, ok := val.(*DatasetInfo); ok {
			span.Debug("cache hit: GetDatasetInfo(datasetID)")

			return rval, nil
		}
	}

	span.Debug("cache miss: GetDatasetInfo(datasetID)")
	info, err := c.db.GetDatasetInfo(ctx, datasetID)
	if err != nil {
		return nil, err
	}

	if info != nil {
		c.cache.SetWithTTL(key, info, 1, c.config.DatasetTTL)
	}

	return info, nil
}

// GetFileByID returns file information by stable ID.
// Results are cached with FileTTL. This is particularly important
// for streaming use cases where the same file may be requested
// multiple times (e.g., range requests).
func (c *CachedDB) GetFileByID(ctx context.Context, fileID string) (*File, error) {
	ctx, span := observability.StartSpan(ctx, "cache.GetFileByID", attribute.String("file-id", fileID))
	defer span.End()

	key := "file:id:" + fileID

	if val, found := c.cache.Get(key); found {
		if rval, ok := val.(*File); ok {
			span.Debug("cache hit: GetFileByID(fileID)")

			return rval, nil
		}
	}

	span.Debug("cache miss: GetFileByID(fileID)")
	file, err := c.db.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, err
	}

	if file != nil {
		c.cache.SetWithTTL(key, file, 1, c.config.FileTTL)
	}

	return file, nil
}

// GetFileByPath returns file information by dataset and submitted path.
// Results are cached with FileTTL.
func (c *CachedDB) GetFileByPath(ctx context.Context, datasetID, filePath string) (*File, error) {
	ctx, span := observability.StartSpan(ctx, "cache.GetFileByPath", attribute.String("dataset-id", datasetID), attribute.String("file-path", filePath))
	defer span.End()

	key := "file:path:" + datasetID + ":" + filePath

	if val, found := c.cache.Get(key); found {
		if rval, ok := val.(*File); ok {
			span.Debug("cache hit: GetFileByPath(datasetID, filePath)")

			return rval, nil
		}
	}

	span.Debug("cache miss: GetFileByPath(datasetID, filePath)")
	file, err := c.db.GetFileByPath(ctx, datasetID, filePath)
	if err != nil {
		return nil, err
	}

	if file != nil {
		c.cache.SetWithTTL(key, file, 1, c.config.FileTTL)
	}

	return file, nil
}

// CheckFilePermission verifies the user has access to download a file.
// Results are cached with PermissionTTL. The cache key includes a hash
// of the sorted datasetIDs to ensure user-scoped caching.
func (c *CachedDB) CheckFilePermission(ctx context.Context, fileID string, datasetIDs []string) (bool, error) {
	ctx, span := observability.StartSpan(ctx, "cache.CheckFilePermission", attribute.String("file-id", fileID))
	defer span.End()

	key := "perm:" + fileID + ":" + hashStrings(datasetIDs)

	if val, found := c.cache.Get(key); found {
		if rval, ok := val.(bool); ok {
			span.Debug("cache hit: CheckFilePermission(fileID)")

			return rval, nil
		}
	}

	span.Debug("cache miss: CheckFilePermission(fileID)")
	hasPermission, err := c.db.CheckFilePermission(ctx, fileID, datasetIDs)
	if err != nil {
		return false, err
	}

	c.cache.SetWithTTL(key, hasPermission, 1, c.config.PermissionTTL)

	return hasPermission, nil
}

// CheckDatasetExists checks if a dataset with the given stable_id exists.
// Results are cached with DatasetTTL.
func (c *CachedDB) CheckDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	ctx, span := observability.StartSpan(ctx, "cache.CheckDatasetExists", attribute.String("dataset-id", datasetID))
	defer span.End()

	key := "dataset:exists:" + datasetID

	if val, found := c.cache.Get(key); found {
		if rval, ok := val.(bool); ok {
			span.Debug("cache hit: CheckDatasetExists(datasetID)")

			return rval, nil
		}
	}

	span.Debug("cache miss: CheckDatasetExists(datasetID)")
	exists, err := c.db.CheckDatasetExists(ctx, datasetID)
	if err != nil {
		return false, err
	}

	c.cache.SetWithTTL(key, exists, 1, c.config.DatasetTTL)

	return exists, nil
}

// GetFileChecksums delegates to the underlying database without caching.
func (c *CachedDB) GetFileChecksums(ctx context.Context, fileID string, source string) ([]Checksum, error) {
	ctx, span := observability.StartSpan(ctx, "cache.GetFileChecksums", attribute.String("file-id", fileID), attribute.String("source", source))
	defer span.End()

	return c.db.GetFileChecksums(ctx, fileID, source)
}

// GetDatasetFilesPaginated delegates to the underlying database without caching.
// Paginated queries use ephemeral cursors, making caching impractical.
func (c *CachedDB) GetDatasetFilesPaginated(ctx context.Context, datasetID string, opts FileListOptions) ([]File, error) {
	ctx, span := observability.StartSpan(ctx, "cache.GetDatasetFilesPaginated", attribute.String("dataset-id", datasetID))
	defer span.End()

	return c.db.GetDatasetFilesPaginated(ctx, datasetID, opts)
}

// hashStrings creates a deterministic hash of a string slice.
// The slice is sorted before hashing to ensure consistent keys
// regardless of input order.
func hashStrings(strs []string) string {
	if len(strs) == 0 {
		return "empty"
	}

	// Sort a copy to avoid modifying the input
	sorted := make([]string, len(strs))
	copy(sorted, strs)
	slices.Sort(sorted)

	h := sha256.New()
	for _, s := range sorted {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0}) // Separator to avoid collision between ["ab", "c"] and ["a", "bc"]
	}

	return hex.EncodeToString(h.Sum(nil))[:24] // Use first 24 chars (96 bits) for low collision probability
}
