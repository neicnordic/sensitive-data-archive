package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getReVerificationDataQuery = "getReVerificationData"

func init() {
	queries[getReVerificationDataQuery] = `
SELECT f.id, f.archive_file_path, f.submission_file_path, f.submission_user, cs.type, cs.checksum 
FROM sda.files AS f 
INNER JOIN sda.checksums AS cs ON f.id = cs.file_id
WHERE f.stable_id = $1 AND cs.source = 'ARCHIVED';
`
}
func (db *pgDb) getReVerificationData(ctx context.Context, tx *sql.Tx, accessionID string) (*database.ReVerificationData, error) {
	stmt := db.getPreparedStmt(tx, getReVerificationDataQuery)

	reVerificationData := new(database.ReVerificationData)

	if err := stmt.QueryRowContext(ctx, accessionID).Scan(
		&reVerificationData.FileID, &reVerificationData.ArchiveFilePath, &reVerificationData.SubmissionFilePath,
		&reVerificationData.SubmissionUser, &reVerificationData.ArchivedCheckSumType, &reVerificationData.ArchivedCheckSum); err != nil {
		return nil, err
	}

	reVerificationData.ArchivedCheckSumType = strings.ToLower(reVerificationData.ArchivedCheckSumType)

	return reVerificationData, nil
}
