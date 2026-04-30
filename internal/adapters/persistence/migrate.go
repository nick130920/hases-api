package persistence

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

func EnsureAdmin(ctx context.Context, pool *pgxpool.Pool, email, passwordHash string) error {
	var n int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ($1, $2, 'Administrador', 'admin')`,
		email, passwordHash)
	return err
}

// Pool opens pgx pool (exported helper).
func OpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, dsn)
}
