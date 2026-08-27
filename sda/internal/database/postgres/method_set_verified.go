package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const (
	setVerifiedQuery                       = "setVerified"
	setVerifiedAddArchiveChecksumQuery     = "setVerifiedAddArchiveChecksum"
	setVerifiedAddUnencryptedChecksumQuery = "setVerifiedAddUnencryptedChecksum"
)

func init() {
	queries[setVerifiedQuery] = `UPDATE sda.files SET decrypted_file_size = $1 WHERE id = $2;`

	queries[setVerifiedAddArchiveChecksumQuery] = `INSERT INTO sda.checksums(file_id, checksum, type, source)
VALUES($1, $2, upper($3)::sda.checksum_algorithm, upper('ARCHIVED')::sda.checksum_source)
ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = EXCLUDED.checksum;`

	queries[setVerifiedAddUnencryptedChecksumQuery] = `INSERT INTO sda.checksums(file_id, checksum, type, source)
VALUES($1, $2, upper($3)::sda.checksum_algorithm, upper('UNENCRYPTED')::sda.checksum_source)
ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = EXCLUDED.checksum;`
}

func (db *pgDb) setVerified(ctx context.Context, tx *sql.Tx, file *database.FileInfo, fileID string) error {
	stmt, err := db.getPreparedStmt(tx, setVerifiedQuery)
	if err != nil {
		return err
	}
	addArchiveChecksumStmt, err := db.getPreparedStmt(tx, setVerifiedAddArchiveChecksumQuery)
	if err != nil {
		return err
	}
	addUnencryptedChecksumStmt, err := db.getPreparedStmt(tx, setVerifiedAddUnencryptedChecksumQuery)
	if err != nil {
		return err
	}

	if _, err := stmt.ExecContext(ctx, file.DecryptedSize, fileID); err != nil {
		return parsePQError(err)
	}

	if _, err := addArchiveChecksumStmt.ExecContext(ctx, fileID, file.ArchivedChecksum, "SHA256"); err != nil {
		return parsePQError(err)
	}

	if _, err := addUnencryptedChecksumStmt.ExecContext(ctx, fileID, file.DecryptedChecksum, "SHA256"); err != nil {
		return parsePQError(err)
	}

	return nil
}
