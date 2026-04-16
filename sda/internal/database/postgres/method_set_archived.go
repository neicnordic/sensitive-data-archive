package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const (
	setArchivedQuery            = "setArchived"
	setArchivedAddCheckSumQuery = "setArchivedAddCheckSum"
)

func init() {
	queries[setArchivedQuery] = `
UPDATE sda.files 
SET archive_location = $1, archive_file_path = $2, archive_file_size = $3 
WHERE id = $4;
`
	queries[setArchivedAddCheckSumQuery] = `
INSERT INTO sda.checksums(file_id, checksum, type, source)
VALUES($1, $2, upper($3)::sda.checksum_algorithm, upper('UPLOADED')::sda.checksum_source)
ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = EXCLUDED.checksum;
`
}

func (db *pgDb) setArchived(ctx context.Context, tx *sql.Tx, location string, file *database.FileInfo, fileID string) error {
	stmt := db.getPreparedStmt(tx, setArchivedQuery)
	addCheckSumStmt := db.getPreparedStmt(tx, setArchivedAddCheckSumQuery)

	if _, err := stmt.ExecContext(ctx, location, file.Path, file.Size, fileID); err != nil {
		return fmt.Errorf("setArchived error: %s", err.Error())
	}

	if _, err := addCheckSumStmt.ExecContext(ctx, fileID, file.UploadedChecksum, "SHA256"); err != nil {
		return fmt.Errorf("addChecksum error: %s", err.Error())
	}

	return nil
}
