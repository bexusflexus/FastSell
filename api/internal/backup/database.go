package backup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	migratefile "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

var internalDatabaseNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type Database interface {
	Info(context.Context) (DatabaseInfo, error)
	CreateDatabase(context.Context, string) error
	DropDatabase(context.Context, string) error
	MigrateDatabase(context.Context, string) error
	VerifyDatabase(context.Context, string, int64) error
	SwapDatabases(context.Context, string, string, string) error
	RollbackSwap(context.Context, string, string, string) error
	Reset()
}

type PostgresDatabase struct {
	pool          *pgxpool.Pool
	connection    *pgx.ConnConfig
	migrationRoot string
}

func NewPostgresDatabase(pool *pgxpool.Pool, migrationRoot string) *PostgresDatabase {
	return &PostgresDatabase{
		pool:          pool,
		connection:    pool.Config().ConnConfig.Copy(),
		migrationRoot: migrationRoot,
	}
}

func (d *PostgresDatabase) Info(ctx context.Context) (DatabaseInfo, error) {
	return databaseInfo(ctx, d.pool)
}

func (d *PostgresDatabase) Reset() { d.pool.Reset() }

func (d *PostgresDatabase) CreateDatabase(ctx context.Context, name string) error {
	if !internalDatabaseNamePattern.MatchString(name) {
		return errors.New("invalid internal database name")
	}
	conn, err := d.adminConnection(ctx)
	if err != nil {
		return errors.New("failed to open PostgreSQL administration connection")
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH TEMPLATE template0"); err != nil {
		return errors.New("failed to create restore staging database")
	}
	return nil
}

func (d *PostgresDatabase) DropDatabase(ctx context.Context, name string) error {
	if !internalDatabaseNamePattern.MatchString(name) {
		return errors.New("invalid internal database name")
	}
	conn, err := d.adminConnection(ctx)
	if err != nil {
		return errors.New("failed to open PostgreSQL administration connection")
	}
	defer conn.Close(ctx)
	if err := terminateDatabaseConnections(ctx, conn, name); err != nil {
		return errors.New("failed to quiesce temporary database")
	}
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil {
		return errors.New("failed to remove temporary database")
	}
	return nil
}

// MigrateDatabase uses the same golang-migrate engine, PostgreSQL driver, and
// migration files as the normal deployment migrate container.
func (d *PostgresDatabase) MigrateDatabase(_ context.Context, databaseName string) error {
	if !internalDatabaseNamePattern.MatchString(databaseName) {
		return errors.New("invalid internal database name")
	}
	source, err := (&migratefile.File{}).Open("file://" + filepath.Clean(d.migrationRoot))
	if err != nil {
		return errors.New("installed database migrations are unavailable")
	}
	config := d.connection.Copy()
	config.Database = databaseName
	sqlDB := stdlib.OpenDB(*config)
	driver, err := migratepostgres.WithInstance(sqlDB, &migratepostgres.Config{})
	if err != nil {
		_ = source.Close()
		_ = sqlDB.Close()
		return errors.New("failed to initialize database migration driver")
	}
	migrator, err := migrate.NewWithInstance("file", source, databaseName, driver)
	if err != nil {
		_ = source.Close()
		_ = driver.Close()
		return errors.New("failed to initialize database migrations")
	}
	defer migrator.Close()
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errors.New("database migration failed")
	}
	return nil
}

func (d *PostgresDatabase) VerifyDatabase(ctx context.Context, databaseName string, target int64) error {
	pool, err := d.databasePool(ctx, databaseName)
	if err != nil {
		return errors.New("database health validation failed")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("database health validation failed")
	}
	info, err := databaseInfo(ctx, pool)
	if err != nil {
		return errors.New("database migration validation failed")
	}
	if info.SchemaVersion != target {
		return fmt.Errorf("database schema is %d; expected %d", info.SchemaVersion, target)
	}
	var ok bool
	err = pool.QueryRow(ctx, `
		SELECT to_regclass('public.inventory_groups') IS NOT NULL
			AND to_regclass('public.items') IS NOT NULL
			AND to_regclass('public.backup_settings') IS NOT NULL
	`).Scan(&ok)
	if err != nil || !ok {
		return errors.New("required FastSell tables are missing after restore")
	}
	return nil
}

func (d *PostgresDatabase) SwapDatabases(ctx context.Context, current, staging, old string) error {
	return d.renamePair(ctx, current, old, staging, current)
}

func (d *PostgresDatabase) RollbackSwap(ctx context.Context, current, old, failed string) error {
	return d.renamePair(ctx, current, failed, old, current)
}

func (d *PostgresDatabase) renamePair(ctx context.Context, firstFrom, firstTo, secondFrom, secondTo string) error {
	for _, name := range []string{firstFrom, firstTo, secondFrom, secondTo} {
		if !internalDatabaseNamePattern.MatchString(name) {
			return errors.New("invalid internal database name")
		}
	}
	d.pool.Reset()
	conn, err := d.adminConnection(ctx)
	if err != nil {
		return errors.New("failed to open PostgreSQL administration connection")
	}
	defer conn.Close(ctx)
	if err := terminateDatabaseConnections(ctx, conn, firstFrom, secondFrom); err != nil {
		return errors.New("failed to quiesce databases for restore swap")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return errors.New("failed to start database swap")
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{firstFrom}.Sanitize()+" RENAME TO "+pgx.Identifier{firstTo}.Sanitize()); err != nil {
		return errors.New("failed to retain current database for rollback")
	}
	if _, err := tx.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{secondFrom}.Sanitize()+" RENAME TO "+pgx.Identifier{secondTo}.Sanitize()); err != nil {
		return errors.New("failed to activate restored database")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("failed to commit database swap")
	}
	d.pool.Reset()
	return nil
}

func (d *PostgresDatabase) adminConnection(ctx context.Context) (*pgx.Conn, error) {
	config := d.connection.Copy()
	config.Database = "postgres"
	return pgx.ConnectConfig(ctx, config)
}

func (d *PostgresDatabase) databasePool(ctx context.Context, name string) (*pgxpool.Pool, error) {
	if !internalDatabaseNamePattern.MatchString(name) {
		return nil, errors.New("invalid internal database name")
	}
	config := d.pool.Config().Copy()
	config.ConnConfig.Database = name
	config.MinConns = 0
	return pgxpool.NewWithConfig(ctx, config)
}

func databaseInfo(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (DatabaseInfo, error) {
	var info DatabaseInfo
	err := query.QueryRow(ctx, `
		SELECT current_database(), current_setting('server_version_num')::int / 10000,
			version, dirty FROM schema_migrations LIMIT 1
	`).Scan(&info.Name, &info.PostgreSQLMajor, &info.SchemaVersion, &info.MigrationDirty)
	if err != nil {
		return DatabaseInfo{}, err
	}
	if info.MigrationDirty {
		return DatabaseInfo{}, errors.New("database migration state is dirty")
	}
	return info, nil
}

func terminateDatabaseConnections(ctx context.Context, conn *pgx.Conn, names ...string) error {
	for _, name := range names {
		if _, err := conn.Exec(ctx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, name); err != nil {
			return err
		}
	}
	return nil
}
