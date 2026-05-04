package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

type pgTx struct {
	tx *sql.Tx
	*pgDb
}

func (tx *pgTx) Commit() error {
	return tx.tx.Commit()
}

func (tx *pgTx) Rollback() error {
	err := tx.tx.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}

	return err
}

func (tx *pgTx) RegisterFile(ctx context.Context, fileID *string, inboxLocation, uploadPath, uploadUser string) (string, error) {
	return tx.registerFile(ctx, tx.tx, fileID, inboxLocation, uploadPath, uploadUser)
}

func (tx *pgTx) GetUploadedSubmissionFilePathAndLocation(ctx context.Context, submissionUser, fileID string) (string, string, error) {
	return tx.getUploadedSubmissionFilePathAndLocation(ctx, tx.tx, submissionUser, fileID)
}

func (tx *pgTx) GetFileIDByUserPathAndStatus(ctx context.Context, submissionUser, filePath, status string) (string, error) {
	return tx.getFileIDByUserPathAndStatus(ctx, tx.tx, submissionUser, filePath, status)
}

func (tx *pgTx) CheckAccessionIDOwnedByUser(ctx context.Context, accessionID, user string) (bool, error) {
	return tx.checkAccessionIDOwnedByUser(ctx, tx.tx, accessionID, user)
}

func (tx *pgTx) UpdateFileEventLog(ctx context.Context, fileID, event, user, details, message string) error {
	return tx.updateFileEventLog(ctx, tx.tx, fileID, event, user, details, message)
}

func (tx *pgTx) StoreHeader(ctx context.Context, header []byte, id string) error {
	return tx.storeHeader(ctx, tx.tx, header, id)
}

func (tx *pgTx) RotateHeaderKey(ctx context.Context, header []byte, keyHash, fileID string) error {
	return tx.rotateHeaderKey(ctx, tx.tx, header, keyHash, fileID)
}

func (tx *pgTx) SetArchived(ctx context.Context, location string, file *database.FileInfo, fileID string) error {
	return tx.setArchived(ctx, tx.tx, location, file, fileID)
}

func (tx *pgTx) CancelFile(ctx context.Context, fileID string, message string) error {
	return tx.cancelFile(ctx, tx.tx, fileID, message)
}

func (tx *pgTx) IsFileInDataset(ctx context.Context, fileID string) (bool, error) {
	return tx.isFileInDataset(ctx, tx.tx, fileID)
}

func (tx *pgTx) GetFileStatus(ctx context.Context, fileID string) (string, error) {
	return tx.getFileStatus(ctx, tx.tx, fileID)
}

func (tx *pgTx) GetHeader(ctx context.Context, fileID string) ([]byte, error) {
	return tx.getHeader(ctx, tx.tx, fileID)
}

func (tx *pgTx) BackupHeader(ctx context.Context, fileID string, header []byte, keyHash string) error {
	return tx.backupHeader(ctx, tx.tx, fileID, header, keyHash)
}

func (tx *pgTx) SetVerified(ctx context.Context, file *database.FileInfo, fileID string) error {
	return tx.setVerified(ctx, tx.tx, file, fileID)
}

func (tx *pgTx) GetArchived(ctx context.Context, fileID string) (*database.ArchiveData, error) {
	return tx.getArchived(ctx, tx.tx, fileID)
}

func (tx *pgTx) CheckAccessionIDExists(ctx context.Context, accessionID, fileID string) (string, error) {
	return tx.checkAccessionIDExists(ctx, tx.tx, accessionID, fileID)
}

func (tx *pgTx) SetAccessionID(ctx context.Context, accessionID, fileID string) error {
	return tx.setAccessionID(ctx, tx.tx, accessionID, fileID)
}

func (tx *pgTx) GetAccessionID(ctx context.Context, fileID string) (string, error) {
	return tx.getAccessionID(ctx, tx.tx, fileID)
}

func (tx *pgTx) MapFileToDataset(ctx context.Context, datasetID, fileID string) error {
	return tx.mapFileToDataset(ctx, tx.tx, datasetID, fileID)
}

func (tx *pgTx) GetInboxPath(ctx context.Context, accessionID string) (string, error) {
	return tx.getInboxPath(ctx, tx.tx, accessionID)
}

func (tx *pgTx) UpdateDatasetEvent(ctx context.Context, datasetID, status, message string) error {
	return tx.updateDatasetEvent(ctx, tx.tx, datasetID, status, message)
}

func (tx *pgTx) GetFileInfo(ctx context.Context, id string) (*database.FileInfo, error) {
	return tx.getFileInfo(ctx, tx.tx, id)
}

func (tx *pgTx) GetSubmissionLocation(ctx context.Context, fileID string) (string, error) {
	return tx.getSubmissionLocation(ctx, tx.tx, fileID)
}

func (tx *pgTx) GetHeaderByAccessionID(ctx context.Context, accessionID string) ([]byte, error) {
	return tx.getHeaderByAccessionID(ctx, tx.tx, accessionID)
}

func (tx *pgTx) GetMappingData(ctx context.Context, accessionID string) (*database.MappingData, error) {
	return tx.getMappingData(ctx, tx.tx, accessionID)
}

func (tx *pgTx) GetSyncData(ctx context.Context, accessionID string) (*database.SyncData, error) {
	return tx.getSyncData(ctx, tx.tx, accessionID)
}

func (tx *pgTx) CheckIfDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	return tx.checkIfDatasetExists(ctx, tx.tx, datasetID)
}

func (tx *pgTx) GetArchivePathAndLocation(ctx context.Context, accessionID string) (string, string, error) {
	return tx.getArchivePathAndLocation(ctx, tx.tx, accessionID)
}

func (tx *pgTx) GetArchiveLocation(ctx context.Context, fileID string) (string, error) {
	return tx.getArchiveLocation(ctx, tx.tx, fileID)
}

func (tx *pgTx) SetSubmissionFileSize(ctx context.Context, fileID string, submissionFileSize int64) error {
	return tx.setSubmissionFileSize(ctx, tx.tx, fileID, submissionFileSize)
}

func (tx *pgTx) GetUserFiles(ctx context.Context, userID, pathPrefix string, allData bool) ([]*database.SubmissionFileInfo, error) {
	return tx.getUserFiles(ctx, tx.tx, userID, pathPrefix, allData)
}

func (tx *pgTx) ListActiveUsers(ctx context.Context) ([]string, error) {
	return tx.listActiveUsers(ctx, tx.tx)
}

func (tx *pgTx) GetDatasetStatus(ctx context.Context, datasetID string) (string, error) {
	return tx.getDatasetStatus(ctx, tx.tx, datasetID)
}

func (tx *pgTx) AddKeyHash(ctx context.Context, keyHash, keyDescription string) error {
	return tx.addKeyHash(ctx, tx.tx, keyHash, keyDescription)
}

func (tx *pgTx) GetKeyHash(ctx context.Context, fileID string) (string, error) {
	return tx.getKeyHash(ctx, tx.tx, fileID)
}

func (tx *pgTx) SetKeyHash(ctx context.Context, keyHash, fileID string) error {
	return tx.setKeyHash(ctx, tx.tx, keyHash, fileID)
}

func (tx *pgTx) ListKeyHashes(ctx context.Context) ([]*database.C4ghKeyHash, error) {
	return tx.listKeyHashes(ctx, tx.tx)
}

func (tx *pgTx) DeprecateKeyHash(ctx context.Context, keyHash string) error {
	return tx.deprecateKeyHash(ctx, tx.tx, keyHash)
}

func (tx *pgTx) ListDatasets(ctx context.Context) ([]*database.DatasetInfo, error) {
	return tx.listDatasets(ctx, tx.tx)
}

func (tx *pgTx) ListUserDatasets(ctx context.Context, submissionUser string) ([]*database.DatasetInfo, error) {
	return tx.listUserDatasets(ctx, tx.tx, submissionUser)
}

func (tx *pgTx) UpdateUserInfo(ctx context.Context, userID, name, email string, groups []string) error {
	return tx.updateUserInfo(ctx, tx.tx, userID, name, email, groups)
}

func (tx *pgTx) GetReVerificationData(ctx context.Context, accessionID string) (*database.ReVerificationData, error) {
	return tx.getReVerificationData(ctx, tx.tx, accessionID)
}

func (tx *pgTx) GetReVerificationDataFromFileID(ctx context.Context, fileID string) (*database.ReVerificationData, error) {
	return tx.getReVerificationDataFromFileID(ctx, tx.tx, fileID)
}

func (tx *pgTx) GetDecryptedChecksum(ctx context.Context, fileID string) (string, error) {
	return tx.getDecryptedChecksum(ctx, tx.tx, fileID)
}

func (tx *pgTx) GetDatasetFiles(ctx context.Context, datasetID string) ([]string, error) {
	return tx.getDatasetFiles(ctx, tx.tx, datasetID)
}

func (tx *pgTx) GetDatasetFileIDs(ctx context.Context, datasetID string) ([]string, error) {
	return tx.getDatasetFileIDs(ctx, tx.tx, datasetID)
}

func (tx *pgTx) GetFileDetails(ctx context.Context, fileID, event string) (*database.FileDetails, error) {
	return tx.getFileDetails(ctx, tx.tx, fileID, event)
}

func (tx *pgTx) GetSizeAndObjectCountOfLocation(ctx context.Context, location string) (uint64, uint64, error) {
	return tx.getSizeAndObjectCountOfLocation(ctx, tx.tx, location)
}

func (tx *pgTx) SetBackedUp(ctx context.Context, location, path, fileID string) error {
	return tx.setBackedUp(ctx, tx.tx, location, path, fileID)
}

func (tx *pgTx) GetFileIDInInbox(ctx context.Context, submissionUser, filePath string) (string, error) {
	return tx.getFileIDInInbox(ctx, tx.tx, submissionUser, filePath)
}
