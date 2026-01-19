package database

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserDatasets(t *testing.T) {
	// Create mock database
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	// Setup expected query
	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at"}).
		AddRow("dataset-1", "Test Dataset", "Description", time.Now()).
		AddRow("dataset-2", nil, nil, time.Now())

	mock.ExpectQuery("SELECT DISTINCT d.stable_id").
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
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at"})

	mock.ExpectQuery("SELECT DISTINCT d.stable_id").
		WithArgs(pq.Array([]string{})).
		WillReturnRows(rows)

	datasets, err := db.GetUserDatasets(context.Background(), []string{})

	assert.NoError(t, err)
	assert.Empty(t, datasets)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDatasetInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	createdAt := time.Now()
	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at", "file_count", "total_size"}).
		AddRow("dataset-1", "Test Dataset", "Description", createdAt, 5, int64(1024000))

	mock.ExpectQuery("SELECT").
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
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	rows := sqlmock.NewRows([]string{"stable_id", "title", "description", "created_at", "file_count", "total_size"})

	mock.ExpectQuery("SELECT").
		WithArgs("nonexistent").
		WillReturnRows(rows)

	info, err := db.GetDatasetInfo(context.Background(), "nonexistent")

	assert.NoError(t, err)
	assert.Nil(t, info)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDatasetFiles(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum", "decrypted_file_checksum_type",
	}).
		AddRow("file-1", "dataset-1", "/path/to/file.txt", "/archive/file.c4gh", int64(1024), int64(900), "abc123", "SHA256").
		AddRow("file-2", "dataset-1", "/path/to/file2.txt", nil, nil, nil, nil, nil)

	mock.ExpectQuery("SELECT").
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
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	header := []byte{0x63, 0x72, 0x79, 0x70, 0x74, 0x34, 0x67, 0x68} // "crypt4gh"
	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum",
		"decrypted_file_checksum_type", "header",
	}).
		AddRow("file-1", "dataset-1", "/path/to/file.txt", "/archive/file.c4gh",
			int64(1024), int64(900), "abc123", "SHA256", header)

	mock.ExpectQuery("SELECT").
		WithArgs("file-1").
		WillReturnRows(rows)

	file, err := db.GetFileByID(context.Background(), "file-1")

	assert.NoError(t, err)
	require.NotNil(t, file)
	assert.Equal(t, "file-1", file.ID)
	assert.Equal(t, "dataset-1", file.DatasetID)
	assert.Equal(t, header, file.Header)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetFileByID_NotFound(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum",
		"decrypted_file_checksum_type", "header",
	})

	mock.ExpectQuery("SELECT").
		WithArgs("nonexistent").
		WillReturnRows(rows)

	file, err := db.GetFileByID(context.Background(), "nonexistent")

	assert.NoError(t, err)
	assert.Nil(t, file)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetFileByPath(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	header := []byte{0x63, 0x72, 0x79, 0x70, 0x74, 0x34, 0x67, 0x68}
	rows := sqlmock.NewRows([]string{
		"stable_id", "dataset_id", "submission_file_path", "archive_file_path",
		"archive_file_size", "decrypted_file_size", "decrypted_file_checksum",
		"decrypted_file_checksum_type", "header",
	}).
		AddRow("file-1", "dataset-1", "/path/to/file.txt", "/archive/file.c4gh",
			int64(1024), int64(900), "abc123", "SHA256", header)

	mock.ExpectQuery("SELECT").
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
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	rows := sqlmock.NewRows([]string{"exists"}).AddRow(true)

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("file-1", pq.Array([]string{"visa1"})).
		WillReturnRows(rows)

	hasPermission, err := db.CheckFilePermission(context.Background(), "file-1", []string{"visa1"})

	assert.NoError(t, err)
	assert.True(t, hasPermission)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckFilePermission_Denied(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	db := &PostgresDB{db: mockDB}

	rows := sqlmock.NewRows([]string{"exists"}).AddRow(false)

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("file-1", pq.Array([]string{"wrong-visa"})).
		WillReturnRows(rows)

	hasPermission, err := db.CheckFilePermission(context.Background(), "file-1", []string{"wrong-visa"})

	assert.NoError(t, err)
	assert.False(t, hasPermission)
	assert.NoError(t, mock.ExpectationsWereMet())
}
