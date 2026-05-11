package benchmark

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

type adapter struct {
	db    *sql.DB
	mode  string
	stmts map[string]*sql.Stmt
}

func newAdapter(tb testing.TB, db *sql.DB, mode string) *adapter {
	tb.Helper()
	a := &adapter{db: db, mode: mode, stmts: map[string]*sql.Stmt{}}
	if mode == "prep" {
		ctx := context.Background()
		for name, q := range queries {
			stmt, err := db.PrepareContext(ctx, q)
			if err != nil {
				tb.Fatalf("prepare %q: %v", name, err)
			}
			a.stmts[name] = stmt
		}
	}
	return a
}

func (a *adapter) Close() error {
	var firstErr error
	for _, s := range a.stmts {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *adapter) exec(ctx context.Context, name string, args ...any) error {
	switch a.mode {
	case "prep":
		stmt, ok := a.stmts[name]
		if !ok {
			return fmt.Errorf("adapter: unknown query %q in prep mode", name)
		}
		_, err := stmt.ExecContext(ctx, args...)
		return err
	case "noprep":
		q, ok := queries[name]
		if !ok {
			return fmt.Errorf("adapter: unknown query %q in noprep mode", name)
		}
		_, err := a.db.ExecContext(ctx, q, args...)
		return err
	default:
		return fmt.Errorf("adapter: unknown mode %q", a.mode)
	}
}

func (a *adapter) queryRow(ctx context.Context, name string, args []any, dest ...any) error {
	switch a.mode {
	case "prep":
		stmt, ok := a.stmts[name]
		if !ok {
			return fmt.Errorf("adapter: unknown query %q in prep mode", name)
		}
		return stmt.QueryRowContext(ctx, args...).Scan(dest...)
	case "noprep":
		q, ok := queries[name]
		if !ok {
			return fmt.Errorf("adapter: unknown query %q in noprep mode", name)
		}
		return a.db.QueryRowContext(ctx, q, args...).Scan(dest...)
	default:
		return fmt.Errorf("adapter: unknown mode %q", a.mode)
	}
}
