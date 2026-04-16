package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getFileInfoQuery = "getFileInfo"
const getFileInfoChecksumQuery = "getFileInfoChecksum"

func init() {
	queries[getFileInfoQuery] = `
SELECT archive_file_path, archive_file_size from sda.files where id = $1;
`
	queries[getFileInfoChecksumQuery] = `
SELECT MAX(checksum) FILTER(where source = 'ARCHIVED') as Archived,
MAX(checksum) FILTER(where source = 'UNENCRYPTED') as Unencrypted,
MAX(checksum) FILTER(where source = 'UPLOADED') as Uploaded from sda.checksums where file_id = $1;
`
}

func (db *pgDb) getFileInfo(ctx context.Context, tx *sql.Tx, id string) (*database.FileInfo, error) {
	getFileIDStmt := db.getPreparedStmt(tx, getFileInfoQuery)
	getChecksumStmt := db.getPreparedStmt(tx, getFileInfoChecksumQuery)

	info := new(database.FileInfo)
	if err := getFileIDStmt.QueryRowContext(ctx, id).Scan(&info.Path, &info.Size); err != nil {
		return nil, err
	}

	var archivedChecksum, decryptedChecksum, uploadedChecksum sql.NullString
	if err := getChecksumStmt.QueryRowContext(ctx, id).Scan(&archivedChecksum, &decryptedChecksum, &uploadedChecksum); err != nil {
		return nil, err
	}
	info.ArchivedChecksum = archivedChecksum.String
	info.DecryptedChecksum = decryptedChecksum.String
	info.UploadedChecksum = uploadedChecksum.String

	return info, nil
}
