package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunListDoesNotLoadConfigurationOrOpenDatabase(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loadDSN := func() (string, error) {
		t.Fatal("list loaded database configuration")
		return "", nil
	}

	exitCode := run([]string{"list"}, &stdout, &stderr, loadDSN)
	if exitCode != 0 {
		t.Fatalf("run(list) exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "  1\n") {
		t.Fatalf("run(list) stdout = %q, want embedded version 1", got)
	}
}

func TestRunUnknownCommandDoesNotLoadConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loadDSN := func() (string, error) {
		t.Fatal("unknown command loaded database configuration")
		return "", nil
	}

	exitCode := run([]string{"unknown"}, &stdout, &stderr, loadDSN)
	if exitCode != 2 {
		t.Fatalf("run(unknown) exit code = %d, want 2", exitCode)
	}
	if got := stderr.String(); !strings.Contains(got, "usage:") {
		t.Fatalf("run(unknown) stderr = %q, want usage", got)
	}
}

func TestRunDatabaseCommandLoadsOnlyDSN(t *testing.T) {
	var stdout, stderr bytes.Buffer
	wantErr := errors.New("load DSN failed")
	calls := 0
	loadDSN := func() (string, error) {
		calls++
		return "", wantErr
	}

	exitCode := run([]string{"up"}, &stdout, &stderr, loadDSN)
	if exitCode != 1 {
		t.Fatalf("run(up) exit code = %d, want 1", exitCode)
	}
	if calls != 1 {
		t.Fatalf("loadDSN calls = %d, want 1", calls)
	}
	if got := stderr.String(); !strings.Contains(got, "MYSQL_DSN") || strings.Contains(got, "JWT_SECRET") {
		t.Fatalf("run(up) stderr = %q, want MYSQL_DSN-only error", got)
	}
}

func TestRunDownRequiresDevelopmentEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	var stdout, stderr bytes.Buffer
	loadDSN := func() (string, error) {
		t.Fatal("protected down command loaded MYSQL_DSN")
		return "", nil
	}

	exitCode := run([]string{"down"}, &stdout, &stderr, loadDSN)
	if exitCode != 1 {
		t.Fatalf("run(down) exit code = %d, want 1", exitCode)
	}
	if got := stderr.String(); !strings.Contains(got, "APP_ENV=dev") {
		t.Fatalf("run(down) stderr = %q, want explicit development protection", got)
	}
}
