package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"frostleaves/pkg/migration"
)

func TestMigrateOnStartupUpgradesLegacyData(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(`{"devices":{"d1":{"id":"d1"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := migrateOnStartup(dir, "1.1.0 beta"); err != nil {
		t.Fatalf("migrateOnStartup: %v", err)
	}
	v, err := migration.DetectVersion(dir)
	if err != nil || v != migration.SchemaVersion {
		t.Fatalf("want schema v%d, got v%d err=%v", migration.SchemaVersion, v, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"codes"`) {
		t.Fatal("codes key missing after startup migration")
	}
}

func TestMigrateOnStartupIsNoopWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, migration.StampFile), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := migrateOnStartup(dir, "1.1.0 beta"); err != nil {
		t.Fatalf("migrateOnStartup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), migration.BackupDirName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup dir created for up-to-date data: %v", err)
	}
}

func TestRollbackFromBackupRestores(t *testing.T) {
	dir := t.TempDir()
	orig := `{"devices":{"d1":{"id":"d1"}}}`
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := migrateOnStartup(dir, "1.1.0 beta"); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(filepath.Dir(dir), migration.BackupDirName)
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no backup produced: %v", err)
	}
	if err := rollbackFromBackup(dir, filepath.Join(backupDir, entries[0].Name())); err != nil {
		t.Fatalf("rollbackFromBackup: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != orig {
		t.Fatalf("rollback mismatch:\nwant %s\ngot  %s", orig, b)
	}
}
