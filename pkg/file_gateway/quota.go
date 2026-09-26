package file_gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// ========== 配额管理（带持久化） ==========
//
// 变化（见 CHANGES.md）：新增 SetStore / Load / Flush，
// 配置变更立即落盘，用量统计每 10 秒去抖落盘。

type QuotaConfig struct {
	DeviceID          string `json:"device_id"`
	MaxUploadBytes    int64  `json:"max_upload_bytes"`    // 0 = unlimited
	UploadSpeedKBPS   int64  `json:"upload_speed_kbps"`   // 0 = unlimited
	DownloadSpeedKBPS int64  `json:"download_speed_kbps"` // 0 = unlimited
	MaxFileSizeBytes  int64  `json:"max_file_size_bytes"` // 0 = unlimited
}

type DeviceUsage struct {
	UploadBytes   int64 `json:"upload_bytes"`
	DownloadBytes int64 `json:"download_bytes"`
}

type QuotaManager struct {
	mu       sync.RWMutex
	defaults QuotaConfig
	configs  map[string]*QuotaConfig // deviceID -> config
	usage    map[string]*DeviceUsage // deviceID -> usage

	storePath string
	storeMu   sync.Mutex
	dirty     bool
	started   bool
}

// quotaStoreFile 是配额/用量的磁盘结构
type quotaStoreFile struct {
	Configs map[string]*QuotaConfig `json:"configs"`
	Usage   map[string]*DeviceUsage `json:"usage"`
}

func NewQuotaManager(defaults QuotaConfig) *QuotaManager {
	return &QuotaManager{
		defaults: defaults,
		configs:  make(map[string]*QuotaConfig),
		usage:    make(map[string]*DeviceUsage),
	}
}

// ========== 持久化 ==========

// SetStore 指定持久化文件路径（空字符串 = 不持久化），并启动去抖落盘协程
func (qm *QuotaManager) SetStore(path string) {
	qm.storePath = path
	if path == "" || qm.started {
		return
	}
	qm.started = true
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			qm.mu.RLock()
			need := qm.dirty
			qm.mu.RUnlock()
			if need {
				qm.Flush()
			}
		}
	}()
}

// Load 从磁盘恢复配额配置与用量
func (qm *QuotaManager) Load() error {
	if qm.storePath == "" {
		return nil
	}
	data, err := os.ReadFile(qm.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f quotaStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", qm.storePath, err)
	}

	qm.mu.Lock()
	for k, v := range f.Configs {
		if v != nil {
			qm.configs[k] = v
		}
	}
	for k, v := range f.Usage {
		if v != nil {
			qm.usage[k] = v
		}
	}
	cfgCount, useCount := len(qm.configs), len(qm.usage)
	qm.mu.Unlock()

	log.Printf("[Quota] restored %d config(s), %d usage record(s)", cfgCount, useCount)
	return nil
}

// Flush 立即落盘（进程退出前调用）
func (qm *QuotaManager) Flush() {
	qm.persist()
}

func (qm *QuotaManager) markDirty(immediate bool) {
	if qm.storePath == "" {
		return
	}
	qm.mu.Lock()
	qm.dirty = true
	qm.mu.Unlock()
	if immediate {
		qm.persist()
	}
}

func (qm *QuotaManager) persist() {
	if qm.storePath == "" {
		return
	}
	qm.storeMu.Lock()
	defer qm.storeMu.Unlock()

	qm.mu.Lock()
	snap := quotaStoreFile{
		Configs: make(map[string]*QuotaConfig, len(qm.configs)),
		Usage:   make(map[string]*DeviceUsage, len(qm.usage)),
	}
	for k, v := range qm.configs {
		cp := *v
		snap.Configs[k] = &cp
	}
	for k, v := range qm.usage {
		cp := *v
		snap.Usage[k] = &cp
	}
	qm.dirty = false
	qm.mu.Unlock()

	if err := writeJSONAtomic(qm.storePath, snap); err != nil {
		log.Printf("[Quota] save failed: %v", err)
	}
}

// ========== 配置读写 ==========

// GetConfig 返回设备配额（设备级优先，否则全局默认）
func (qm *QuotaManager) GetConfig(deviceID string) QuotaConfig {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	if cfg, ok := qm.configs[deviceID]; ok {
		return *cfg
	}
	return qm.defaults
}

// SetConfig 设置设备级配额
func (qm *QuotaManager) SetConfig(cfg QuotaConfig) {
	qm.mu.Lock()
	qm.configs[cfg.DeviceID] = &cfg
	qm.mu.Unlock()
	qm.markDirty(true)
}

// RemoveConfig 删除设备级配额（回退到默认）
func (qm *QuotaManager) RemoveConfig(deviceID string) {
	qm.mu.Lock()
	delete(qm.configs, deviceID)
	qm.mu.Unlock()
	qm.markDirty(true)
}

// GetAllConfigs 返回全部设备级配额
func (qm *QuotaManager) GetAllConfigs() map[string]QuotaConfig {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	result := make(map[string]QuotaConfig)
	for k, v := range qm.configs {
		result[k] = *v
	}
	return result
}

// ========== 用量 ==========

func (qm *QuotaManager) GetUsage(deviceID string) DeviceUsage {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	if u, ok := qm.usage[deviceID]; ok {
		return *u
	}
	return DeviceUsage{}
}

func (qm *QuotaManager) GetAllUsage() map[string]DeviceUsage {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	result := make(map[string]DeviceUsage)
	for k, v := range qm.usage {
		result[k] = *v
	}
	return result
}

// CheckUploadQuota 检查累计上传配额
func (qm *QuotaManager) CheckUploadQuota(deviceID string, fileSize int64) bool {
	cfg := qm.GetConfig(deviceID)
	if cfg.MaxUploadBytes == 0 {
		return true
	}
	usage := qm.GetUsage(deviceID)
	return (usage.UploadBytes + fileSize) <= cfg.MaxUploadBytes
}

// CheckSingleFileSize 检查单文件大小限制
func (qm *QuotaManager) CheckSingleFileSize(deviceID string, fileSize int64) bool {
	cfg := qm.GetConfig(deviceID)
	if cfg.MaxFileSizeBytes == 0 {
		return true
	}
	return fileSize <= cfg.MaxFileSizeBytes
}

// AddUpload 记录上传字节
func (qm *QuotaManager) AddUpload(deviceID string, bytes int64) {
	qm.mu.Lock()
	if _, ok := qm.usage[deviceID]; !ok {
		qm.usage[deviceID] = &DeviceUsage{}
	}
	qm.usage[deviceID].UploadBytes += bytes
	qm.mu.Unlock()
	qm.markDirty(false)
}

// AddDownload 记录下载字节
func (qm *QuotaManager) AddDownload(deviceID string, bytes int64) {
	qm.mu.Lock()
	if _, ok := qm.usage[deviceID]; !ok {
		qm.usage[deviceID] = &DeviceUsage{}
	}
	qm.usage[deviceID].DownloadBytes += bytes
	qm.mu.Unlock()
	qm.markDirty(false)
}

// GetRemainingQuota 返回剩余上传配额（-1 = 不限）
func (qm *QuotaManager) GetRemainingQuota(deviceID string) int64 {
	cfg := qm.GetConfig(deviceID)
	if cfg.MaxUploadBytes == 0 {
		return -1
	}
	usage := qm.GetUsage(deviceID)
	remaining := cfg.MaxUploadBytes - usage.UploadBytes
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ResetUsage 重置单设备用量
func (qm *QuotaManager) ResetUsage(deviceID string) {
	qm.mu.Lock()
	qm.usage[deviceID] = &DeviceUsage{}
	qm.mu.Unlock()
	qm.markDirty(true)
}

// ResetAllUsage 重置全部用量
func (qm *QuotaManager) ResetAllUsage() {
	qm.mu.Lock()
	qm.usage = make(map[string]*DeviceUsage)
	qm.mu.Unlock()
	qm.markDirty(true)
}
