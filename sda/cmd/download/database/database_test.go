package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMockDB creates a mock database with prepared statements for testing.
// It prepares all queries defined in the queries map and returns the mock for expectations.
func setupMockDB(t *testing.T) (*PostgresDB, sqlmock.Sqlmock, func()) {
	t.Helper()

	// Use QueryMatcherEqual to avoid regex issues with SQL queries
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)

	// Prepare all statements
	preparedStatements := make(map[string]*sql.Stmt)
	for queryName, query := range queries {
		mock.ExpectPrepare(query)
		stmt, err := mockDB.Prepare(query)
		require.NoError(t, err, "failed to prepare query: %s", queryName)
		preparedStatements[queryName] = stmt
	}

	db := &PostgresDB{
		db:                 mockDB,
		preparedStatements: preparedStatements,
	}

	cleanup := func() {
		mockDB.Close()
	}

	return db, mock, cleanup
}

func TestGetAllDatasets(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at"}).
		AddRow("dataset-1", "Test Dataset", "Description", time.Now()).
		AddRow("dataset-2", "Another Dataset", nil, time.Now())

	mock.ExpectQuery(queries[getAllDatasetsQuery]).
		WillReturnRows(rows)

	datasets, err := db.GetAllDatasets(context.Background())

	assert.NoError(t, err)
	assert.Len(t, datasets, 2)
	assert.Equal(t, "dataset-1", datasets[0].ID)
	assert.Equal(t, "dataset-2", datasets[1].ID)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAllDatasets_Empty(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at"})

	mock.ExpectQuery(queries[getAllDatasetsQuery]).
		WillReturnRows(rows)

	datasets, err := db.GetAllDatasets(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, datasets)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDatasetIDsByUser(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"stable_id"}).
		AddRow("dataset-1").
		AddRow("dataset-2").
		AddRow("dataset-3")

	mock.ExpectQuery(queries[getDatasetIDsByUserQuery]).
		WithArgs("test-user").
		WillReturnRows(rows)

	datasetIDs, err := db.GetDatasetIDsByUser(context.Background(), "test-user")

	assert.NoError(t, err)
	assert.Len(t, datasetIDs, 3)
	assert.Equal(t, "dataset-1", datasetIDs[0])
	assert.Equal(t, "dataset-2", datasetIDs[1])
	assert.Equal(t, "dataset-3", datasetIDs[2])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDatasetIDsByUser_Empty(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"stable_id"})

	mock.ExpectQuery(queries[getDatasetIDsByUserQuery]).
		WithArgs("test-user").
		WillReturnRows(rows)

	datasetIDs, err := db.GetDatasetIDsByUser(context.Background(), "test-user")

	assert.NoError(t, err)
	assert.Empty(t, datasetIDs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetUserDatasets(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	// Setup expected query - use the full query from the queries map
	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at"}).
		AddRow("dataset-1", "Test Dataset", "Description", time.Now()).
		AddRow("dataset-2", nil, nil, time.Now())

	mock.ExpectQuery(queries[getUserDatasetsQuery]).
		WithArgs(pq.Array([]string{"visa1", "visa2"})).
		WillReturnRows(rows)

	// Execute
	datasets, err := db.GetUserDatasets(context.Background(), []string{"visa1", "visa2"})

	// Assert
	assert.NoError(t, err)
	assert.Len(t, datasets, 2)
	assert.Equal(t, "dataset-1", datasets[0].ID)
	assert.Equal(t, "Test Dataset", datasets[0].Title)
	assert.Equal(t, "dataset-2", datasets[1].ID)
	assert.Empty(t, datasets[1].Title)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetUserDatasets_Empty(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at"})

	mock.ExpectQuery(queries[getUserDatasetsQuery]).
		WithArgs(pq.Array([]string{})).
		WillReturnRows(rows)

	datasets, err := db.GetUserDatasets(context.Background(), []string{})

	assert.NoError(t, err)
	assert.Empty(t, datasets)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDatasetInfo(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	createdAt := time.Now()
	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at", "file_count", "total_size"}).
		AddRow("dataset-1", "Test Dataset", "Description", createdAt, 5, int64(1024000))

	mock.ExpectQuery(queries[getDatasetInfoQuery]).
		WithArgs("dataset-1").
		WillReturnRows(rows)

	info, err := db.GetDatasetInfo(context.Background(), "dataset-1")

	assert.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "dataset-1", info.ID)
	assert.Equal(t, "Test Dataset", info.Title)
	assert.Equal(t, 5, info.FileCount)
	assert.Equal(t, int64(1024000), info.TotalSize)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDatasetInfo_NotFound(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at", "file_count", "total_size"})

	mock.ExpectQuery(queries[getDatasetInfoQuery]).
		WithArgs("nonexistent").
		WillReturnRows(rows)

	info, err := db.GetDatasetInfo(context.Background(), "nonexistent")

	assert.NoError(t, err)
	assert.Nil(t, info)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDatasetFiles(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum", "decrypted_file_checksum_type",
	}).
		AddRow("file-1", "dataset-1", "/path/to/file.txt", "/archive/file.c4gh", int64(1024), int64(900), "abc123", "SHA256").
		AddRow("file-2", "dataset-1", "/path/to/file2.txt", nil, nil, nil, nil, nil)

	mock.ExpectQuery(queries[getDatasetFilesQuery]).
		WithArgs("dataset-1").
		WillReturnRows(rows)

	files, err := db.GetDatasetFiles(context.Background(), "dataset-1")

	assert.NoError(t, err)
	assert.Len(t, files, 2)
	assert.Equal(t, "file-1", files[0].ID)
	assert.Equal(t, "/path/to/file.txt", files[0].SubmittedPath)
	assert.Equal(t, int64(1024), files[0].ArchiveSize)
	assert.Equal(t, "file-2", files[1].ID)
	assert.Empty(t, files[1].ArchivePath)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetFileByID(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	// Header is stored as hex string in database
	headerBytes := []byte{0x63, 0x72, 0x79, 0x70, 0x74, 0x34, 0x67, 0x68} // "crypt4gh"
	headerHex := "6372797074346768"                                       // hex encoding of headerBytes
	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum",
		"decrypted_file_checksum_type", "header",
	}).
		AddRow("file-1", "dataset-1", "/path/to/file.txt", "/archive/file.c4gh",
			int64(1024), int64(900), "abc123", "SHA256", headerHex)

	mock.ExpectQuery(queries[getFileByIDQuery]).
		WithArgs("file-1").
		WillReturnRows(rows)

	file, err := db.GetFileByID(context.Background(), "file-1")

	assert.NoError(t, err)
	require.NotNil(t, file)
	assert.Equal(t, "file-1", file.ID)
	assert.Equal(t, "dataset-1", file.DatasetID)
	assert.Equal(t, headerBytes, file.Header)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetFileByID_NotFound(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum",
		"decrypted_file_checksum_type", "header",
	})

	mock.ExpectQuery(queries[getFileByIDQuery]).
		WithArgs("nonexistent").
		WillReturnRows(rows)

	file, err := db.GetFileByID(context.Background(), "nonexistent")

	assert.NoError(t, err)
	assert.Nil(t, file)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetFileByPath(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	// Header is stored as hex string in database
	headerHex := "6372797074346768"
	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum",
		"decrypted_file_checksum_type", "header",
	}).
		AddRow("file-1", "dataset-1", "/path/to/file.txt", "/archive/file.c4gh",
			int64(1024), int64(900), "abc123", "SHA256", headerHex)

	mock.ExpectQuery(queries[getFileByPathQuery]).
		WithArgs("dataset-1", "/path/to/file.txt").
		WillReturnRows(rows)

	file, err := db.GetFileByPath(context.Background(), "dataset-1", "/path/to/file.txt")

	assert.NoError(t, err)
	require.NotNil(t, file)
	assert.Equal(t, "file-1", file.ID)
	assert.Equal(t, "/path/to/file.txt", file.SubmittedPath)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckFilePermission(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"exists"}).AddRow(true)

	mock.ExpectQuery(queries[checkFilePermissionQuery]).
		WithArgs("file-1", pq.Array([]string{"visa1"})).
		WillReturnRows(rows)

	hasPermission, err := db.CheckFilePermission(context.Background(), "file-1", []string{"visa1"})

	assert.NoError(t, err)
	assert.True(t, hasPermission)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckFilePermission_Denied(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"exists"}).AddRow(false)

	mock.ExpectQuery(queries[checkFilePermissionQuery]).
		WithArgs("file-1", pq.Array([]string{"wrong-visa"})).
		WillReturnRows(rows)

	hasPermission, err := db.CheckFilePermission(context.Background(), "file-1", []string{"wrong-visa"})

	assert.NoError(t, err)
	assert.False(t, hasPermission)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Error handling tests

func TestGetUserDatasets_QueryError(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.ExpectQuery(queries[getUserDatasetsQuery]).
		WithArgs(pq.Array([]string{"visa1"})).
		WillReturnError(sql.ErrConnDone)

	datasets, err := db.GetUserDatasets(context.Background(), []string{"visa1"})

	assert.Error(t, err)
	assert.Nil(t, datasets)
	assert.Contains(t, err.Error(), "failed to query datasets")
}

func TestGetDatasetInfo_QueryError(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.ExpectQuery(queries[getDatasetInfoQuery]).
		WithArgs("dataset-1").
		WillReturnError(sql.ErrConnDone)

	info, err := db.GetDatasetInfo(context.Background(), "dataset-1")

	assert.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "failed to query dataset info")
}

func TestGetDatasetFiles_QueryError(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.ExpectQuery(queries[getDatasetFilesQuery]).
		WithArgs("dataset-1").
		WillReturnError(sql.ErrConnDone)

	files, err := db.GetDatasetFiles(context.Background(), "dataset-1")

	assert.Error(t, err)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "failed to query dataset files")
}

func TestGetDatasetFiles_Empty(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum", "decrypted_file_checksum_type",
	})

	mock.ExpectQuery(queries[getDatasetFilesQuery]).
		WithArgs("empty-dataset").
		WillReturnRows(rows)

	files, err := db.GetDatasetFiles(context.Background(), "empty-dataset")

	assert.NoError(t, err)
	assert.Empty(t, files)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetFileByID_QueryError(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.ExpectQuery(queries[getFileByIDQuery]).
		WithArgs("file-1").
		WillReturnError(sql.ErrConnDone)

	file, err := db.GetFileByID(context.Background(), "file-1")

	assert.Error(t, err)
	assert.Nil(t, file)
	assert.Contains(t, err.Error(), "failed to query file by ID")
}

func TestGetFileByPath_QueryError(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.ExpectQuery(queries[getFileByPathQuery]).
		WithArgs("dataset-1", "/path/file.txt").
		WillReturnError(sql.ErrConnDone)

	file, err := db.GetFileByPath(context.Background(), "dataset-1", "/path/file.txt")

	assert.Error(t, err)
	assert.Nil(t, file)
	assert.Contains(t, err.Error(), "failed to query file by path")
}

func TestGetFileByPath_NotFound(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum",
		"decrypted_file_checksum_type", "header",
	})

	mock.ExpectQuery(queries[getFileByPathQuery]).
		WithArgs("dataset-1", "/nonexistent").
		WillReturnRows(rows)

	file, err := db.GetFileByPath(context.Background(), "dataset-1", "/nonexistent")

	assert.NoError(t, err)
	assert.Nil(t, file)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckFilePermission_QueryError(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.ExpectQuery(queries[checkFilePermissionQuery]).
		WithArgs("file-1", pq.Array([]string{"visa1"})).
		WillReturnError(sql.ErrConnDone)

	hasPermission, err := db.CheckFilePermission(context.Background(), "file-1", []string{"visa1"})

	assert.Error(t, err)
	assert.False(t, hasPermission)
	assert.Contains(t, err.Error(), "failed to check file permission")
}

// Package-level function tests

func TestRegisterAndGetDB(t *testing.T) {
	mockDB := &mockTestDatabase{}

	RegisterDatabase(mockDB)
	defer RegisterDatabase(nil)

	retrieved := GetDB()
	assert.Equal(t, mockDB, retrieved)
}

func TestClose_WithNilDB(t *testing.T) {
	// Save current db
	oldDB := db
	defer func() { db = oldDB }()

	db = nil
	err := Close()
	assert.NoError(t, err)
}

func TestClose_WithDB(t *testing.T) {
	// Save current db
	oldDB := db
	defer func() { db = oldDB }()

	mockDB := &mockTestDatabase{}
	db = mockDB
	err := Close()
	assert.NoError(t, err)
	assert.True(t, mockDB.closed)
}

// mockTestDatabase for package-level function tests
type mockTestDatabase struct {
	closed bool
}

func (m *mockTestDatabase) Close() error {
	m.closed = true

	return nil
}

func (m *mockTestDatabase) GetAllDatasets(_ context.Context) ([]Dataset, error) {
	return nil, nil
}

func (m *mockTestDatabase) GetDatasetIDsByUser(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (m *mockTestDatabase) GetUserDatasets(_ context.Context, _ []string) ([]Dataset, error) {
	return nil, nil
}

func (m *mockTestDatabase) GetDatasetInfo(_ context.Context, _ string) (*DatasetInfo, error) {
	return nil, nil
}

func (m *mockTestDatabase) GetDatasetFiles(_ context.Context, _ string) ([]File, error) {
	return nil, nil
}

func (m *mockTestDatabase) GetFileByID(_ context.Context, _ string) (*File, error) {
	return nil, nil
}

func (m *mockTestDatabase) GetFileByPath(_ context.Context, _, _ string) (*File, error) {
	return nil, nil
}

func (m *mockTestDatabase) CheckFilePermission(_ context.Context, _ string, _ []string) (bool, error) {
	return false, nil
}
