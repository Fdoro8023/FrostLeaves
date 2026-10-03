package file_gateway

import (
	"os"
	"path/filepath"

	"frostleaves/pkg/policy"
)

// SetPolicyStore injects the three-level policy store used for server-side
// authorization (模块 3)。
func (gw *FileGateway) SetPolicyStore(s *policy.Store) { gw.policyStore = s }

// PolicyStore returns the injected policy store (may be nil).
func (gw *FileGateway) PolicyStore() *policy.Store { return gw.policyStore }

// deviceUsageBytes returns the bytes currently stored for a device.
func (gw *FileGateway) deviceUsageBytes(deviceID string) int64 {
	var total int64
	if gw.quota != nil {
		total = gw.quota.GetUsage(deviceID).UploadBytes
	}
	// 模块 2 / 3-10：回收站占用是否计入配额由策略开关决定
	if gw.policyStore != nil && gw.policyStore.Resolve(deviceID).RecycleBinCountsQuota {
		total += gw.recycleUsageBytes(deviceID)
	}
	return total
}

// deviceFileCount counts regular files under the device private root.
func (gw *FileGateway) deviceFileCount(deviceID string) int64 {
	root := filepath.Join(gw.config.StorageRoot, "devices", deviceID, "private")
	var count int64
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count
}
