package file_gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPurgeDeviceDataRemovesStorageAndRecycle(t *testing.T) {
	gw, _, _, _ := newTestGateway(t)
	devDir := filepath.Join(gw.config.StorageRoot, "devices", "dev1", "private")
	if err := os.MkdirAll(filepath.Join(devDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "sub", "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	recDir := gw.recycleDeviceDir("dev1")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recDir, "b.bin"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := gw.PurgeDeviceData("dev1"); err != nil {
		t.Fatalf("PurgeDeviceData: %v", err)
	}
	if _, err := os.Stat(filepath.Join(gw.config.StorageRoot, "devices", "dev1")); !os.IsNotExist(err) {
		t.Fatalf("device storage should be gone, err=%v", err)
	}
	if _, err := os.Stat(recDir); !os.IsNotExist(err) {
		t.Fatalf("device recycle bin should be gone, err=%v", err)
	}
}

func TestPurgeDeviceDataRejectsUnsafeID(t *testing.T) {
	gw, _, _, _ := newTestGateway(t)
	if err := gw.PurgeDeviceData("../evil"); err == nil {
		t.Fatal("expected error for unsafe device id")
	}
	if err := gw.PurgeDeviceData(""); err == nil {
		t.Fatal("expected error for empty device id")
	}
}
