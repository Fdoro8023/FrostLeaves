package file_gateway

import (
	"fmt"
	"os"
	"path/filepath"

	"frostleaves/pkg/security"
)

// PurgeDeviceData permanently removes a device's private data: its storage
// folder and its recycle bin (模块 7：离线账户数据清除).
func (gw *FileGateway) PurgeDeviceData(deviceID string) error {
	if deviceID == "" || security.HasDotDot(deviceID) || deviceID != filepath.Base(deviceID) {
		return fmt.Errorf("invalid device id")
	}
	removed := false
	for _, p := range []string{
		filepath.Join(gw.config.StorageRoot, "devices", deviceID),
		filepath.Join(gw.config.StorageRoot, "recycle_bin", deviceID),
	} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("purge %s: %w", p, err)
		}
		removed = true
	}
	if removed && gw.audit != nil {
		gw.audit.Log(deviceID, "device_data_purged", "", "success", "", "")
	}
	return nil
}
