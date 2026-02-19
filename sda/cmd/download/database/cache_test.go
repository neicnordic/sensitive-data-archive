package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockDatabase is a mock implementation of the Database interface
type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) Ping(ctx context.Context) error {
	args := m.Called(ctx)

	return args.Error(0)
}

func (m *MockDatabase) Close() error {
	args := m.Called()

	return args.Error(0)
}

func (m *MockDatabase) GetAllDatasets(ctx context.Context) ([]Dataset, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]Dataset), args.Error(1)
}

func (m *MockDatabase) GetDatasetIDsByUser(ctx context.Context, user string) ([]string, error) {
	args := m.Called(ctx, user)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDatabase) GetUserDatasets(ctx context.Context, datasetIDs []string) ([]Dataset, error) {
	args := m.Called(ctx, datasetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]Dataset), args.Error(1)
}

func (m *MockDatabase) GetDatasetInfo(ctx context.Context, datasetID string) (*DatasetInfo, error) {
	args := m.Called(ctx, datasetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*DatasetInfo), args.Error(1)
}

func (m *MockDatabase) GetDatasetFiles(ctx context.Context, datasetID string) ([]File, error) {
	args := m.Called(ctx, datasetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]File), args.Error(1)
}

func (m *MockDatabase) GetFileByID(ctx context.Context, fileID string) (*File, error) {
	args := m.Called(ctx, fileID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*File), args.Error(1)
}

func (m *MockDatabase) GetFileByPath(ctx context.Context, datasetID, filePath string) (*File, error) {
	args := m.Called(ctx, datasetID, filePath)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*File), args.Error(1)
}

func (m *MockDatabase) CheckFilePermission(ctx context.Context, fileID string, datasetIDs []string) (bool, error) {
	args := m.Called(ctx, fileID, datasetIDs)

	return args.Bool(0), args.Error(1)
}

func TestNewCachedDB(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := DefaultCacheConfig()

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)
	require.NotNil(t, cachedDB)

	mockDB.On("Close").Return(nil)
	err = cachedDB.Close()
	assert.NoError(t, err)
	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetFileByID_CacheHit(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	expectedFile := &File{
		ID:            "file123",
		DatasetID:     "dataset1",
		SubmittedPath: "/path/to/file.txt",
		ArchiveSize:   1024,
		DecryptedSize: 1000,
	}

	// First call should hit the database
	mockDB.On("GetFileByID", ctx, "file123").Return(expectedFile, nil).Once()

	file1, err := cachedDB.GetFileByID(ctx, "file123")
	require.NoError(t, err)
	assert.Equal(t, expectedFile, file1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call should hit the cache (no additional DB call)
	file2, err := cachedDB.GetFileByID(ctx, "file123")
	require.NoError(t, err)
	assert.Equal(t, expectedFile, file2)

	// Verify the database was only called once
	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetFileByID_NotFound(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := DefaultCacheConfig()

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()

	// Database returns nil for not found
	mockDB.On("GetFileByID", ctx, "nonexistent").Return(nil, nil).Once()

	file, err := cachedDB.GetFileByID(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, file)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetFileByID_Error(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := DefaultCacheConfig()

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	expectedErr := errors.New("database error")

	mockDB.On("GetFileByID", ctx, "file123").Return(nil, expectedErr).Once()

	file, err := cachedDB.GetFileByID(ctx, "file123")
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, file)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetFileByPath_CacheHit(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	expectedFile := &File{
		ID:            "file456",
		DatasetID:     "dataset1",
		SubmittedPath: "/path/to/file.txt",
	}

	// First call should hit the database
	mockDB.On("GetFileByPath", ctx, "dataset1", "/path/to/file.txt").Return(expectedFile, nil).Once()

	file1, err := cachedDB.GetFileByPath(ctx, "dataset1", "/path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, expectedFile, file1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call should hit the cache
	file2, err := cachedDB.GetFileByPath(ctx, "dataset1", "/path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, expectedFile, file2)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_CheckFilePermission_CacheHit(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	datasetIDs := []string{"dataset1", "dataset2"}

	// First call should hit the database
	mockDB.On("CheckFilePermission", ctx, "file123", datasetIDs).Return(true, nil).Once()

	hasPermission1, err := cachedDB.CheckFilePermission(ctx, "file123", datasetIDs)
	require.NoError(t, err)
	assert.True(t, hasPermission1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call should hit the cache
	hasPermission2, err := cachedDB.CheckFilePermission(ctx, "file123", datasetIDs)
	require.NoError(t, err)
	assert.True(t, hasPermission2)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_CheckFilePermission_DifferentDatasets(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	datasetIDs1 := []string{"dataset1", "dataset2"}
	datasetIDs2 := []string{"dataset3"}

	// Each unique set of datasets should result in a separate cache entry
	mockDB.On("CheckFilePermission", ctx, "file123", datasetIDs1).Return(true, nil).Once()
	mockDB.On("CheckFilePermission", ctx, "file123", datasetIDs2).Return(false, nil).Once()

	hasPermission1, err := cachedDB.CheckFilePermission(ctx, "file123", datasetIDs1)
	require.NoError(t, err)
	assert.True(t, hasPermission1)

	hasPermission2, err := cachedDB.CheckFilePermission(ctx, "file123", datasetIDs2)
	require.NoError(t, err)
	assert.False(t, hasPermission2)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_CheckFilePermission_OrderIndependent(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	// Different order, same datasets - should produce same cache key
	datasetIDs1 := []string{"dataset2", "dataset1"}
	datasetIDs2 := []string{"dataset1", "dataset2"}

	// Only one DB call expected because cache key should be the same
	mockDB.On("CheckFilePermission", ctx, "file123", datasetIDs1).Return(true, nil).Once()

	hasPermission1, err := cachedDB.CheckFilePermission(ctx, "file123", datasetIDs1)
	require.NoError(t, err)
	assert.True(t, hasPermission1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call with different order should hit cache
	hasPermission2, err := cachedDB.CheckFilePermission(ctx, "file123", datasetIDs2)
	require.NoError(t, err)
	assert.True(t, hasPermission2)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetDatasetInfo_CacheHit(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	expectedInfo := &DatasetInfo{
		ID:        "dataset1",
		Title:     "Test Dataset",
		FileCount: 10,
		TotalSize: 1024000,
	}

	mockDB.On("GetDatasetInfo", ctx, "dataset1").Return(expectedInfo, nil).Once()

	info1, err := cachedDB.GetDatasetInfo(ctx, "dataset1")
	require.NoError(t, err)
	assert.Equal(t, expectedInfo, info1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call should hit cache
	info2, err := cachedDB.GetDatasetInfo(ctx, "dataset1")
	require.NoError(t, err)
	assert.Equal(t, expectedInfo, info2)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetDatasetFiles_CacheHit(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	expectedFiles := []File{
		{ID: "file1", DatasetID: "dataset1", SubmittedPath: "/file1.txt"},
		{ID: "file2", DatasetID: "dataset1", SubmittedPath: "/file2.txt"},
	}

	mockDB.On("GetDatasetFiles", ctx, "dataset1").Return(expectedFiles, nil).Once()

	files1, err := cachedDB.GetDatasetFiles(ctx, "dataset1")
	require.NoError(t, err)
	assert.Equal(t, expectedFiles, files1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call should hit cache
	files2, err := cachedDB.GetDatasetFiles(ctx, "dataset1")
	require.NoError(t, err)
	assert.Equal(t, expectedFiles, files2)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetAllDatasets_CacheHit(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	expectedDatasets := []Dataset{
		{ID: "dataset1", Title: "Dataset 1"},
		{ID: "dataset2", Title: "Dataset 2"},
	}

	mockDB.On("GetAllDatasets", ctx).Return(expectedDatasets, nil).Once()

	datasets1, err := cachedDB.GetAllDatasets(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedDatasets, datasets1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call should hit cache
	datasets2, err := cachedDB.GetAllDatasets(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedDatasets, datasets2)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetDatasetIDsByUser_CacheHit(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	expectedIDs := []string{"dataset1", "dataset2"}

	mockDB.On("GetDatasetIDsByUser", ctx, "user@example.com").Return(expectedIDs, nil).Once()

	ids1, err := cachedDB.GetDatasetIDsByUser(ctx, "user@example.com")
	require.NoError(t, err)
	assert.Equal(t, expectedIDs, ids1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call should hit cache
	ids2, err := cachedDB.GetDatasetIDsByUser(ctx, "user@example.com")
	require.NoError(t, err)
	assert.Equal(t, expectedIDs, ids2)

	mockDB.AssertExpectations(t)
}

func TestCachedDB_GetUserDatasets_CacheHit(t *testing.T) {
	mockDB := new(MockDatabase)
	cfg := CacheConfig{
		FileTTL:       1 * time.Minute,
		PermissionTTL: 1 * time.Minute,
		DatasetTTL:    1 * time.Minute,
	}

	cachedDB, err := NewCachedDB(mockDB, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	datasetIDs := []string{"dataset1", "dataset2"}
	expectedDatasets := []Dataset{
		{ID: "dataset1", Title: "Dataset 1"},
		{ID: "dataset2", Title: "Dataset 2"},
	}

	mockDB.On("GetUserDatasets", ctx, datasetIDs).Return(expectedDatasets, nil).Once()

	datasets1, err := cachedDB.GetUserDatasets(ctx, datasetIDs)
	require.NoError(t, err)
	assert.Equal(t, expectedDatasets, datasets1)

	// Wait for ristretto to process the set
	time.Sleep(10 * time.Millisecond)

	// Second call should hit cache
	datasets2, err := cachedDB.GetUserDatasets(ctx, datasetIDs)
	require.NoError(t, err)
	assert.Equal(t, expectedDatasets, datasets2)

	mockDB.AssertExpectations(t)
}

func TestHashStrings(t *testing.T) {
	tests := []struct {
		name   string
		input1 []string
		input2 []string
		equal  bool
	}{
		{
			name:   "same order",
			input1: []string{"a", "b", "c"},
			input2: []string{"a", "b", "c"},
			equal:  true,
		},
		{
			name:   "different order same elements",
			input1: []string{"c", "a", "b"},
			input2: []string{"a", "b", "c"},
			equal:  true,
		},
		{
			name:   "different elements",
			input1: []string{"a", "b", "c"},
			input2: []string{"a", "b", "d"},
			equal:  false,
		},
		{
			name:   "empty slices",
			input1: []string{},
			input2: []string{},
			equal:  true,
		},
		{
			name:   "single element",
			input1: []string{"single"},
			input2: []string{"single"},
			equal:  true,
		},
		{
			name:   "collision prevention - different concatenations",
			input1: []string{"ab", "c"},
			input2: []string{"a", "bc"},
			equal:  false, // Should not collide due to separator
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := hashStrings(tt.input1)
			hash2 := hashStrings(tt.input2)

			if tt.equal {
				assert.Equal(t, hash1, hash2, "hashes should be equal for %v and %v", tt.input1, tt.input2)
			} else {
				assert.NotEqual(t, hash1, hash2, "hashes should be different for %v and %v", tt.input1, tt.input2)
			}
		})
	}
}

func TestDefaultCacheConfig(t *testing.T) {
	cfg := DefaultCacheConfig()
	assert.Equal(t, 5*time.Minute, cfg.FileTTL)
	assert.Equal(t, 2*time.Minute, cfg.PermissionTTL)
	assert.Equal(t, 5*time.Minute, cfg.DatasetTTL)
}
