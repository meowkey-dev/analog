package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirDefaultsToUserHome(t *testing.T) {
	t.Setenv("ANALOG_DATA_DIR", "")
	t.Setenv("ANALOG_DB", "")
	t.Setenv("ANALOG_AUTH_FILE", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".analog")
	if got := DataDir(); got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
	if got := DBPath(); got != filepath.Join(want, "analog.db") {
		t.Fatalf("DBPath() = %q, want a path under %q", got, want)
	}
	if got := MediaDir(); got != filepath.Join(want, "media") {
		t.Fatalf("MediaDir() = %q, want a path under %q", got, want)
	}
	if got := AuthPath(); got != filepath.Join(want, "auth.json") {
		t.Fatalf("AuthPath() = %q, want a path under %q", got, want)
	}
}

func TestDataDirEnvironmentOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "server-data")
	t.Setenv("ANALOG_DATA_DIR", dir)
	t.Setenv("ANALOG_DB", "")
	t.Setenv("ANALOG_AUTH_FILE", "")

	if got := DataDir(); got != dir {
		t.Fatalf("DataDir() = %q, want %q", got, dir)
	}
	if got := DBPath(); got != filepath.Join(dir, "analog.db") {
		t.Fatalf("DBPath() = %q, want a path under %q", got, dir)
	}
	if got := MediaDir(); got != filepath.Join(dir, "media") {
		t.Fatalf("MediaDir() = %q, want a path under %q", got, dir)
	}
	if got := AuthPath(); got != filepath.Join(dir, "auth.json") {
		t.Fatalf("AuthPath() = %q, want a path under %q", got, dir)
	}
}

func TestSpecificStoragePathsOverrideDataDir(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "database", "analog.db")
	authFile := filepath.Join(root, "credentials", "auth.json")
	t.Setenv("ANALOG_DATA_DIR", filepath.Join(root, "server-data"))
	t.Setenv("ANALOG_DB", db)
	t.Setenv("ANALOG_AUTH_FILE", authFile)

	if got := DBPath(); got != db {
		t.Fatalf("DBPath() = %q, want %q", got, db)
	}
	if got := AuthPath(); got != authFile {
		t.Fatalf("AuthPath() = %q, want %q", got, authFile)
	}
}
