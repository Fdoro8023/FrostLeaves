package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"frostleaves/pkg/file_gateway"
	"frostleaves/pkg/policy"
)

func setupRetentionTest(t *testing.T, retentionDays int, retain bool) (*DeviceManager, *file_gateway.Authenticator, *file_gateway.AuditLogger, string) {
	t.Helper()
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	if err := os.MkdirAll(filepath.Join(storage, "devices", "dev1", "private"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage, "devices", "dev1", "private", "x.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	audit, err := file_gateway.NewAuditLogger(logs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { audit.Close() })
	quota := file_gateway.NewQuotaManager(file_gateway.QuotaConfig{})
	auth := file_gateway.NewAuthenticator("test-secret")
	gw := file_gateway.NewFileGateway(file_gateway.GatewayConfig{
		StorageRoot:     storage,
		PublicSharedDir: filepath.Join(storage, "shared"),
	}, quota, audit, auth)

	activeGateway = gw
	policyStore = policy.NewStore()
	p := policy.DefaultPolicy()
	p.RetainOfflineData = retain
	p.OfflineRetentionDays = retentionDays
	if err := policyStore.SetGlobal(p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		activeGateway = nil
		policyStore = nil
	})

	dm := NewDeviceManager()
	if err := dm.ApplyDevice("dev1", "PC", "windows", "model"); err != nil {
		t.Fatalf("ApplyDevice: %v", err)
	}
	if _, err := dm.ApproveDevice("dev1"); err != nil {
		t.Fatalf("ApproveDevice: %v", err)
	}
	return dm, auth, audit, storage
}

func ageDevice(dm *DeviceManager, id string, ago time.Duration) {
	old := time.Now().Add(-ago)
	dm.mu.Lock()
	if d := dm.devices[id]; d != nil {
		d.LastSeenAt = &old
	}
	dm.mu.Unlock()
}

func TestRunOfflineRetentionPurgesExpiredDevice(t *testing.T) {
	dm, auth, audit, storage := setupRetentionTest(t, 5, true)
	ageDevice(dm, "dev1", 30*24*time.Hour)

	purged := runOfflineRetention(dm, auth, audit)
	if purged != 1 {
		t.Fatalf("want 1 purged device, got %d", purged)
	}
	if _, err := os.Stat(filepath.Join(storage, "devices", "dev1")); !os.IsNotExist(err) {
		t.Fatalf("expired device private data must be purged, err=%v", err)
	}
}

func TestRunOfflineRetentionKeepsOnlineDevice(t *testing.T) {
	dm, auth, audit, storage := setupRetentionTest(t, 5, true)
	ageDevice(dm, "dev1", 30*24*time.Hour)
	auth.GenerateToken("dev1", "PC", "windows", "client") // device is online again

	if purged := runOfflineRetention(dm, auth, audit); purged != 0 {
		t.Fatalf("online device must not be purged, got %d", purged)
	}
	if _, err := os.Stat(filepath.Join(storage, "devices", "dev1", "private", "x.txt")); err != nil {
		t.Fatalf("online device data must survive: %v", err)
	}
}

func TestRunOfflineRetentionKeepsWithinWindow(t *testing.T) {
	dm, auth, audit, storage := setupRetentionTest(t, 10, true)
	ageDevice(dm, "dev1", 2*24*time.Hour)

	if purged := runOfflineRetention(dm, auth, audit); purged != 0 {
		t.Fatalf("device within window must not be purged, got %d", purged)
	}
	if _, err := os.Stat(filepath.Join(storage, "devices", "dev1", "private", "x.txt")); err != nil {
		t.Fatalf("device data within window must survive: %v", err)
	}
}

func TestRunOfflineRetentionDisabledPurgesImmediately(t *testing.T) {
	dm, auth, audit, storage := setupRetentionTest(t, 30, false)
	ageDevice(dm, "dev1", time.Minute)

	if purged := runOfflineRetention(dm, auth, audit); purged != 1 {
		t.Fatalf("retention disabled must purge offline device, got %d", purged)
	}
	if _, err := os.Stat(filepath.Join(storage, "devices", "dev1")); !os.IsNotExist(err) {
		t.Fatalf("device data must be purged, err=%v", err)
	}
}

func TestRetentionDecisionsReportReasons(t *testing.T) {
	dm, auth, _, _ := setupRetentionTest(t, 5, true)
	ageDevice(dm, "dev1", 30*24*time.Hour)
	decisions := retentionDecisions(dm, auth)
	if len(decisions) != 1 || !decisions[0].Purge {
		t.Fatalf("expected one purge decision, got %+v", decisions)
	}
}
