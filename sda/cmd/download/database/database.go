// Package database provides database operations for the download service.
package database

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	log "github.com/sirupsen/logrus"
)

// Query name constants
const (
	getAllDatasetsQuery              = "getAllDatasets"
	getDatasetIDsByUserQuery         = "getDatasetIDsByUser"
	getUserDatasetsQuery             = "getUserDatasets"
	getDatasetInfoQuery              = "getDatasetInfo"
	getFileByIDQuery                 = "getFileByID"
	getFileByPathQuery               = "getFileByPath"
	checkFilePermissionQuery         = "checkFilePermission"
	checkDatasetExistsQuery          = "checkDatasetExists"
	getDatasetFilesPageQuery         = "getDatasetFilesPage"
	getDatasetFilesPageByPathQuery   = "getDatasetFilesPageByPath"
	getDatasetFilesPageByPrefixQuery = "getDatasetFilesPageByPrefix"
	getFileChecksumsQuery            = "getFileChecksums"
)

// paginatedFileBase is the shared SELECT+JOIN+LATERAL block for keyset-paginated
// dataset file queries. Each variant appends its own WHERE filter and LIMIT clause.
const paginatedFileBase = `
		SELECT f.stable_id, f.submission_file_path, f.archive_file_size,
		       f.decrypted_file_size, cs.checksums
		FROM sda.files f
		INNER JOIN sda.file_dataset fd ON f.id = fd.file_id
		INNER JOIN sda.datasets d ON fd.dataset_id = d.id
		LEFT JOIN LATERAL (
		    SELECT json_agg(json_build_object('type', lower(c.type::text), 'checksum', c.checksum)) AS checksums
		    FROM sda.checksums c WHERE c.file_id = f.id AND c.source = 'UNENCRYPTED'
		) cs ON true
		WHERE d.stable_id = $1
		  AND f.stable_id IS NOT NULL`

// queries contains all SQL queries used by the download service.
// These are prepared at startup to verify correctness and improve performance.
var queries = map[string]string{
	// getAllDatasets returns all datasets (for allow-all-data mode)
	getAllDatasetsQuery: `
		SELECT DISTINCT d.stable_id, d.title, d.description, d.created_at
		FROM sda.datasets d
		ORDER BY d.created_at DESC`,

	// getDatasetIDsByUser returns dataset stable_ids where the user is the submission_user
	// This follows the data ownership model used by the API service
	getDatasetIDsByUserQuery: `
		SELECT DISTINCT d.stable_id
		FROM sda.datasets d
		INNER JOIN sda.file_dataset fd ON d.id = fd.dataset_id
		INNER JOIN sda.files f ON fd.file_id = f.id
		WHERE f.submission_user = $1 AND f.stable_id IS NOT NULL`,

	// getUserDatasets returns datasets where the stable_id matches any of the allowed dataset IDs
	getUserDatasetsQuery: `
		SELECT DISTINCT d.stable_id, d.title, d.description, d.created_at
		FROM sda.datasets d
		WHERE d.stable_id = ANY($1)
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

	getFileByIDQuery: `
		SELECT
			f.stable_id,
			d.stable_id as dataset_id,
			f.submission_file_path,
			f.archive_file_path,
			f.archive_location,
			f.archive_file_size,
			f.decrypted_file_size,
			c.checksum as decrypted_checksum,
			c.type as decrypted_checksum_type,
			f.header,
			f.created_at
		FROM sda.files f
		INNER JOIN sda.file_dataset fd ON f.id = fd.file_id
		INNER JOIN sda.datasets d ON fd.dataset_id = d.id
		LEFT JOIN sda.checksums c ON f.id = c.file_id AND c.source = 'UNENCRYPTED'
		WHERE f.stable_id = $1`,

	getFileByPathQuery: `
		SELECT
			f.stable_id,
			d.stable_id as dataset_id,
			f.submission_file_path,
			f.archive_file_path,
			f.archive_location,
			f.archive_file_size,
			f.decrypted_file_size,
			c.checksum as decrypted_checksum,
			c.type as decrypted_checksum_type,
			f.header,
			f.created_at
		FROM sda.files f
		INNER JOIN sda.file_dataset fd ON f.id = fd.file_id
		INNER JOIN sda.datasets d ON fd.dataset_id = d.id
		LEFT JOIN sda.checksums c ON f.id = c.file_id AND c.source = 'UNENCRYPTED'
		WHERE d.stable_id = $1 AND f.submission_file_path = $2`,

	// checkFilePermission verifies user has access to the file's dataset
	// by checking if the dataset stable_id is in the user's visas
	checkFilePermissionQuery: `
		SELECT EXISTS(
			SELECT 1
			FROM sda.files f
			INNER JOIN sda.file_dataset fd ON f.id = fd.file_id
			INNER JOIN sda.datasets d ON fd.dataset_id = d.id
			WHERE f.stable_id = $1 AND d.stable_id = ANY($2)
		)`,

	// checkDatasetExists checks if a dataset with the given stable_id exists
	checkDatasetExistsQuery: `
		SELECT EXISTS(
			SELECT 1
			FROM sda.datasets d
			WHERE d.stable_id = $1
		)`,

	// getFileChecksums returns checksums for a file filtered by source (e.g., "ARCHIVED", "UNENCRYPTED").
	getFileChecksumsQuery: `
		SELECT c.checksum, c.type
		FROM sda.checksums c
		INNER JOIN sda.files f ON c.file_id = f.id
		WHERE f.stable_id = $1 AND c.source = $2`,

	// Keyset-paginated file queries compose from paginatedFileBase (defined below).

	// getDatasetFilesPage returns paginated files in a dataset (no path filter).
	// Keyset cursor on (submission_file_path, stable_id). $2='' means first page.
	getDatasetFilesPageQuery: paginatedFileBase + `
		  AND ($2 = '' OR (f.submission_file_path, f.stable_id) > ($2, $3))
		ORDER BY f.submission_file_path, f.stable_id
		LIMIT $4`,

	// getDatasetFilesPageByPath returns a file by exact path (at most 1 result).
	getDatasetFilesPageByPathQuery: paginatedFileBase + `
		  AND f.submission_file_path = $2
		LIMIT $3`,

	// getDatasetFilesPageByPrefix returns paginated files matching a path prefix.
	// Keyset cursor on (submission_file_path, stable_id). $3='' means first page.
	getDatasetFilesPageByPrefixQuery: paginatedFileBase + `
		  AND f.submission_file_path LIKE $2 ESCAPE '\'
		  AND ($3 = '' OR (f.submission_file_path, f.stable_id) > ($3, $4))
		ORDER BY f.submission_file_path, f.stable_id
		LIMIT $5`,
}

// Checksum represents a file checksum with its algorithm type.
type Checksum struct {
	Type     string `json:"type"`
	Checksum string `json:"checksum"`
}

// FileListOptions controls filtering and pagination for GetDatasetFilesPaginated.
type FileListOptions struct {
	FilePath   string // exact match (mutually exclusive with PathPrefix)
	PathPrefix string // prefix match
	Limit      int    // pageSize + 1 (fetch one extra to detect next page)
	CursorPath string // last-seen submission_file_path ("" for first page)
	CursorID   string // tie-breaker: last-seen stable_id ("" for first page)
}

// Database defines the interface for download service database operations.
type Database interface {
	// Ping verifies the database connection is alive.
	Ping(ctx context.Context) error

	// Close closes the database connection.
	Close() error

	// GetAllDatasets returns all datasets (for allow-all-data mode).
	GetAllDatasets(ctx context.Context) ([]Dataset, error)

	// GetDatasetIDsByUser returns dataset IDs where the user is the submission_user (data owner).
	// This follows the data ownership model - users have access to datasets containing files they submitted.
	GetDatasetIDsByUser(ctx context.Context, user string) ([]string, error)

	// GetUserDatasets returns datasets the user has access to based on their allowed dataset IDs.
	GetUserDatasets(ctx context.Context, datasetIDs []string) ([]Dataset, error)

	// GetDatasetInfo returns metadata for a specific dataset.
	GetDatasetInfo(ctx context.Context, datasetID string) (*DatasetInfo, error)

	// GetFileByID returns file information by stable ID.
	GetFileByID(ctx context.Context, fileID string) (*File, error)

	// GetFileByPath returns file information by dataset and submitted path.
	GetFileByPath(ctx context.Context, datasetID, filePath string) (*File, error)

	// CheckFilePermission verifies the user has access to download a file.
	CheckFilePermission(ctx context.Context, fileID string, datasetIDs []string) (bool, error)

	// CheckDatasetExists checks if a dataset with the given stable_id exists.
	CheckDatasetExists(ctx context.Context, datasetID string) (bool, error)

	// GetFileChecksums returns checksums for a file filtered by source (e.g., "ARCHIVED", "UNENCRYPTED").
	GetFileChecksums(ctx context.Context, fileID string, source string) ([]Checksum, error)

	// GetDatasetFilesPaginated returns files in a dataset with keyset cursor pagination.
	// Files are returned with aggregated checksums. Use FileListOptions to filter and paginate.
	GetDatasetFilesPaginated(ctx context.Context, datasetID string, opts FileListOptions) ([]File, error)
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
	ID                    string     `json:"fileId"`
	DatasetID             string     `json:"datasetId"`
	SubmittedPath         string     `json:"filePath"`
	ArchivePath           string     `json:"-"`
	ArchiveLocation       string     `json:"-"` // Storage backend location (e.g., "s3:9000/archive" or "/archive")
	ArchiveSize           int64      `json:"archiveSize"`
	DecryptedSize         int64      `json:"decryptedSize"`
	DecryptedChecksum     string     `json:"decryptedChecksum"`
	DecryptedChecksumType string     `json:"decryptedChecksumType"`
	Header                []byte     `json:"-"`
	CreatedAt             time.Time  `json:"-"`
	Checksums             []Checksum `json:"checksums,omitempty"` // Aggregated checksums (from paginated queries)
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
	if config.DBSSLMode() == "verify-full" {
		if config.DBClientCert() == "" || config.DBClientKey() == "" {
			return errors.New("both db.clientcert and db.clientkey are required when db.sslmode=verify-full")
		}
	}

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

	if config.DBClientCert() != "" {
		connStr += fmt.Sprintf(" sslcert=%s", config.DBClientCert())
	}

	if config.DBClientKey() != "" {
		connStr += fmt.Sprintf(" sslkey=%s", config.DBClientKey())
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

// Ping verifies the database connection is alive.
func (p *PostgresDB) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
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

// GetAllDatasets returns all datasets (for allow-all-data mode).
func (p *PostgresDB) GetAllDatasets(ctx context.Context) ([]Dataset, error) {
	stmt := p.preparedStatements[getAllDatasetsQuery]
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query all datasets: %w", err)
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

// GetDatasetIDsByUser returns dataset IDs where the user is the submission_user (data owner).
func (p *PostgresDB) GetDatasetIDsByUser(ctx context.Context, user string) ([]string, error) {
	stmt := p.preparedStatements[getDatasetIDsByUserQuery]
	rows, err := stmt.QueryContext(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to query datasets by user: %w", err)
	}
	defer rows.Close()

	var datasetIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan dataset ID: %w", err)
		}
		datasetIDs = append(datasetIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating dataset ID rows: %w", err)
	}

	return datasetIDs, nil
}

// GetUserDatasets returns datasets the user has access to based on their allowed dataset IDs.
func (p *PostgresDB) GetUserDatasets(ctx context.Context, datasetIDs []string) ([]Dataset, error) {
	stmt := p.preparedStatements[getUserDatasetsQuery]
	rows, err := stmt.QueryContext(ctx, pq.Array(datasetIDs))
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
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query dataset info: %w", err)
	}

	info.Title = title.String
	info.Description = description.String

	return &info, nil
}

// GetFileByID returns file information by stable ID.
func (p *PostgresDB) GetFileByID(ctx context.Context, fileID string) (*File, error) {
	stmt := p.preparedStatements[getFileByIDQuery]

	var f File
	var archivePath, archiveLocation, decryptedChecksum, decryptedChecksumType, headerHex sql.NullString
	var archiveSize, decryptedSize sql.NullInt64
	err := stmt.QueryRowContext(ctx, fileID).Scan(
		&f.ID,
		&f.DatasetID,
		&f.SubmittedPath,
		&archivePath,
		&archiveLocation,
		&archiveSize,
		&decryptedSize,
		&decryptedChecksum,
		&decryptedChecksumType,
		&headerHex,
		&f.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query file by ID: %w", err)
	}

	f.ArchivePath = archivePath.String
	f.ArchiveLocation = archiveLocation.String
	f.ArchiveSize = archiveSize.Int64
	f.DecryptedSize = decryptedSize.Int64
	f.DecryptedChecksum = decryptedChecksum.String
	f.DecryptedChecksumType = decryptedChecksumType.String

	// Header is stored as hex string in database, decode it.
	// The ingest pipeline (sda/internal/database/db_functions.go:StoreHeader) stores the
	// crypt4gh file header as a hex-encoded string using hex.EncodeToString().
	// After decoding, the header bytes represent the complete crypt4gh header which includes:
	// - magic number (8 bytes: "crypt4gh")
	// - version (4 bytes)
	// - header packet count (4 bytes)
	// - header packets (variable length, contains encryption info)
	// NOTE: If you encounter "encoding/hex: invalid byte" errors, ensure the header was
	// stored using hex encoding, not base64. The SDA pipeline consistently uses hex.
	if headerHex.Valid && headerHex.String != "" {
		f.Header, err = hex.DecodeString(headerHex.String)
		if err != nil {
			return nil, fmt.Errorf("failed to decode header from hex (expected hex-encoded string, got %d chars): %w",
				len(headerHex.String), err)
		}
		// Validate header has minimum crypt4gh structure (magic + version + count = 16 bytes minimum)
		if len(f.Header) < 16 {
			return nil, fmt.Errorf("decoded header too small (%d bytes), expected at least 16 bytes for crypt4gh", len(f.Header))
		}
	}

	return &f, nil
}

// GetFileByPath returns file information by dataset and submitted path.
func (p *PostgresDB) GetFileByPath(ctx context.Context, datasetID, filePath string) (*File, error) {
	stmt := p.preparedStatements[getFileByPathQuery]

	var f File
	var archivePath, archiveLocation, decryptedChecksum, decryptedChecksumType, headerHex sql.NullString
	var archiveSize, decryptedSize sql.NullInt64
	err := stmt.QueryRowContext(ctx, datasetID, filePath).Scan(
		&f.ID,
		&f.DatasetID,
		&f.SubmittedPath,
		&archivePath,
		&archiveLocation,
		&archiveSize,
		&decryptedSize,
		&decryptedChecksum,
		&decryptedChecksumType,
		&headerHex,
		&f.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query file by path: %w", err)
	}

	f.ArchivePath = archivePath.String
	f.ArchiveLocation = archiveLocation.String
	f.ArchiveSize = archiveSize.Int64
	f.DecryptedSize = decryptedSize.Int64
	f.DecryptedChecksum = decryptedChecksum.String
	f.DecryptedChecksumType = decryptedChecksumType.String

	// Header is stored as hex string in database, decode it.
	// See GetFileByID for details on the header format and encoding.
	if headerHex.Valid && headerHex.String != "" {
		f.Header, err = hex.DecodeString(headerHex.String)
		if err != nil {
			return nil, fmt.Errorf("failed to decode header from hex (expected hex-encoded string, got %d chars): %w",
				len(headerHex.String), err)
		}
		// Validate header has minimum crypt4gh structure
		if len(f.Header) < 16 {
			return nil, fmt.Errorf("decoded header too small (%d bytes), expected at least 16 bytes for crypt4gh", len(f.Header))
		}
	}

	return &f, nil
}

// CheckFilePermission verifies the user has access to download a file.
func (p *PostgresDB) CheckFilePermission(ctx context.Context, fileID string, datasetIDs []string) (bool, error) {
	stmt := p.preparedStatements[checkFilePermissionQuery]

	var hasPermission bool
	err := stmt.QueryRowContext(ctx, fileID, pq.Array(datasetIDs)).Scan(&hasPermission)
	if err != nil {
		return false, fmt.Errorf("failed to check file permission: %w", err)
	}

	return hasPermission, nil
}

// CheckDatasetExists checks if a dataset with the given stable_id exists.
func (p *PostgresDB) CheckDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	stmt := p.preparedStatements[checkDatasetExistsQuery]

	var exists bool
	err := stmt.QueryRowContext(ctx, datasetID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check dataset existence: %w", err)
	}

	return exists, nil
}

// GetFileChecksums returns checksums for a file filtered by source (e.g., "ARCHIVED", "UNENCRYPTED").
func (p *PostgresDB) GetFileChecksums(ctx context.Context, fileID string, source string) ([]Checksum, error) {
	stmt := p.preparedStatements[getFileChecksumsQuery]
	rows, err := stmt.QueryContext(ctx, fileID, source)
	if err != nil {
		return nil, fmt.Errorf("failed to query file checksums: %w", err)
	}
	defer rows.Close()

	var checksums []Checksum
	for rows.Next() {
		var c Checksum
		if err := rows.Scan(&c.Checksum, &c.Type); err != nil {
			return nil, fmt.Errorf("failed to scan checksum row: %w", err)
		}
		checksums = append(checksums, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating checksum rows: %w", err)
	}

	return checksums, nil
}

// GetDatasetFilesPaginated returns files with keyset cursor pagination and aggregated checksums.
func (p *PostgresDB) GetDatasetFilesPaginated(ctx context.Context, datasetID string, opts FileListOptions) ([]File, error) {
	var rows *sql.Rows
	var err error

	switch {
	case opts.FilePath != "":
		stmt := p.preparedStatements[getDatasetFilesPageByPathQuery]
		rows, err = stmt.QueryContext(ctx, datasetID, opts.FilePath, opts.Limit)
	case opts.PathPrefix != "":
		stmt := p.preparedStatements[getDatasetFilesPageByPrefixQuery]
		escaped := escapeLikePrefix(opts.PathPrefix)
		rows, err = stmt.QueryContext(ctx, datasetID, escaped, opts.CursorPath, opts.CursorID, opts.Limit)
	default:
		stmt := p.preparedStatements[getDatasetFilesPageQuery]
		rows, err = stmt.QueryContext(ctx, datasetID, opts.CursorPath, opts.CursorID, opts.Limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query paginated dataset files: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		var archiveSize, decryptedSize sql.NullInt64
		var checksumsJSON []byte

		if err := rows.Scan(&f.ID, &f.SubmittedPath, &archiveSize, &decryptedSize, &checksumsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan paginated file row: %w", err)
		}

		f.ArchiveSize = archiveSize.Int64
		f.DecryptedSize = decryptedSize.Int64

		if checksumsJSON != nil {
			if err := json.Unmarshal(checksumsJSON, &f.Checksums); err != nil {
				return nil, fmt.Errorf("failed to unmarshal checksums for file %s: %w", f.ID, err)
			}
		}

		files = append(files, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating paginated file rows: %w", err)
	}

	return files, nil
}

// escapeLikePrefix escapes SQL LIKE wildcards in a prefix and appends %.
func escapeLikePrefix(prefix string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

	return r.Replace(prefix) + "%"
}
