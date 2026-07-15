// Package main is the migration CLI entrypoint.
//
// Usage:
//
//	migrate up       apply all pending migrations
//	migrate down     roll back one migration (development only)
//	migrate version  print the current version and dirty state
//	migrate list     list embedded migration versions
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/lsy/blog/internal/config"
	migrationspkg "github.com/lsy/blog/internal/platform/migrations"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, loadMigrationDSN))
}

type loadDSNFunc func() (string, error)

func run(args []string, stdout, stderr io.Writer, loadDSN loadDSNFunc) int {
	if len(args) != 1 {
		usage(stderr)
		return 2
	}

	cmd := args[0]
	if cmd == "list" {
		versions, err := migrationspkg.ListVersions()
		if err != nil {
			fmt.Fprintf(stderr, "list failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "embedded migration versions:")
		for _, version := range versions {
			fmt.Fprintf(stdout, "  %d\n", version)
		}
		return 0
	}
	if cmd != "up" && cmd != "down" && cmd != "version" {
		usage(stderr)
		return 2
	}

	if cmd == "down" && os.Getenv("APP_ENV") != "dev" {
		fmt.Fprintln(stderr, "migrate down is allowed only when APP_ENV=dev")
		return 1
	}

	dsn, err := loadDSN()
	if err != nil {
		fmt.Fprintf(stderr, "load MYSQL_DSN failed: %v\n", err)
		return 1
	}

	switch cmd {
	case "up":
		if err := migrationspkg.RunUp(dsn); err != nil {
			fmt.Fprintf(stderr, "migrate up failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "migrations applied")
	case "down":
		if err := migrationspkg.RunDown(dsn); err != nil {
			fmt.Fprintf(stderr, "migrate down failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "last migration rolled back")
	case "version":
		version, dirty, err := migrationspkg.Version(dsn)
		if err != nil {
			fmt.Fprintf(stderr, "version failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "version=%d dirty=%v\n", version, dirty)
	}
	return 0
}

func loadMigrationDSN() (string, error) {
	return config.LoadMySQLDSN()
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: migrate [up|down|version|list]")
}
