package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// ========== 设备与验证码持久化 ==========
//
// 变化（见 CHANGES.md）：设备、验证码、连接状态不再只存活于内存。
// 策略：进程内变更由定时的 StartAutoSave 落盘，退出时再强制保存一次。

// deviceStoreFile 设备落盘结构
type deviceStoreFile struct {
	Devices map[string]*Device           `json:"devices"`
	Codes   map[string]*VerificationCode `json:"codes"`
}

// SetStore 指定设备存储文件（空字符串 = 不持久化）
func (dm *DeviceManager) SetStore(path string) { dm.storePath = path }

// Load 从磁盘恢复设备与未过期的验证码
func (dm *DeviceManager) Load() error {
	if dm.storePath == "" {
		return nil
	}
	data, err := os.ReadFile(dm.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var f deviceStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", dm.storePath, err)
	}

	now := time.Now()
	dm.mu.Lock()
	for id, d := range f.Devices {
		if d == nil || id == "" {
			continue
		}
		dm.devices[id] = d
	}
	for _, c := range f.Codes {
		if c == nil || !c.IsValid || c.ID == "" || now.After(c.ExpiresAt) {
			continue
		}
		dm.codes[c.ID] = c
		dm.codesByDevice[c.DeviceID] = c
	}
	devices, codes := len(dm.devices), len(dm.codes)
	dm.mu.Unlock()

	log.Printf("[Server] restored %d device(s), %d pending verification code(s)", devices, codes)
	return nil
}

// Save 原子保存设备与验证码
func (dm *DeviceManager) Save() error {
	if dm.storePath == "" {
		return nil
	}

	dm.mu.RLock()
	snap := deviceStoreFile{
		Devices: make(map[string]*Device, len(dm.devices)),
		Codes:   make(map[string]*VerificationCode, len(dm.codes)),
	}
	for k, v := range dm.devices {
		cp := *v
		snap.Devices[k] = &cp
	}
	for k, v := range dm.codes {
		cp := *v
		snap.Codes[k] = &cp
	}
	dm.mu.RUnlock()

	return writeJSONAtomicFile(dm.storePath, snap)
}

// StartAutoSave 周期性保存（interval <= 0 则不启用）
func (dm *DeviceManager) StartAutoSave(interval time.Duration) {
	if dm.storePath == "" || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := dm.Save(); err != nil {
				log.Printf("[Server] save devices failed: %v", err)
			}
		}
	}()
}

// writeJSONAtomicFile 原子写 JSON（临时文件 + 改名）
func writeJSONAtomicFile(path string, v interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
