package postgres

import (
	"context"
	"database/sql"
)

const listActiveUsersQuery = "listActiveUsers"

func init() {
	queries[listActiveUsersQuery] = `
SELECT DISTINCT submission_user 
FROM sda.files f 
WHERE NOT EXISTS (
	SELECT 1 
	FROM sda.file_dataset d 
	WHERE f.id = d.file_id
) ORDER BY submission_user ASC;
`
}

func (db *pgDb) listActiveUsers(ctx context.Context, tx *sql.Tx) ([]string, error) {
	stmt := db.getPreparedStmt(tx, listActiveUsersQuery)

	var users []string
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var user string
		err := rows.Scan(&user)
		if err != nil {
			return nil, err
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}
