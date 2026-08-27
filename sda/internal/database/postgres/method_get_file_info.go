package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getFileInfoQuery = "getFileInfo"
const getFileInfoChecksumQuery = "getFileInfoChecksum"

func init() {
	queries[getFileInfoQuery] = `SELECT archive_file_path, archive_file_size 
FROM sda.files 
WHERE id = $1;`

	queries[getFileInfoChecksumQuery] = `SELECT MAX(checksum) FILTER(WHERE source = 'ARCHIVED') AS Archived,
MAX(checksum) FILTER(WHERE source = 'UNENCRYPTED') AS Unencrypted,
MAX(checksum) FILTER(WHERE source = 'UPLOADED') AS Uploaded 
FROM sda.checksums 
WHERE file_id = $1;`
}

func (db *pgDb) getFileInfo(ctx context.Context, tx *sql.Tx, id string) (*database.FileInfo, error) {
	getFileIDStmt, err := db.getPreparedStmt(tx, getFileInfoQuery)
	if err != nil {
		return nil, err
	}
	getChecksumStmt, err := db.getPreparedStmt(tx, getFileInfoChecksumQuery)
	if err != nil {
		return nil, err
	}

	info := new(database.FileInfo)
	if err := getFileIDStmt.QueryRowContext(ctx, id).Scan(&info.Path, &info.Size); err != nil {
		return nil, parsePQError(err)
	}

	var archivedChecksum, decryptedChecksum, uploadedChecksum sql.NullString
	if err := getChecksumStmt.QueryRowContext(ctx, id).Scan(&archivedChecksum, &decryptedChecksum, &uploadedChecksum); err != nil {
		return nil, parsePQError(err)
	}
	info.ArchivedChecksum = archivedChecksum.String
	info.DecryptedChecksum = decryptedChecksum.String
	info.UploadedChecksum = uploadedChecksum.String

	return info, nil
}
