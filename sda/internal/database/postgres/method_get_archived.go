package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getArchivedQuery = "getArchived"

func init() {
	queries[getArchivedQuery] = `
SELECT archive_file_path, archive_file_size, archive_location, backup_path, backup_location 
FROM sda.files 
WHERE id = $1;
`
}

func (db *pgDb) getArchived(ctx context.Context, tx *sql.Tx, fileID string) (*database.ArchiveData, error) {
	stmt := db.getPreparedStmt(tx, getArchivedQuery)

	var archiveFilePath string
	var archiveLocation, backupFilePath, backupLocation sql.NullString
	var archiveFileSize sql.NullInt64
	if err := stmt.QueryRowContext(ctx, fileID).Scan(&archiveFilePath, &archiveFileSize, &archiveLocation, &backupFilePath, &backupLocation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}
	if archiveFilePath != "" || archiveFileSize.Valid || archiveLocation.Valid {
		ad := &database.ArchiveData{
			FilePath:       archiveFilePath,
			Location:       archiveLocation.String,
			FileSize:       archiveFileSize.Int64,
			BackupFilePath: backupFilePath.String,
			BackupLocation: backupLocation.String,
		}
		if backupFilePath.Valid {
			ad.BackupFilePath = backupFilePath.String
		}
		if backupLocation.Valid {
			ad.BackupLocation = backupLocation.String
		}

		return ad, nil
	}

	// We have a files table entry but archive data has not been set
	return nil, nil
}
