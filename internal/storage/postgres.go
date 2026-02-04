package storage

import (
	"context"
	"fmt"

	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	pool *pgxpool.Pool
	config config.Database
}

func NewDB(ctx context.Context, cfg config.Database) (*DB, error) {
	db := &DB{config: cfg}
	if err := db.connect(ctx); err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) connect(ctx context.Context) error {
	connection := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", db.config.User, db.config.Password, db.config.Host, db.config.Port, db.config.Name)

	pool, err := pgxpool.New(ctx, connection)

	if err != nil {
		return err
	}

	err = pool.Ping(ctx)

	if err != nil {
		pool.Close()
		return err
	}

	db.pool = pool

	return nil
}

func (db *DB) Close() error {
	if db.pool == nil {
		return nil
	}

	db.pool.Close()

	return nil
}
