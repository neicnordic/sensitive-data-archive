package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getReVerificationDataFromFileIDQuery = "getReVerificationDataFromFileID"

func init() {
	queries[getReVerificationDataFromFileIDQuery] = `
SELECT f.id, f.archive_file_path, f.submission_file_path, f.submission_user, cs.type, cs.checksum 
FROM sda.files AS f 
INNER JOIN sda.checksums AS cs ON f.id = cs.file_id
WHERE f.id = $1 AND cs.source = 'ARCHIVED';
`
}

func (db *pgDb) getReVerificationDataFromFileID(ctx context.Context, tx *sql.Tx, fileID string) (*database.ReVerificationData, error) {
	stmt := db.getPreparedStmt(tx, getReVerificationDataFromFileIDQuery)

	reVerificationData := new(database.ReVerificationData)

	if err := stmt.QueryRowContext(ctx, fileID).Scan(
		&reVerificationData.FileID, &reVerificationData.ArchiveFilePath, &reVerificationData.SubmissionFilePath,
		&reVerificationData.SubmissionUser, &reVerificationData.ArchivedCheckSumType, &reVerificationData.ArchivedCheckSum); err != nil {
		return nil, err
	}

	reVerificationData.ArchivedCheckSumType = strings.ToLower(reVerificationData.ArchivedCheckSumType)

	return reVerificationData, nil
}
