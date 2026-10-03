package file_gateway

import (
	"os"
	"path/filepath"
	"strings"

	"frostleaves/pkg/security"
)

// recycleDeviceDir returns the recycle-bin directory for a device.
func (gw *FileGateway) recycleDeviceDir(deviceID string) string {
	return filepath.Join(gw.config.StorageRoot, "recycle_bin", deviceID)
}

// recycleRetentionDays returns the configured recycle-bin retention in days
// (模块 2). A policy value <= 0 means "auto-cleanup disabled".
func (gw *FileGateway) recycleRetentionDays() int {
	if gw.policyStore != nil {
		if d := gw.policyStore.Global().RecycleBinRetentionDays; d > 0 {
			return d
		}
		return 0
	}
	return 30
}

// safeRecycleID rejects ids that could escape the recycle directory.
func safeRecycleID(id string) bool {
	if id == "" || security.HasDotDot(id) {
		return false
	}
	return id == filepath.Base(id)
}

// dirSize returns the total size of the files under path (recursively).
func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// recycleUsageBytes returns the bytes currently held in a device recycle bin,
// used to decide whether recycle-bin usage counts towards the storage quota.
func (gw *FileGateway) recycleUsageBytes(deviceID string) int64 {
	entries, err := os.ReadDir(gw.recycleDeviceDir(deviceID))
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".meta") {
			continue
		}
		p := filepath.Join(gw.recycleDeviceDir(deviceID), e.Name())
		if e.IsDir() {
			total += dirSize(p)
			continue
		}
		if info, ierr := e.Info(); ierr == nil {
			total += info.Size()
		}
	}
	return total
}

// purgeRecycleItem permanently removes one recycle item and its metadata.
func purgeRecycleItem(recycleDir, id string) bool {
	p := filepath.Join(recycleDir, id)
	if _, err := os.Stat(p); err != nil {
		return false
	}
	if err := os.RemoveAll(p); err != nil {
		return false
	}
	_ = os.Remove(p + ".meta")
	return true
}

// movePath moves src to dst, handling both files and directories.
func (gw *FileGateway) movePath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return gw.moveDir(src, dst)
	}
	if err := os.Rename(src, dst); err != nil {
		if cerr := gw.copyFile(src, dst); cerr != nil {
			return cerr
		}
		return os.Remove(src)
	}
	return nil
}
