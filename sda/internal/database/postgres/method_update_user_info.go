package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

const updateUserInfoQuery = "updateUserInfo"

func init() {
	queries[updateUserInfoQuery] = `
INSERT INTO sda.userinfo(id, name, email, groups) VALUES($1, $2, $3, $4)
ON CONFLICT (id)
DO UPDATE SET name = excluded.name, email = excluded.email, groups = excluded.groups;

`
}
func (db *pgDb) updateUserInfo(ctx context.Context, tx *sql.Tx, userID, name, email string, groups []string) error {
	stmt := db.getPreparedStmt(tx, updateUserInfoQuery)

	result, err := stmt.ExecContext(ctx, userID, name, email, pq.Array(groups))
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}
