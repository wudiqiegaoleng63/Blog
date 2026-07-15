// Package migrations provides SQL migration execution through golang-migrate.
//
// Production code must not use GORM AutoMigrate for schema evolution. All
// schema changes are managed explicitly by migrations/*.up.sql and *.down.sql.
package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	mysqlmigrate "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	migrationfiles "github.com/lsy/blog/migrations"
)

var (
	openSQL           = sql.Open
	withMySQLInstance = func(db *sql.DB) (migratedatabase.Driver, error) {
		return mysqlmigrate.WithInstance(db, &mysqlmigrate.Config{})
	}
)

// RunUp applies all pending up migrations.
func RunUp(dsn string) (err error) {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeMigrator(m)) }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}

// RunDown rolls back the latest migration. It is intended for development use.
func RunDown(dsn string) (err error) {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeMigrator(m)) }()

	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: down: %w", err)
	}
	return nil
}

// Version returns the current migration version and whether it is dirty.
// A database without applied migrations is reported as version zero, clean.
func Version(dsn string) (version uint, dirty bool, err error) {
	m, err := newMigrator(dsn)
	if err != nil {
		return 0, false, err
	}
	defer func() { err = errors.Join(err, closeMigrator(m)) }()
	return normalizeVersion(m.Version())
}

func normalizeVersion(version uint, dirty bool, err error) (uint, bool, error) {
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return version, dirty, err
}

func newMigrator(dsn string) (*migrate.Migrate, error) {
	normalizedDSN, err := migrationDSN(dsn)
	if err != nil {
		return nil, err
	}

	db, err := openSQL("mysql", normalizedDSN)
	if err != nil {
		return nil, fmt.Errorf("migrations: open mysql: %w", err)
	}

	src, err := iofs.New(migrationfiles.FS, ".")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("migrations: embedded source: %w", err), db.Close())
	}

	drv, err := withMySQLInstance(db)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("migrations: mysql driver: %w", err),
			closeSource(src),
			db.Close(),
		)
	}

	m, err := migrate.NewWithInstance("iofs", src, "mysql", drv)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("migrations: new: %w", err),
			closeSource(src),
			closeDatabase(drv),
		)
	}
	return m, nil
}

func migrationDSN(dsn string) (string, error) {
	cfg, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("migrations: parse MYSQL_DSN: %w", err)
	}
	cfg.MultiStatements = true
	return cfg.FormatDSN(), nil
}

func closeMigrator(m *migrate.Migrate) error {
	sourceErr, databaseErr := m.Close()
	return errors.Join(
		wrapCloseError("source", sourceErr),
		wrapCloseError("database", databaseErr),
	)
}

func closeSource(src interface{ Close() error }) error {
	return wrapCloseError("source", src.Close())
}

func closeDatabase(drv interface{ Close() error }) error {
	return wrapCloseError("database", drv.Close())
}

func wrapCloseError(resource string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("migrations: close %s: %w", resource, err)
}

// ListVersions lists and validates migration versions from the embedded SQL source.
func ListVersions() ([]uint, error) {
	return listVersions(migrationfiles.FS)
}

func listVersions(source fs.FS) ([]uint, error) {
	driver, err := iofs.New(source, ".")
	if err != nil {
		return nil, fmt.Errorf("migrations: list embedded files: %w", err)
	}
	defer driver.Close()

	first, err := driver.First()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	versions := []uint{first}
	current := first
	for {
		next, err := driver.Next(current)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		versions = append(versions, next)
		current = next
	}
	return versions, nil
}
