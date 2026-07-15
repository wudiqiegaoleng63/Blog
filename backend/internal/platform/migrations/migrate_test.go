package migrations

import (
	"database/sql"
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	migratesource "github.com/golang-migrate/migrate/v4/source"
	migrationfiles "github.com/lsy/blog/migrations"
)

func TestRootMigrationFSProvidesVersionOne(t *testing.T) {
	for _, name := range []string{"0001_init.up.sql", "0001_init.down.sql"} {
		contents, err := fs.ReadFile(migrationfiles.FS, name)
		if err != nil {
			t.Fatalf("read embedded migration %q: %v", name, err)
		}
		if len(contents) == 0 {
			t.Fatalf("embedded migration %q is empty", name)
		}
	}

	versions, err := ListVersions()
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if want := []uint{1}; !reflect.DeepEqual(versions, want) {
		t.Fatalf("ListVersions() = %v, want %v", versions, want)
	}
}

func TestMigrationDSNForcesMultiStatementsAndPreservesConfiguration(t *testing.T) {
	input := "blog:p@ss:word@tcp(db.example:3306)/blog%2Fprod?loc=Asia%2FShanghai&parseTime=true&timeout=7s"

	normalized, err := migrationDSN(input)
	if err != nil {
		t.Fatalf("migrationDSN: %v", err)
	}
	cfg, err := mysqlconfig.ParseDSN(normalized)
	if err != nil {
		t.Fatalf("parse normalized DSN: %v", err)
	}

	if !cfg.MultiStatements {
		t.Fatal("MultiStatements is false, want true")
	}
	if cfg.User != "blog" || cfg.Passwd != "p@ss:word" || cfg.DBName != "blog/prod" {
		t.Fatalf("credentials/database changed: user=%q password=%q database=%q", cfg.User, cfg.Passwd, cfg.DBName)
	}
	if cfg.Loc.String() != "Asia/Shanghai" || !cfg.ParseTime || cfg.Timeout.String() != "7s" {
		t.Fatalf("query options changed: loc=%v parseTime=%v timeout=%v", cfg.Loc, cfg.ParseTime, cfg.Timeout)
	}
}

func TestListVersionsUsesMigrationSourceValidation(t *testing.T) {
	duplicateVersions := fstest.MapFS{
		"0001_first.up.sql":  {Data: []byte("SELECT 1")},
		"0001_second.up.sql": {Data: []byte("SELECT 2")},
	}

	_, err := listVersions(duplicateVersions)
	var duplicate migratesource.ErrDuplicateMigration
	if !errors.As(err, &duplicate) {
		t.Fatalf("listVersions duplicate error = %v, want ErrDuplicateMigration", err)
	}
}

func TestNormalizeVersionTreatsNilVersionAsFreshDatabase(t *testing.T) {
	version, dirty, err := normalizeVersion(0, false, migrate.ErrNilVersion)
	if err != nil || version != 0 || dirty {
		t.Fatalf("normalizeVersion(ErrNilVersion) = (%d, %v, %v), want (0, false, nil)", version, dirty, err)
	}
}

func TestNewMigratorClosesOwnedDBWhenDriverCreationFails(t *testing.T) {
	originalOpen := openSQL
	originalWithInstance := withMySQLInstance
	t.Cleanup(func() {
		openSQL = originalOpen
		withMySQLInstance = originalWithInstance
	})

	var opened *sql.DB
	openSQL = func(driverName, dataSourceName string) (*sql.DB, error) {
		if driverName != "mysql" {
			t.Fatalf("driverName = %q, want mysql", driverName)
		}
		var err error
		opened, err = sql.Open("mysql", dataSourceName)
		return opened, err
	}
	withMySQLInstance = func(*sql.DB) (migratedatabase.Driver, error) {
		return nil, errors.New("driver setup failed")
	}

	_, err := newMigrator("blog:secret@tcp(localhost:3306)/blog")
	if err == nil {
		t.Fatal("newMigrator returned nil error")
	}
	if opened == nil {
		t.Fatal("sql.Open was not called")
	}
	if err := opened.Ping(); err == nil {
		t.Fatal("owned DB remains open: Ping() succeeded")
	}
}
