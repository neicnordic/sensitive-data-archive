package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/lib/pq" // Import pg driver
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	log "github.com/sirupsen/logrus"
)

type pgDb struct {
	db                 *sql.DB
	config             *dbConfig
	schemaVersion      int
	preparedStatements map[string]*sql.Stmt
}

const getSchemaVersionQuery = "getSchemaVersion"

var queries = make(map[string]string)

func init() {
	queries[getSchemaVersionQuery] = `
SELECT MAX(version) 
FROM sda.dbschema_version;
`
}

func NewPostgresSQLDatabase(options ...func(config *dbConfig)) (database.Database, error) {
	dbConf := globalConf.clone()

	for _, o := range options {
		o(dbConf)
	}

	pg := &pgDb{db: nil, config: dbConf}

	var err error
	pg.db, err = sql.Open("postgres", pg.config.dataSourceName())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := pg.db.Ping(); err != nil {
		_ = pg.db.Close()

		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Prepare the statements from the queries
	pg.preparedStatements = make(map[string]*sql.Stmt)
	for queryName, query := range queries {
		preparedStmt, err := pg.db.Prepare(query)
		if err != nil {
			log.Errorf("failed to prepare query: %s, due to: %v", queryName, err)
			_ = pg.Close()

			return nil, fmt.Errorf("failed to prepare query: %s, due to: %w", queryName, err)
		}
		pg.preparedStatements[queryName] = preparedStmt
	}

	stmt := pg.preparedStatements[getSchemaVersionQuery]

	if err := stmt.QueryRow().Scan(&pg.schemaVersion); err != nil {
		_ = pg.Close()

		return nil, fmt.Errorf("failed to query schema version: %w", err)
	}

	return pg, nil
}

func (db *pgDb) SchemaVersion() (int, error) {
	if db.db == nil {
		return 0, errors.New("database not initialized")
	}

	return db.schemaVersion, nil
}

func (db *pgDb) Ping(ctx context.Context) error {
	if db.db == nil {
		return errors.New("database not initialized")
	}

	return db.db.PingContext(ctx)
}

// Close terminates the connection to the database
func (db *pgDb) Close() error {
	if db.db == nil {
		return nil
	}
	for queryName, stmt := range db.preparedStatements {
		if err := stmt.Close(); err != nil {
			log.Errorf("failed to close %s, stmt", queryName)
		}
	}

	return db.db.Close()
}

func (db *pgDb) BeginTransaction(ctx context.Context) (database.Transaction, error) {
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &pgTx{
		tx:   tx,
		pgDb: db,
	}, nil
}

func (db *pgDb) getPreparedStmt(tx *sql.Tx, queryName string) *sql.Stmt {
	if tx == nil {
		return db.preparedStatements[queryName]
	}

	return tx.Stmt(db.preparedStatements[queryName])
}

func (db *pgDb) RegisterFile(ctx context.Context, fileID *string, inboxLocation, uploadPath, uploadUser string) (string, error) {
	return db.registerFile(ctx, nil, fileID, inboxLocation, uploadPath, uploadUser)
}

func (db *pgDb) GetUploadedSubmissionFilePathAndLocation(ctx context.Context, submissionUser, fileID string) (string, string, error) {
	return db.getUploadedSubmissionFilePathAndLocation(ctx, nil, submissionUser, fileID)
}

func (db *pgDb) GetFileIDByUserPathAndStatus(ctx context.Context, submissionUser, filePath, status string) (string, error) {
	return db.getFileIDByUserPathAndStatus(ctx, nil, submissionUser, filePath, status)
}

func (db *pgDb) CheckAccessionIDOwnedByUser(ctx context.Context, accessionID, user string) (bool, error) {
	return db.checkAccessionIDOwnedByUser(ctx, nil, accessionID, user)
}

func (db *pgDb) UpdateFileEventLog(ctx context.Context, fileID, event, user, details, message string) error {
	return db.updateFileEventLog(ctx, nil, fileID, event, user, details, message)
}

func (db *pgDb) StoreHeader(ctx context.Context, header []byte, id string) error {
	return db.storeHeader(ctx, nil, header, id)
}

func (db *pgDb) RotateHeaderKey(ctx context.Context, header []byte, keyHash, fileID string) error {
	return db.rotateHeaderKey(ctx, nil, header, keyHash, fileID)
}

func (db *pgDb) SetArchived(ctx context.Context, location string, file *database.FileInfo, fileID string) error {
	return db.setArchived(ctx, nil, location, file, fileID)
}

func (db *pgDb) CancelFile(ctx context.Context, fileID string, message string) error {
	return db.cancelFile(ctx, nil, fileID, message)
}

func (db *pgDb) IsFileInDataset(ctx context.Context, fileID string) (bool, error) {
	return db.isFileInDataset(ctx, nil, fileID)
}

func (db *pgDb) GetFileStatus(ctx context.Context, fileID string) (string, error) {
	return db.getFileStatus(ctx, nil, fileID)
}

func (db *pgDb) GetHeader(ctx context.Context, fileID string) ([]byte, error) {
	return db.getHeader(ctx, nil, fileID)
}

func (db *pgDb) BackupHeader(ctx context.Context, fileID string, header []byte, keyHash string) error {
	return db.backupHeader(ctx, nil, fileID, header, keyHash)
}

func (db *pgDb) SetVerified(ctx context.Context, file *database.FileInfo, fileID string) error {
	return db.setVerified(ctx, nil, file, fileID)
}

func (db *pgDb) GetArchived(ctx context.Context, fileID string) (*database.ArchiveData, error) {
	return db.getArchived(ctx, nil, fileID)
}

func (db *pgDb) CheckAccessionIDExists(ctx context.Context, accessionID, fileID string) (string, error) {
	return db.checkAccessionIDExists(ctx, nil, accessionID, fileID)
}

func (db *pgDb) SetAccessionID(ctx context.Context, accessionID, fileID string) error {
	return db.setAccessionID(ctx, nil, accessionID, fileID)
}

func (db *pgDb) GetAccessionID(ctx context.Context, fileID string) (string, error) {
	return db.getAccessionID(ctx, nil, fileID)
}

func (db *pgDb) MapFileToDataset(ctx context.Context, datasetID, fileID string) error {
	return db.mapFileToDataset(ctx, nil, datasetID, fileID)
}

func (db *pgDb) GetInboxPath(ctx context.Context, accessionID string) (string, error) {
	return db.getInboxPath(ctx, nil, accessionID)
}

func (db *pgDb) UpdateDatasetEvent(ctx context.Context, datasetID, status, message string) error {
	return db.updateDatasetEvent(ctx, nil, datasetID, status, message)
}

func (db *pgDb) GetFileInfo(ctx context.Context, id string) (*database.FileInfo, error) {
	return db.getFileInfo(ctx, nil, id)
}

func (db *pgDb) GetSubmissionLocation(ctx context.Context, fileID string) (string, error) {
	return db.getSubmissionLocation(ctx, nil, fileID)
}

func (db *pgDb) GetHeaderByAccessionID(ctx context.Context, accessionID string) ([]byte, error) {
	return db.getHeaderByAccessionID(ctx, nil, accessionID)
}

func (db *pgDb) GetMappingData(ctx context.Context, accessionID string) (*database.MappingData, error) {
	return db.getMappingData(ctx, nil, accessionID)
}

func (db *pgDb) GetSyncData(ctx context.Context, accessionID string) (*database.SyncData, error) {
	return db.getSyncData(ctx, nil, accessionID)
}

func (db *pgDb) CheckIfDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	return db.checkIfDatasetExists(ctx, nil, datasetID)
}

func (db *pgDb) GetArchivePathAndLocation(ctx context.Context, accessionID string) (string, string, error) {
	return db.getArchivePathAndLocation(ctx, nil, accessionID)
}

func (db *pgDb) GetArchiveLocation(ctx context.Context, fileID string) (string, error) {
	return db.getArchiveLocation(ctx, nil, fileID)
}

func (db *pgDb) SetSubmissionFileSize(ctx context.Context, fileID string, submissionFileSize int64) error {
	return db.setSubmissionFileSize(ctx, nil, fileID, submissionFileSize)
}

func (db *pgDb) GetUserFiles(ctx context.Context, userID, pathPrefix string, allData bool) ([]*database.SubmissionFileInfo, error) {
	return db.getUserFiles(ctx, nil, userID, pathPrefix, allData)
}

func (db *pgDb) ListActiveUsers(ctx context.Context) ([]string, error) {
	return db.listActiveUsers(ctx, nil)
}

func (db *pgDb) GetDatasetStatus(ctx context.Context, datasetID string) (string, error) {
	return db.getDatasetStatus(ctx, nil, datasetID)
}

func (db *pgDb) AddKeyHash(ctx context.Context, keyHash, keyDescription string) error {
	return db.addKeyHash(ctx, nil, keyHash, keyDescription)
}

func (db *pgDb) GetKeyHash(ctx context.Context, fileID string) (string, error) {
	return db.getKeyHash(ctx, nil, fileID)
}

func (db *pgDb) SetKeyHash(ctx context.Context, keyHash, fileID string) error {
	return db.setKeyHash(ctx, nil, keyHash, fileID)
}

func (db *pgDb) ListKeyHashes(ctx context.Context) ([]*database.C4ghKeyHash, error) {
	return db.listKeyHashes(ctx, nil)
}

func (db *pgDb) DeprecateKeyHash(ctx context.Context, keyHash string) error {
	return db.deprecateKeyHash(ctx, nil, keyHash)
}

func (db *pgDb) ListDatasets(ctx context.Context) ([]*database.DatasetInfo, error) {
	return db.listDatasets(ctx, nil)
}

func (db *pgDb) ListUserDatasets(ctx context.Context, submissionUser string) ([]*database.DatasetInfo, error) {
	return db.listUserDatasets(ctx, nil, submissionUser)
}

func (db *pgDb) UpdateUserInfo(ctx context.Context, userID, name, email string, groups []string) error {
	return db.updateUserInfo(ctx, nil, userID, name, email, groups)
}

func (db *pgDb) GetReVerificationData(ctx context.Context, accessionID string) (*database.ReVerificationData, error) {
	return db.getReVerificationData(ctx, nil, accessionID)
}

func (db *pgDb) GetReVerificationDataFromFileID(ctx context.Context, fileID string) (*database.ReVerificationData, error) {
	return db.getReVerificationDataFromFileID(ctx, nil, fileID)
}

func (db *pgDb) GetDecryptedChecksum(ctx context.Context, fileID string) (string, error) {
	return db.getDecryptedChecksum(ctx, nil, fileID)
}

func (db *pgDb) GetDatasetFiles(ctx context.Context, datasetID string) ([]string, error) {
	return db.getDatasetFiles(ctx, nil, datasetID)
}

func (db *pgDb) GetDatasetFileIDs(ctx context.Context, datasetID string) ([]string, error) {
	return db.getDatasetFileIDs(ctx, nil, datasetID)
}

func (db *pgDb) GetFileDetails(ctx context.Context, fileID, event string) (*database.FileDetails, error) {
	return db.getFileDetails(ctx, nil, fileID, event)
}

func (db *pgDb) GetSizeAndObjectCountOfLocation(ctx context.Context, location string) (uint64, uint64, error) {
	return db.getSizeAndObjectCountOfLocation(ctx, nil, location)
}

func (db *pgDb) SetBackedUp(ctx context.Context, location, path, fileID string) error {
	return db.setBackedUp(ctx, nil, location, path, fileID)
}

func (db *pgDb) GetFileIDInInbox(ctx context.Context, submissionUser, filePath string) (string, error) {
	return db.getFileIDInInbox(ctx, nil, submissionUser, filePath)
}
