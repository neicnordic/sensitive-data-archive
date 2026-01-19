// Package database provides database operations for the download service.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	log "github.com/sirupsen/logrus"
)

// Query name constants
const (
	getUserDatasetsQuery     = "getUserDatasets"
	getDatasetInfoQuery      = "getDatasetInfo"
	getDatasetFilesQuery     = "getDatasetFiles"
	getFileByIDQuery         = "getFileByID"
	getFileByPathQuery       = "getFileByPath"
	checkFilePermissionQuery = "checkFilePermission"
)

// queries contains all SQL queries used by the download service.
// These are prepared at startup to verify correctness and improve performance.
var queries = map[string]string{
	getUserDatasetsQuery: `
		SELECT DISTINCT d.stable_id, d.title, d.description, d.created_at
		FROM sda.datasets d
		INNER JOIN sda.dataset_permission_table dp ON d.id = dp.dataset_id
		WHERE dp.title = ANY($1)
		ORDER BY d.created_at DESC`,

	getDatasetInfoQuery: `
		SELECT
			d.stable_id,
			d.title,
			d.description,
			d.created_at,
			COUNT(f.id) as file_count,
			COALESCE(SUM(f.decrypted_file_size), 0) as total_size
		FROM sda.datasets d
		LEFT JOIN sda.file_dataset fd ON d.id = fd.dataset_id
		LEFT JOIN sda.files f ON fd.file_id = f.id
		WHERE d.stable_id = $1
		GROUP BY d.id, d.stable_id, d.title, d.description, d.created_at`,

	getDatasetFilesQuery: `
		SELECT
			f.stable_id,
			d.stable_id as dataset_id,
			f.submission_file_path,
			f.archive_file_path,
			f.archive_file_size,
			f.decrypted_file_size,
			f.decrypted_file_checksum,
			f.decrypted_file_checksum_type
		FROM sda.files f
		INNER JOIN sda.file_dataset fd ON f.id = fd.file_id
		INNER JOIN sda.datasets d ON fd.dataset_id = d.id
		WHERE d.stable_id = $1 AND f.stable_id IS NOT NULL
		ORDER BY f.submission_file_path`,

	getFileByIDQuery: `
		SELECT
			f.stable_id,
			d.stable_id as dataset_id,
			f.submission_file_path,
			f.archive_file_path,
			f.archive_file_size,
			f.decrypted_file_size,
			f.decrypted_file_checksum,
			f.decrypted_file_checksum_type,
			f.header
		FROM sda.files f
		INNER JOIN sda.file_dataset fd ON f.id = fd.file_id
		INNER JOIN sda.datasets d ON fd.dataset_id = d.id
		WHERE f.stable_id = $1`,

	getFileByPathQuery: `
		SELECT
			f.stable_id,
			d.stable_id as dataset_id,
			f.submission_file_path,
			f.archive_file_path,
			f.archive_file_size,
			f.decrypted_file_size,
			f.decrypted_file_checksum,
			f.decrypted_file_checksum_type,
			f.header
		FROM sda.files f
		INNER JOIN sda.file_dataset fd ON f.id = fd.file_id
		INNER JOIN sda.datasets d ON fd.dataset_id = d.id
		WHERE d.stable_id = $1 AND f.submission_file_path = $2`,

	checkFilePermissionQuery: `
		SELECT EXISTS(
			SELECT 1
			FROM sda.files f
			INNER JOIN sda.file_dataset fd ON f.id = fd.file_id
			INNER JOIN sda.dataset_permission_table dp ON fd.dataset_id = dp.dataset_id
			WHERE f.stable_id = $1 AND dp.title = ANY($2)
		)`,
}

// Database defines the interface for download service database operations.
type Database interface {
	// Close closes the database connection.
	Close() error

	// GetUserDatasets returns datasets the user has access to based on their visas.
	GetUserDatasets(ctx context.Context, visas []string) ([]Dataset, error)

	// GetDatasetInfo returns metadata for a specific dataset.
	GetDatasetInfo(ctx context.Context, datasetID string) (*DatasetInfo, error)

	// GetDatasetFiles returns files in a dataset.
	GetDatasetFiles(ctx context.Context, datasetID string) ([]File, error)

	// GetFileByID returns file information by stable ID.
	GetFileByID(ctx context.Context, fileID string) (*File, error)

	// GetFileByPath returns file information by dataset and submitted path.
	GetFileByPath(ctx context.Context, datasetID, filePath string) (*File, error)

	// CheckFilePermission verifies the user has access to download a file.
	CheckFilePermission(ctx context.Context, fileID string, visas []string) (bool, error)
}

// Dataset represents a dataset the user has access to.
type Dataset struct {
	ID          string    `json:"id"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// DatasetInfo contains metadata about a dataset.
type DatasetInfo struct {
	ID          string    `json:"id"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	FileCount   int       `json:"fileCount"`
	TotalSize   int64     `json:"totalSize"`
	CreatedAt   time.Time `json:"createdAt"`
}

// File represents a file in the archive.
type File struct {
	ID                    string `json:"fileId"`
	DatasetID             string `json:"datasetId"`
	SubmittedPath         string `json:"filePath"`
	ArchivePath           string `json:"-"`
	ArchiveSize           int64  `json:"archiveSize"`
	DecryptedSize         int64  `json:"decryptedSize"`
	DecryptedChecksum     string `json:"decryptedChecksum"`
	DecryptedChecksumType string `json:"decryptedChecksumType"`
	Header                []byte `json:"-"`
}

// PostgresDB implements the Database interface for PostgreSQL.
type PostgresDB struct {
	db                 *sql.DB
	preparedStatements map[string]*sql.Stmt
}

var db Database

// RegisterDatabase registers the database implementation to be used.
func RegisterDatabase(d Database) {
	db = d
}

// GetDB returns the registered database instance.
func GetDB() Database {
	return db
}

// Init initializes the database connection using configuration values.
// All queries are prepared at startup to verify correctness and improve runtime performance.
func Init() error {
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.DBHost(),
		config.DBPort(),
		config.DBUser(),
		config.DBPassword(),
		config.DBDatabase(),
		config.DBSSLMode(),
	)

	if config.DBCACert() != "" {
		connStr += fmt.Sprintf(" sslrootcert=%s", config.DBCACert())
	}

	sqlDB, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("Database connection established")

	// Prepare all statements to verify correctness and improve performance
	preparedStatements := make(map[string]*sql.Stmt)
	for queryName, query := range queries {
		preparedStmt, err := sqlDB.PrepareContext(ctx, query)
		if err != nil {
			log.Errorf("failed to prepare query: %s, due to: %v", queryName, err)

			return errors.Join(fmt.Errorf("failed to prepare query: %s", queryName), err)
		}
		preparedStatements[queryName] = preparedStmt
		log.Debugf("Prepared query: %s", queryName)
	}

	log.Infof("Successfully prepared %d database queries", len(preparedStatements))

	RegisterDatabase(&PostgresDB{
		db:                 sqlDB,
		preparedStatements: preparedStatements,
	})

	return nil
}

// Close closes the database connection.
func Close() error {
	if db != nil {
		return db.Close()
	}

	return nil
}

// Close closes the PostgreSQL database connection and all prepared statements.
func (p *PostgresDB) Close() error {
	// Close all prepared statements first
	for name, stmt := range p.preparedStatements {
		if err := stmt.Close(); err != nil {
			log.Warnf("failed to close prepared statement %s: %v", name, err)
		}
	}

	return p.db.Close()
}

// GetUserDatasets returns datasets the user has access to based on their visas.
func (p *PostgresDB) GetUserDatasets(ctx context.Context, visas []string) ([]Dataset, error) {
	stmt := p.preparedStatements[getUserDatasetsQuery]
	rows, err := stmt.QueryContext(ctx, pq.Array(visas))
	if err != nil {
		return nil, fmt.Errorf("failed to query datasets: %w", err)
	}
	defer rows.Close()

	var datasets []Dataset
	for rows.Next() {
		var d Dataset
		var title, description sql.NullString
		if err := rows.Scan(&d.ID, &title, &description, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan dataset row: %w", err)
		}
		d.Title = title.String
		d.Description = description.String
		datasets = append(datasets, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating dataset rows: %w", err)
	}

	return datasets, nil
}

// GetDatasetInfo returns metadata for a specific dataset.
func (p *PostgresDB) GetDatasetInfo(ctx context.Context, datasetID string) (*DatasetInfo, error) {
	stmt := p.preparedStatements[getDatasetInfoQuery]

	var info DatasetInfo
	var title, description sql.NullString
	err := stmt.QueryRowContext(ctx, datasetID).Scan(
		&info.ID,
		&title,
		&description,
		&info.CreatedAt,
		&info.FileCount,
		&info.TotalSize,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query dataset info: %w", err)
	}

	info.Title = title.String
	info.Description = description.String

	return &info, nil
}

// GetDatasetFiles returns files in a dataset.
func (p *PostgresDB) GetDatasetFiles(ctx context.Context, datasetID string) ([]File, error) {
	stmt := p.preparedStatements[getDatasetFilesQuery]
	rows, err := stmt.QueryContext(ctx, datasetID)
	if err != nil {
		return nil, fmt.Errorf("failed to query dataset files: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		var archivePath, decryptedChecksum, decryptedChecksumType sql.NullString
		var archiveSize, decryptedSize sql.NullInt64
		if err := rows.Scan(
			&f.ID,
			&f.DatasetID,
			&f.SubmittedPath,
			&archivePath,
			&archiveSize,
			&decryptedSize,
			&decryptedChecksum,
			&decryptedChecksumType,
		); err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}
		f.ArchivePath = archivePath.String
		f.ArchiveSize = archiveSize.Int64
		f.DecryptedSize = decryptedSize.Int64
		f.DecryptedChecksum = decryptedChecksum.String
		f.DecryptedChecksumType = decryptedChecksumType.String
		files = append(files, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating file rows: %w", err)
	}

	return files, nil
}

// GetFileByID returns file information by stable ID.
func (p *PostgresDB) GetFileByID(ctx context.Context, fileID string) (*File, error) {
	stmt := p.preparedStatements[getFileByIDQuery]

	var f File
	var archivePath, decryptedChecksum, decryptedChecksumType sql.NullString
	var archiveSize, decryptedSize sql.NullInt64
	err := stmt.QueryRowContext(ctx, fileID).Scan(
		&f.ID,
		&f.DatasetID,
		&f.SubmittedPath,
		&archivePath,
		&archiveSize,
		&decryptedSize,
		&decryptedChecksum,
		&decryptedChecksumType,
		&f.Header,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query file by ID: %w", err)
	}

	f.ArchivePath = archivePath.String
	f.ArchiveSize = archiveSize.Int64
	f.DecryptedSize = decryptedSize.Int64
	f.DecryptedChecksum = decryptedChecksum.String
	f.DecryptedChecksumType = decryptedChecksumType.String

	return &f, nil
}

// GetFileByPath returns file information by dataset and submitted path.
func (p *PostgresDB) GetFileByPath(ctx context.Context, datasetID, filePath string) (*File, error) {
	stmt := p.preparedStatements[getFileByPathQuery]

	var f File
	var archivePath, decryptedChecksum, decryptedChecksumType sql.NullString
	var archiveSize, decryptedSize sql.NullInt64
	err := stmt.QueryRowContext(ctx, datasetID, filePath).Scan(
		&f.ID,
		&f.DatasetID,
		&f.SubmittedPath,
		&archivePath,
		&archiveSize,
		&decryptedSize,
		&decryptedChecksum,
		&decryptedChecksumType,
		&f.Header,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query file by path: %w", err)
	}

	f.ArchivePath = archivePath.String
	f.ArchiveSize = archiveSize.Int64
	f.DecryptedSize = decryptedSize.Int64
	f.DecryptedChecksum = decryptedChecksum.String
	f.DecryptedChecksumType = decryptedChecksumType.String

	return &f, nil
}

// CheckFilePermission verifies the user has access to download a file.
func (p *PostgresDB) CheckFilePermission(ctx context.Context, fileID string, visas []string) (bool, error) {
	stmt := p.preparedStatements[checkFilePermissionQuery]

	var hasPermission bool
	err := stmt.QueryRowContext(ctx, fileID, pq.Array(visas)).Scan(&hasPermission)
	if err != nil {
		return false, fmt.Errorf("failed to check file permission: %w", err)
	}

	return hasPermission, nil
}

// Package-level functions that delegate to the registered database

// GetUserDatasets returns datasets the user has access to based on their visas.
func GetUserDatasets(ctx context.Context, visas []string) ([]Dataset, error) {
	return db.GetUserDatasets(ctx, visas)
}

// GetDatasetInfo returns metadata for a specific dataset.
func GetDatasetInfo(ctx context.Context, datasetID string) (*DatasetInfo, error) {
	return db.GetDatasetInfo(ctx, datasetID)
}

// GetDatasetFiles returns files in a dataset.
func GetDatasetFiles(ctx context.Context, datasetID string) ([]File, error) {
	return db.GetDatasetFiles(ctx, datasetID)
}

// GetFileByID returns file information by stable ID.
func GetFileByID(ctx context.Context, fileID string) (*File, error) {
	return db.GetFileByID(ctx, fileID)
}

// GetFileByPath returns file information by dataset and submitted path.
func GetFileByPath(ctx context.Context, datasetID, filePath string) (*File, error) {
	return db.GetFileByPath(ctx, datasetID, filePath)
}

// CheckFilePermission verifies the user has access to download a file.
func CheckFilePermission(ctx context.Context, fileID string, visas []string) (bool, error) {
	return db.CheckFilePermission(ctx, fileID, visas)
}
