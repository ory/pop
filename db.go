package pop

import (
	"context"
	"database/sql"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jmoiron/sqlx"
)

type dB struct {
	*sqlx.DB
	p *pgxpool.Pool
}

func (db *dB) SQLDB() *sql.DB {
	return db.DB.DB
}

func (db *dB) PGXPool() *pgxpool.Pool {
	return db.p
}

func (db *dB) TransactionContext(ctx context.Context) (*Tx, error) {
	return newTX(ctx, db, db.p, nil)
}

func (db *dB) Transaction() (*Tx, error) {
	return newTX(context.Background(), db, db.p, nil)
}

func (db *dB) TransactionContextOptions(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	return newTX(ctx, db, db.p, opts)
}

func (db *dB) Rollback() error {
	return nil
}

func (db *dB) Commit() error {
	return nil
}
