package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func topKeys(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(mustRead(t, path)), &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func TestDetectVersionLegacyAndStamped(t *testing.T) {
	dir := t.TempDir()
	v, err := DetectVersion(dir)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if v != LegacyVersion {
		t.Fatalf("want legacy %d, got %d", LegacyVersion, v)
	}
	mustWrite(t, filepath.Join(dir, StampFile), `{"version": 1}`)
	v, err = DetectVersion(dir)
	if err != nil || v != 1 {
		t.Fatalf("want 1, got %d err=%v", v, err)
	}
}

func TestBackupThenRollbackBitForBit(t *testing.T) {
	dataDir := t.TempDir()
	orig := `{"devices":{"d1":{"id":"d1"}},"codes":{}}`
	mustWrite(t, filepath.Join(dataDir, "devices.json"), orig)
	backupRoot := t.TempDir()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	backupDir, man, err := Backup(dataDir, backupRoot, "1.0.1 beta", LegacyVersion, now)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if len(man.Files) != 1 || man.Files[0].File != "devices.json" {
		t.Fatalf("unexpected manifest: %+v", man)
	}
	if man.Files[0].SHA256 == "" || man.Files[0].Size == 0 {
		t.Fatalf("manifest missing hash/size: %+v", man.Files[0])
	}
	if _, err := os.Stat(filepath.Join(backupDir, ManifestName)); err != nil {
		t.Fatalf("manifest file missing: %v", err)
	}

	mustWrite(t, filepath.Join(dataDir, "devices.json"), `{"devices":{}}`)
	mustWrite(t, filepath.Join(dataDir, StampFile), `{"version":1}`)

	if err := Rollback(dataDir, backupDir); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := mustRead(t, filepath.Join(dataDir, "devices.json")); got != orig {
		t.Fatalf("rollback not byte-for-byte:\nwant %s\ngot  %s", orig, got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, StampFile)); !os.IsNotExist(err) {
		t.Fatalf("stamp should be removed after rollback, err=%v", err)
	}
}

func TestRunMigratesLegacyStores(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, "devices.json"), `{"devices":{"d1":{"id":"d1","name":"PC"}}}`)
	mustWrite(t, filepath.Join(dataDir, "quota.json"), `{"configs":{"d1":{"device_id":"d1"}}}`)
	mustWrite(t, filepath.Join(dataDir, "tokens.json"), `{"blocked":["bad"]}`)
	mustWrite(t, filepath.Join(dataDir, "shares.json"), `{}`)

	res, err := Run(Options{DataDir: dataDir, AppVersion: "1.1.0 beta"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Already {
		t.Fatal("did not expect Already")
	}
	if res.FromVersion != LegacyVersion || res.ToVersion != SchemaVersion {
		t.Fatalf("version transition wrong: %+v", res)
	}
	if len(res.Applied) != 1 || res.Applied[0] != "v0-to-v1-normalize-stores" {
		t.Fatalf("unexpected steps: %v", res.Applied)
	}
	if res.BackupDir == "" {
		t.Fatal("expected a backup dir")
	}
	if _, err := os.Stat(filepath.Join(res.BackupDir, ManifestName)); err != nil {
		t.Fatalf("backup manifest missing: %v", err)
	}

	dev := topKeys(t, filepath.Join(dataDir, "devices.json"))
	if _, ok := dev["codes"]; !ok {
		t.Fatal("devices.json missing codes after migration")
	}
	if !strings.Contains(string(dev["devices"]), `"d1"`) {
		t.Fatal("device data was not preserved")
	}
	q := topKeys(t, filepath.Join(dataDir, "quota.json"))
	if _, ok := q["usage"]; !ok {
		t.Fatal("quota.json missing usage")
	}
	tk := topKeys(t, filepath.Join(dataDir, "tokens.json"))
	if _, ok := tk["tokens"]; !ok {
		t.Fatal("tokens.json missing tokens")
	}
	v, err := DetectVersion(dataDir)
	if err != nil || v != SchemaVersion {
		t.Fatalf("stamp wrong: v=%d err=%v", v, err)
	}
}

func TestRunIdempotentWhenCurrent(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, StampFile), `{"version":1}`)
	res, err := Run(Options{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Already {
		t.Fatal("expected Already on up-to-date data")
	}
}

func TestRunRollsBackOnCorruptStore(t *testing.T) {
	dataDir := t.TempDir()
	devOrig := `{"devices":{"d1":{"id":"d1"}}}`
	badQuota := `{ this is not json`
	mustWrite(t, filepath.Join(dataDir, "devices.json"), devOrig)
	mustWrite(t, filepath.Join(dataDir, "quota.json"), badQuota)

	_, err := Run(Options{DataDir: dataDir, AppVersion: "1.1.0 beta"})
	if err == nil {
		t.Fatal("expected migration to fail on corrupt store")
	}
	if got := mustRead(t, filepath.Join(dataDir, "devices.json")); got != devOrig {
		t.Fatalf("devices.json not rolled back:\nwant %s\ngot  %s", devOrig, got)
	}
	if got := mustRead(t, filepath.Join(dataDir, "quota.json")); got != badQuota {
		t.Fatal("quota.json changed during failed migration")
	}
	if _, err := os.Stat(filepath.Join(dataDir, StampFile)); !os.IsNotExist(err) {
		t.Fatalf("stamp should not exist after failed migration, err=%v", err)
	}
}

func TestValidateDetectsMissingKey(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, "devices.json"), `{"devices":{}}`)
	if err := Validate(dataDir); err == nil {
		t.Fatal("expected validation error for missing codes key")
	}
}

func TestValidateAcceptsMissingOptionalFiles(t *testing.T) {
	if err := Validate(t.TempDir()); err != nil {
		t.Fatalf("empty dir should validate: %v", err)
	}
}

func TestRunRefusesDowngrade(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, StampFile), `{"version": 9}`)
	if _, err := Run(Options{DataDir: dataDir}); err == nil {
		t.Fatal("expected downgrade refusal")
	}
}

func TestRunFailsOnWrongShape(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, "shares.json"), `[1,2,3]`)
	if _, err := Run(Options{DataDir: dataDir}); err == nil {
		t.Fatal("expected failure: JSON array is not an object")
	}
}
