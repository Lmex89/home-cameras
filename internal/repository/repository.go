// Package repository implements the persistence layer over sqlx. All
// repositories accept a DBTX (either *sqlx.DB or *sqlx.Tx) so services
// can run multi-statement operations inside explicit transactions.
package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// ErrNotFound is returned when a single-row lookup misses.
var ErrNotFound = errors.New("record not found")

// DBTX is the minimal query interface satisfied by both *sqlx.DB and
// *sqlx.Tx, enabling transparent transaction injection. It matches the
// union of sqlx's ExecerContext and QueryerContext so repositories can
// use the sqlx helper functions (GetContext/SelectContext) against
// either connection type.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryxContext(ctx context.Context, query string, args ...any) (*sqlx.Rows, error)
	QueryRowxContext(ctx context.Context, query string, args ...any) *sqlx.Row
	Rebind(query string) string
}
