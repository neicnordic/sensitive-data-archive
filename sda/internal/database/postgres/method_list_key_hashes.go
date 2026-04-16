package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const listKeyHashesQuery = "listKeyHashes"

func init() {
	queries[listKeyHashesQuery] = `
SELECT key_hash, description, created_at, deprecated_at 
FROM sda.encryption_keys 
ORDER BY created_at ASC;
`
}

func (db *pgDb) listKeyHashes(ctx context.Context, tx *sql.Tx) ([]*database.C4ghKeyHash, error) {
	stmt := db.getPreparedStmt(tx, listKeyHashesQuery)

	var hashList []*database.C4ghKeyHash
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		h := new(database.C4ghKeyHash)
		depr := sql.NullString{}
		err := rows.Scan(&h.Hash, &h.Description, &h.CreatedAt, &depr)
		if err != nil {
			return nil, err
		}
		h.DeprecatedAt = depr.String

		hashList = append(hashList, h)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return hashList, nil
}
