package db

import (
	"context"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver for goose
	"github.com/pressly/goose/v3"

	"hotelpms/api/migrations"
)

// Migrate applies every pending migration. url must connect as the role that owns the tables.
func Migrate(ctx context.Context, url string) error {
	sqlDB, err := goose.OpenDBWithDriver("pgx", url)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, sqlDB, ".")
}
